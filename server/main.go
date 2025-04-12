package main

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

const (
	TARGET_RADIUS = 5   // TODO: this should be stored in the server
	WIDTH         = 1280
	HEIGHT        = 720
	REDIS_KEY     = "target_positions"
	HIT_RADIUS    = 30  // Increased hit detection radius for easier testing
	POS_SIZE 	  = 100 // Store last 100 positions
	COMPENSATION_WINDOW = 250 // Allows 250ms compensation window
)

type Position struct {
	X           int   `json:"x"`
	Y           int   `json:"y"`
	Timestamp   int64 `json:"timestamp"`
	ServerTime  int64 `json:"serverTime"`
}

type SyncMessage struct {
	Type        string `json:"type"`
	ClientTime  int64  `json:"clientTime"`
	ServerTime  int64  `json:"serverTime"`
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
	CheckOrigin: func(r *http.Request) bool { return true },
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
	// Initialize target position at the left edge
	currentPos := Position{
		X:           0,
		Y:           HEIGHT / 2,
		Timestamp:   time.Now().UnixMilli(),
		ServerTime:  0,
	}

	// Movement speed in pixels per second
	speed := 200.0
	lastUpdate := time.Now()

	// Debug logging for initial position
	log.Printf("Target initialized at position: (%d, %d)", currentPos.X, currentPos.Y)

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
		currentPos.X += int(speed * elapsed)

		// Reset position when target reaches right edge
		if currentPos.X >= WIDTH {
			currentPos.X = 0
		}

		// Update timestamps
		currentPos.Timestamp = currentTime.UnixMilli()
		currentPos.ServerTime = currentTime.UnixMilli() - gs.serverStartTime

		gs.mutex.Lock()
		// Only keep the current position in memory
		gs.targetPositions = []Position{currentPos}
		gs.mutex.Unlock()

		// Store position in Redis with debug logging
		if err := gs.storePositionInRedis(currentPos); err != nil {
			log.Printf("Error storing position in Redis: %v", err)
		} else {
			// Verify the position was stored
			results, err := gs.redisClient.ZRange(ctx, REDIS_KEY, -1, -1).Result()
			if err != nil {
				log.Printf("Error verifying Redis storage: %v", err)
			} else if len(results) > 0 {
				var storedPos Position
				if err := json.Unmarshal([]byte(results[0]), &storedPos); err == nil {
					log.Printf("Latest position in Redis: (%d, %d), Relative Time: %d", 
						storedPos.X, storedPos.Y, storedPos.ServerTime)
				}
			}
		}

		// Broadcast position
		gs.BroadcastTargetPosition(currentPos)

		time.Sleep(25 * time.Millisecond)
	}
}

func (gs *GameServer) BroadcastTargetPosition(pos Position) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	message, _ := json.Marshal(map[string]interface{}{
		"type":     "position",
		"position": pos,
	})
	for client := range gs.clients {
		client.WriteMessage(websocket.TextMessage, message)
	}
}

func (gs *GameServer) HandleSync(conn *websocket.Conn, clientTime int64) {
	serverTime := time.Now().UnixMilli()
	
	// Calculate clock offset: ((t1 - t0) + (t2 - t3))/2
	// where t0 = client send time, t1 = server receive time
	// t2 = server send time, t3 = client receive time
	// For now we only have t0 and t1, so we'll estimate offset
	offset := serverTime - clientTime
	
	gs.mutex.Lock()
	gs.clientOffsets[conn] = offset
	gs.mutex.Unlock()
	
	// Send response with server time and original client time
	response := SyncMessage{
		Type:       "sync_response",
		ClientTime: clientTime,
		ServerTime: serverTime,
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

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			gs.mutex.Lock()
			delete(gs.clients, conn)
			delete(gs.clientOffsets, conn)
			delete(gs.clientLatencies, conn)
			gs.mutex.Unlock()
			return
		}

		var data map[string]interface{}
		json.Unmarshal(msg, &data)

		switch data["type"].(string) {
		case "sync":
			gs.HandleSync(conn, int64(data["timestamp"].(float64)))
		case "shoot":
			gs.HandleShoot(conn, 
				int64(data["timestamp"].(float64)),
				time.Now().UnixMilli(),
				Position{
					X: int(data["x"].(float64)),
					Y: int(data["y"].(float64)),
				},
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

func (gs *GameServer) HandleShoot(conn *websocket.Conn, clientShootTime, serverReceivedTime int64, clientPerceivePos Position) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	// Get client's clock offset and latency
	offset, exists := gs.clientOffsets[conn]
	if !exists {
		log.Printf("Warning: No clock offset found for client")
		offset = 0
	}

	latency, exists := gs.clientLatencies[conn]
	if !exists {
		log.Printf("Warning: No latency measurement found for client")
		latency = 0
	}

	// Convert client time to server time using the offset
	serverTime := serverReceivedTime - gs.serverStartTime
	adjustedClientTime := clientShootTime + offset
	
	// Calculate the target time by rewinding by the measured latency
	targetTime := adjustedClientTime - latency

	// Lag compensation window (250ms)
	lagCompensationWindow := int64(COMPENSATION_WINDOW)

	// Get positions from Redis within the compensation window
	positions, err := gs.getPositionsFromRedis(
		targetTime - lagCompensationWindow,
		targetTime + lagCompensationWindow,
	)
	
	if err != nil {
		log.Printf("Error getting positions from Redis: %v", err)
		positions = gs.targetPositions
		log.Printf("Using in-memory positions instead")
	}

	// Debug logging
	log.Printf("Shoot attempt - Client pos: (%d, %d), Client Time: %d, Server Time: %d, Offset: %d, Latency: %d", 
		clientPerceivePos.X, clientPerceivePos.Y, clientShootTime, serverTime, offset, latency)
	log.Printf("Looking for positions around time: %d (±%d ms)", targetTime, lagCompensationWindow)
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
		log.Printf("Checking position (%d, %d) at relative time %d - Distance: %.2f, TimeDiff: %d, Target Radius: %.2f", 
			closestPos.X, closestPos.Y, closestPos.ServerTime, dist, minTimeDiff, float64(HIT_RADIUS))
		
		if dist < float64(HIT_RADIUS) {
			result := map[string]interface{}{
				"type":      "hit_result", 
				"hit":       true,
				"distance":  dist,
				"timeDiff":  minTimeDiff,
			}
			conn.WriteJSON(result)
			return
		}
	}

	result := map[string]interface{}{
		"type": "hit_result", 
		"hit":  false,
	}
	conn.WriteJSON(result)
}

func distance(a, b Position) float64 {
	return math.Sqrt(math.Pow(float64(a.X-b.X), 2) + math.Pow(float64(a.Y-b.Y), 2))
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
	http.HandleFunc("/ws", gameServer.HandleConnection)
	log.Println("Server started on :8080")
	http.ListenAndServe(":8080", nil)
}
