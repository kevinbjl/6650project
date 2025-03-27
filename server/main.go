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
	TARGET_RADIUS = 5  // Reduced from 20 to 5
	WIDTH         = 800
	HEIGHT        = 600
	REDIS_KEY     = "target_positions"
	HIT_RADIUS    = 30  // Increased hit detection radius for easier testing
)

type Position struct {
	X           int   `json:"x"`
	Y           int   `json:"y"`
	Timestamp   int64 `json:"timestamp"`
	ServerTime  int64 `json:"serverTime"`
}

type TargetTrajectory struct {
	StartX     int
	StartY     int
	EndX       int
	EndY       int
	StartTime  int64
	Duration   int64 // milliseconds
	Velocity   float64
}

type GameServer struct {
	targetPositions   []Position
	currentTrajectory TargetTrajectory
	clients           map[*websocket.Conn]bool
	mutex             sync.Mutex
	serverStartTime   int64
	redisClient       *redis.Client
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
	}
}

func (gs *GameServer) GenerateTrajectory() TargetTrajectory {
	// More varied and faster trajectories
	startX := 0
	startY := rand.Intn(HEIGHT)
	
	// Randomize end point with more variance
	endX := WIDTH
	endY := startY + rand.Intn(200) - 100  // Can deviate vertically

	// Shorter, faster duration (1-2 seconds)
	duration := int64(rand.Intn(1000) + 1000)
	
	// Calculate velocity for smooth interpolation
	distance := math.Sqrt(math.Pow(float64(endX-startX), 2) + math.Pow(float64(endY-startY), 2))
	velocity := distance / float64(duration)

	return TargetTrajectory{
		StartX:    startX,
		StartY:    startY,
		EndX:      endX,
		EndY:      endY,
		StartTime: time.Now().UnixMilli(),
		Duration:  duration,
		Velocity:  velocity,
	}
}

func (gs *GameServer) CalculateCurrentPosition(trajectory TargetTrajectory, currentTime int64) Position {
	// Calculate progress (0.0 to 1.0)
	progress := float64(currentTime - trajectory.StartTime) / float64(trajectory.Duration)
	
	// Clamp progress between 0 and 1
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}

	// Smooth interpolation with slight curve
	smoothProgress := math.Sin(progress * math.Pi / 2)

	x := int(float64(trajectory.StartX) + smoothProgress * float64(trajectory.EndX - trajectory.StartX))
	y := int(float64(trajectory.StartY) + smoothProgress * float64(trajectory.EndY - trajectory.StartY))

	return Position{
		X:           x,
		Y:           y,
		Timestamp:   currentTime,
		ServerTime:  currentTime - gs.serverStartTime,
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

	// Keep only the last 100 positions
	gs.redisClient.ZRemRangeByRank(ctx, REDIS_KEY, 0, -101)
	
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
	// Initialize target at left side of screen
	startPos := Position{
		X:           0,
		Y:           HEIGHT / 2,
		Timestamp:   time.Now().UnixMilli(),
		ServerTime:  0,
	}

	// Movement parameters
	speed := 200.0 // pixels per second
	startTime := time.Now().UnixMilli()

	// Debug logging for initial position
	log.Printf("Target initialized at position: (%d, %d)", startPos.X, startPos.Y)

	// Test Redis connection
	ctx := context.Background()
	if err := gs.redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("Warning: Redis connection failed: %v", err)
	} else {
		log.Printf("Redis connection successful")
	}

	for {
		currentTime := time.Now().UnixMilli()
		elapsedTime := float64(currentTime - startTime) / 1000.0 // Convert to seconds
		
		// Calculate new position (linear movement)
		newX := int(elapsedTime * speed)
		if newX > WIDTH {
			// Reset to start when reaching the right edge
			newX = 0
			startTime = currentTime
			log.Printf("Target reset to start position")
		}

		currentPos := Position{
			X:           newX,
			Y:           HEIGHT / 2,
			Timestamp:   currentTime,
			ServerTime:  currentTime - gs.serverStartTime,
		}

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

func (gs *GameServer) HandleShoot(conn *websocket.Conn, clientShootTime, serverReceivedTime int64, clientPerceivePos Position) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	// Convert client time to server time
	serverTime := serverReceivedTime - gs.serverStartTime
	adjustedClientTime := clientShootTime + serverTime

	// Lag compensation window (250ms)
	lagCompensationWindow := int64(250)

	// Get positions from Redis within the compensation window
	positions, err := gs.getPositionsFromRedis(
		adjustedClientTime - lagCompensationWindow,
		adjustedClientTime + lagCompensationWindow,
	)
	
	if err != nil {
		log.Printf("Error getting positions from Redis: %v", err)
		// Fallback to in-memory positions
		positions = gs.targetPositions
		log.Printf("Using in-memory positions instead")
	}

	// Debug logging
	log.Printf("Shoot attempt - Client pos: (%d, %d), Client Time: %d, Server Time: %d", 
		clientPerceivePos.X, clientPerceivePos.Y, clientShootTime, serverTime)
	log.Printf("Number of positions to check: %d", len(positions))

	// Find the closest position in time to when the client shot
	var closestPos Position
	minTimeDiff := int64(^uint64(0) >> 1) // Max int64

	for _, pos := range positions {
		timeDiff := abs(pos.ServerTime - adjustedClientTime)
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
			gs.mutex.Unlock()
			return
		}

		var data map[string]interface{}
		json.Unmarshal(msg, &data)

		if data["type"].(string) == "shoot" {
			gs.HandleShoot(conn, 
				int64(data["timestamp"].(float64)),
				time.Now().UnixMilli(),
				Position{
					X: int(data["x"].(float64)),
					Y: int(data["y"].(float64)),
				},
			)
		}
	}
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
