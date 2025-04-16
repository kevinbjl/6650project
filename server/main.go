package main

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

const (
	ARENA_SIZE      = 50
	REDIS_KEY       = "target_positions"
	HIT_RADIUS      = 1.0  // Match client's TARGET_RADIUS
	POS_SIZE        = 100  // Store last 100 positions
	COMPENSATION_WINDOW = 100 // 100ms window for lag compensation
	NO_COMPENSATION_WINDOW = 10 // 10ms window when compensation is disabled
)

type Position struct {
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Z           float64 `json:"z"`
	Timestamp   int64   `json:"timestamp"`
	ServerTime  int64   `json:"serverTime"`
}

type SyncMessage struct {
	Type            string `json:"type"`
	ClientTime      int64  `json:"clientTime"`
	ServerRecvTime  int64  `json:"serverRecvTime"`
	ServerSendTime  int64  `json:"serverSendTime"`
}

type GameServer struct {
	targetPositions   []Position
	clients           map[*websocket.Conn]bool
	mutex             sync.Mutex
	serverStartTime   int64
	redisClient       *redis.Client
	clientOffsets     map[*websocket.Conn]int64  // Store clock offset for each client
	clientLatencies   map[*websocket.Conn]int64  // Store measured latency for each client
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func NewGameServer() *GameServer {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	// Initialize Redis key
	ctx := context.Background()
	// Delete the key if it exists to ensure clean state
	rdb.Del(ctx, REDIS_KEY)

	return &GameServer{
		targetPositions:   make([]Position, 0),
		clients:           make(map[*websocket.Conn]bool),
		serverStartTime:   time.Now().UnixMilli(),
		redisClient:       rdb,
		clientOffsets:     make(map[*websocket.Conn]int64),
		clientLatencies:   make(map[*websocket.Conn]int64),
	}
}

func (gs *GameServer) storePositionInRedis(pos Position) error {
	ctx := context.Background()
	
	// Store relative time instead of absolute time
	relativeTime := pos.Timestamp - gs.serverStartTime
	
	// Convert position to JSON
	posJSON, err := json.Marshal(pos)
	if err != nil {
		return err
	}

	// Store in Redis with relative timestamp as score
	err = gs.redisClient.ZAdd(ctx, REDIS_KEY, redis.Z{
		Score:  float64(relativeTime),
		Member: posJSON,
	}).Err()
	
	if err != nil {
		// If we get WRONGTYPE error, try to fix it by deleting and recreating the key
		if err.Error() == "WRONGTYPE Operation against a key holding the wrong kind of value" {
			gs.redisClient.Del(ctx, REDIS_KEY)
			return gs.redisClient.ZAdd(ctx, REDIS_KEY, redis.Z{
				Score:  float64(relativeTime),
				Member: posJSON,
			}).Err()
		}
		return err
	}

	// Keep the last few positions
	gs.redisClient.ZRemRangeByRank(ctx, REDIS_KEY, 0, -(POS_SIZE + 1))
	
	return nil
}

func (gs *GameServer) getPositionsFromRedis(startTime, endTime int64) ([]Position, error) {
	ctx := context.Background()
	
	// Get all positions and filter in memory for now
	results, err := gs.redisClient.ZRange(ctx, REDIS_KEY, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	positions := make([]Position, 0, len(results))
	for _, result := range results {
		var pos Position
		if err := json.Unmarshal([]byte(result), &pos); err != nil {
			continue
		}
		positions = append(positions, pos)
	}

	// Debug logging for Redis contents
	log.Printf("Redis contains %d positions", len(positions))
	if len(positions) > 0 {
		log.Printf("Time range in Redis: %d to %d", positions[0].ServerTime, positions[len(positions)-1].ServerTime)
		log.Printf("Looking for positions between %d and %d", startTime, endTime)
	}

	return positions, nil
}

func (gs *GameServer) MoveTarget() {
	// Initialize target position at a random point in the arena
	currentPos := Position{
		X:           -ARENA_SIZE/4 + float64(rand.Intn(ARENA_SIZE/2)), // Keep target in visible area
		Y:           1.0,  // Height of the target
		Z:           -ARENA_SIZE/4 + float64(rand.Intn(ARENA_SIZE/2)), // Keep target in visible area
		Timestamp:   time.Now().UnixMilli(),
		ServerTime:  0,
	}

	// Movement speed in units per second
	speed := 5.0
	lastUpdate := time.Now()
	direction := 1.0  // 1 for moving right, -1 for moving left

	// Debug logging for initial position
	log.Printf("Target initialized at position: (%.2f, %.2f, %.2f)", currentPos.X, currentPos.Y, currentPos.Z)

	// Test Redis connection
	ctx := context.Background()
	if err := gs.redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("Warning: Redis connection failed: %v", err)
	} else {
		log.Printf("Redis connection successful")
	}

	for {
		currentTime := time.Now()
		elapsed := currentTime.Sub(lastUpdate).Seconds()
		lastUpdate = currentTime

		// Update position based on speed
		currentPos.X += speed * elapsed * direction

		// Change direction when target reaches arena boundaries
		// Keep target within visible area (ARENA_SIZE/4 to -ARENA_SIZE/4)
		if currentPos.X >= ARENA_SIZE/4 {
			direction = -1.0
			currentPos.Z = -ARENA_SIZE/4 + float64(rand.Intn(ARENA_SIZE/2))
		} else if currentPos.X <= -ARENA_SIZE/4 {
			direction = 1.0
			currentPos.Z = -ARENA_SIZE/4 + float64(rand.Intn(ARENA_SIZE/2))
		}

		// Update timestamps
		currentPos.Timestamp = currentTime.UnixMilli()
		currentPos.ServerTime = currentTime.UnixMilli() - gs.serverStartTime

		gs.mutex.Lock()
		// Only keep the current position in memory
		gs.targetPositions = []Position{currentPos}
		gs.mutex.Unlock()

		// Store position in Redis
		if err := gs.storePositionInRedis(currentPos); err != nil {
			log.Printf("Error storing position in Redis: %v", err)
		}

		// Broadcast position with debug logging
		log.Printf("Broadcasting position: (%.2f, %.2f, %.2f)", currentPos.X, currentPos.Y, currentPos.Z)
		gs.BroadcastTargetPosition(currentPos)

		time.Sleep(25 * time.Millisecond)
	}
}

func (gs *GameServer) BroadcastTargetPosition(pos Position) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	message, _ := json.Marshal(map[string]interface{}{
		"type": "position",
		"position": map[string]interface{}{
			"x": pos.X,
			"y": pos.Y,
			"z": pos.Z,
			"timestamp": pos.Timestamp,
			"serverTime": pos.ServerTime,
		},
	})
	for client := range gs.clients {
		client.WriteMessage(websocket.TextMessage, message)
	}
}

