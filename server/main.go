package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
)

// Position struct represents target position with timestamp
type Position struct {
	X, Y      int
	Timestamp int64
}

// GameServer struct
type GameServer struct {
	redis           *redis.Client
	targetPositions []Position
	mu              sync.RWMutex
}

// NewGameServer initializes a game server
func NewGameServer() *GameServer {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	return &GameServer{
		redis: rdb,
		targetPositions: make([]Position, 0),
	}
}

// MoveTarget moves the target and stores history in Redis
func (gs *GameServer) MoveTarget() {
	for {
		pos := Position{
			X: rand.Intn(10),
			Y: rand.Intn(10),
			Timestamp: time.Now().UnixMilli(),
		}

		gs.mu.Lock()
		gs.targetPositions = append(gs.targetPositions, pos)
		if len(gs.targetPositions) > 10 {
			gs.targetPositions = gs.targetPositions[1:]
		}
		gs.mu.Unlock()

		// Store in Redis
		data, _ := json.Marshal(pos)
		gs.redis.LPush(context.Background(), "target_positions", data)
		gs.redis.LTrim(context.Background(), "target_positions", 0, 9)

		time.Sleep(50 * time.Millisecond)
	}
}

// HandleShoot performs lag compensation and hit detection
func (gs *GameServer) HandleShoot(shootTime int64, clientPerceivePos Position) bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	// Binary search for nearest timestamp
	l, r := 0, len(gs.targetPositions)-1
	for l <= r {
		mid := (l + r) / 2
		if abs(gs.targetPositions[mid].Timestamp - shootTime) < 100 {
			return distance(gs.targetPositions[mid], clientPerceivePos) < 2
		}
		if gs.targetPositions[mid].Timestamp < shootTime {
			l = mid + 1
		} else {
			r = mid - 1
		}
	}
	return false
}

// WebSocket upgrade
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WebSocket handler
func (gs *GameServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade failed:", err)
		return
	}
	defer conn.Close()

	for {
		var clientData struct {
			ShootTime        int64    `json:"shoot_time"`
			ClientPerceivePos Position `json:"position"`
		}

		err := conn.ReadJSON(&clientData)
		if err != nil {
			log.Println("WebSocket read error:", err)
			break
		}

		hit := gs.HandleShoot(clientData.ShootTime, clientData.ClientPerceivePos)
		response := map[string]bool{"hit": hit}

		err = conn.WriteJSON(response)
		if err != nil {
			log.Println("WebSocket write error:", err)
			break
		}
	}
}

// Utility functions
func distance(a, b Position) float64 {
	return math.Sqrt(math.Pow(float64(a.X-b.X), 2) + math.Pow(float64(a.Y-b.Y), 2))
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// Main function
func main() {
	gameServer := NewGameServer()
	go gameServer.MoveTarget()

	http.HandleFunc("/ws", gameServer.handleWebSocket)

	fmt.Println("Lag Compensation Server Ready on ws://localhost:8080/ws")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
