package simulator

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// MathEngine computes whether a simulated server should be online
// based on uptime_rate, hourly sin wave variation, and server-specific phase.
type MathEngine struct {
	rng *rand.Rand
	mu  sync.Mutex
}

// NewMathEngine creates a new MathEngine
func NewMathEngine() *MathEngine {
	return &MathEngine{
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// ShouldBeOnline determines if a server should be online at the current time.
//
// Formula:
//
//	effectiveRate = uptimeRate + sin(hour*π/12)*0.05 + sin((hour+serverPhase)*π/24)*0.02
//	clamp to [0, 1]
//	online = random() < effectiveRate
//
// Parameters:
//   - uptimeRate: base probability (0.0 to 1.0), e.g. 0.95 = 95% likely to be ON
//   - serverIndex: used to derive a server-specific phase offset
func (e *MathEngine) ShouldBeOnline(uptimeRate float64, serverIndex int) bool {
	hour := float64(time.Now().Hour())

	// Hourly sin wave variation creates natural peaks/troughs during the day
	hourlyVariation := math.Sin(hour*math.Pi/12) * 0.05

	// Server-specific phase ensures not all servers toggle simultaneously
	serverPhase := float64(serverIndex) * 0.1
	serverVariation := math.Sin((hour+serverPhase)*math.Pi/24) * 0.02

	// Compute effective rate and clamp to [0, 1]
	effectiveRate := uptimeRate + hourlyVariation + serverVariation
	effectiveRate = math.Max(0, math.Min(1, effectiveRate))

	e.mu.Lock()
	r := e.rng.Float64()
	e.mu.Unlock()

	return r < effectiveRate
}