func (gs *GameServer) HandleSync(conn *websocket.Conn, clientTime int64) {
	serverRecvTime := time.Now().UnixMilli() - gs.serverStartTime
	serverSendTime := time.Now().UnixMilli() - gs.serverStartTime
	
	response := SyncMessage{
		Type:           "sync_response",
		ClientTime:     clientTime,
		ServerRecvTime: serverRecvTime,
		ServerSendTime: serverSendTime,
	}
	
	conn.WriteJSON(response)
}

func (gs *GameServer) HandleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket Upgrade Error:", err)
		return
	}
	defer conn.Close()

	gs.mutex.Lock()
	gs.clients[conn] = true
	gs.mutex.Unlock()

	// Send initial position
	if len(gs.targetPositions) > 0 {
		gs.BroadcastTargetPosition(gs.targetPositions[0])
	}

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Println("Read Error:", err)
			gs.mutex.Lock()
			delete(gs.clients, conn)
			gs.mutex.Unlock()
			return
		}

		var data map[string]interface{}
		if err := json.Unmarshal(msg, &data); err != nil {
			log.Println("JSON Unmarshal Error:", err)
			continue
		}

		switch data["type"].(string) {
		case "sync":
			gs.HandleSync(conn, int64(data["timestamp"].(float64)))
		case "shoot":
			gs.HandleShoot(
				conn,
				int64(data["timestamp"].(float64)),
				time.Now().UnixMilli(),
				Position{
					X: data["x"].(float64),
					Y: data["y"].(float64),
					Z: data["z"].(float64),
				},
				int64(data["offset"].(float64)),
				data["compensation_enabled"].(bool),
			)
		case "latency_update":
			// Client is reporting its measured RTT
			rtt := int64(data["rtt"].(float64))
			gs.mutex.Lock()
			gs.clientLatencies[conn] = rtt / 2  // Store one-way latency
			gs.mutex.Unlock()
			log.Printf("Updated latency for client: %d ms", rtt/2)
		case "ping":
			// Respond to ping with pong
			pongData := map[string]interface{}{
				"type": "pong",
			}
			conn.WriteJSON(pongData)
		}
	}
}

