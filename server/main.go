package main

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/go-redis/redis/v8"
)

type Position struct {
	X, Y     int
	Timestamp int64
}

type GameServer struct {
	redis *redis.Client
	targetPositions []Position
}

func NewGameServer() *GameServer {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	return &GameServer{
		redis: rdb,
		targetPositions: make([]Position, 0),
	}
}

func (gs *GameServer) MoveTarget() {
	for {
		pos := Position{
			X: rand.Intn(10),
			Y: rand.Intn(10),
			Timestamp: time.Now().UnixMilli(),
		}
		gs.targetPositions = append(gs.targetPositions, pos)
		
		// Keep last 10 positions
		if len(gs.targetPositions) > 10 {
			gs.targetPositions = gs.targetPositions[1:]
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func (gs *GameServer) HandleShoot(shootTime int64, clientPerceivePos Position) bool {
	// Rewind to client's perceived moment
	for _, historicalPos := range gs.targetPositions {
		if abs(historicalPos.Timestamp - shootTime) < 100 {
			// Simple hit detection
			return distance(historicalPos, clientPerceivePos) < 2
		}
	}
	return false
}

func distance(a, b Position) float64 {
	// Euclidean distance
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

	// WebSocket setup would go here
	fmt.Println("Lag Compensation Server Ready")
}