func (gs *GameServer) HandleShoot(conn *websocket.Conn, clientShootTime, serverReceivedTime int64, clientPerceivePos Position, clientOffset int64, compensationEnabled bool) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	var targetTime int64
	var window int64

	if compensationEnabled {
		// Convert client time to server time using the provided offset
		targetTime = clientShootTime + clientOffset
		window = COMPENSATION_WINDOW
	} else {
		// If compensation is disabled, use the current server time with a very small window
		targetTime = serverReceivedTime
		window = NO_COMPENSATION_WINDOW
	}

	// Get positions from Redis within the window
	positions, err := gs.getPositionsFromRedis(
		targetTime - window,
		targetTime + window,
	)
	
	if err != nil {
		log.Printf("Error getting positions from Redis: %v", err)
		positions = gs.targetPositions
		log.Printf("Using in-memory positions instead")
	}

	// Debug logging
	log.Printf("Shoot attempt - Client pos: (%.2f, %.2f, %.2f), Client Time: %d, Server Time: %d, Offset: %d, Compensation: %v, Window: %dms", 
		clientPerceivePos.X, clientPerceivePos.Y, clientPerceivePos.Z, clientShootTime, targetTime, clientOffset, compensationEnabled, window)
	log.Printf("Looking for positions around time: %d (±%d ms)", targetTime, window)
	log.Printf("Number of positions to check: %d", len(positions))

	// Find the closest position in time to when the client shot
	var closestPos Position
	minTimeDiff := int64(^uint64(0) >> 1) // Max int64

	for _, pos := range positions {
		timeDiff := abs(pos.ServerTime - targetTime)
		if timeDiff < minTimeDiff {
			minTimeDiff = timeDiff
			closestPos = pos
		}
	}

	if minTimeDiff != int64(^uint64(0) >> 1) {
		// Calculate distance to the target at the time of the shot
		dist := distance(closestPos, clientPerceivePos)
		
		// Calculate individual axis distances for debugging
		dx := math.Abs(closestPos.X - clientPerceivePos.X)
		dy := math.Abs(closestPos.Y - clientPerceivePos.Y)
		dz := math.Abs(closestPos.Z - clientPerceivePos.Z)
		
		log.Printf("Checking position (%.2f, %.2f, %.2f) at relative time %d", 
			closestPos.X, closestPos.Y, closestPos.Z, closestPos.ServerTime)
		log.Printf("Distance components - X: %.2f, Y: %.2f, Z: %.2f, Total: %.2f, Target Radius: %.2f", 
			dx, dy, dz, dist, float64(HIT_RADIUS))
		log.Printf("Time difference: %d ms", minTimeDiff)
		
		// More detailed hit detection logging
		if dist < float64(HIT_RADIUS) {
			log.Printf("HIT! Distance: %.2f < Radius: %.2f", dist, float64(HIT_RADIUS))
			result := map[string]interface{}{
				"type":      "hit_result", 
				"hit":       true,
				"distance":  dist,
				"timeDiff":  minTimeDiff,
				"hit_x":     clientPerceivePos.X,
				"hit_y":     clientPerceivePos.Y,
				"hit_z":     clientPerceivePos.Z,
				"target_x":  closestPos.X,
				"target_y":  closestPos.Y,
				"target_z":  closestPos.Z,
			}
			conn.WriteJSON(result)
			return
		} else {
			log.Printf("MISS! Distance: %.2f >= Radius: %.2f", dist, float64(HIT_RADIUS))
		}
	}

	result := map[string]interface{}{
		"type": "hit_result", 
		"hit":  false,
	}
	conn.WriteJSON(result)
}

func distance(a, b Position) float64 {
	return math.Sqrt(math.Pow(float64(a.X-b.X), 2) + math.Pow(float64(a.Y-b.Y), 2) + math.Pow(float64(a.Z-b.Z), 2))
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func main() {
	gameServer := NewGameServer()
	go gameServer.MoveTarget()

	// Add CORS middleware
	corsMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			
			next(w, r)
		}
	}

	// Set up routes
	http.HandleFunc("/ws", corsMiddleware(gameServer.HandleConnection))
	
	// Serve static files
	fs := http.FileServer(http.Dir("../web-client"))
	http.Handle("/", fs)

	log.Println("Server started on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
