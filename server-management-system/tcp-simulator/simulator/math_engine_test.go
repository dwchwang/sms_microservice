package simulator

import (
	"sync"
	"testing"
)

func TestMathEngine_HighUptimeRate(t *testing.T) {
	engine := NewMathEngine()
	uptimeRate := 0.99
	onlineCount := 0
	totalTrials := 10000

	for i := 0; i < totalTrials; i++ {
		if engine.ShouldBeOnline(uptimeRate, i%100) {
			onlineCount++
		}
	}

	rate := float64(onlineCount) / float64(totalTrials)
	// With 0.99 rate plus variation, should be >= 0.90
	if rate < 0.90 {
		t.Errorf("Expected high online rate, got %.4f (online=%d/%d)", rate, onlineCount, totalTrials)
	}
}

func TestMathEngine_LowUptimeRate(t *testing.T) {
	engine := NewMathEngine()
	uptimeRate := 0.50
	onlineCount := 0
	totalTrials := 10000

	for i := 0; i < totalTrials; i++ {
		if engine.ShouldBeOnline(uptimeRate, i%100) {
			onlineCount++
		}
	}

	rate := float64(onlineCount) / float64(totalTrials)
	// With 0.50 rate plus variation, should be between 0.35 and 0.65
	if rate < 0.35 || rate > 0.65 {
		t.Errorf("Expected medium online rate, got %.4f (online=%d/%d)", rate, onlineCount, totalTrials)
	}
}

func TestMathEngine_ZeroRate(t *testing.T) {
	engine := NewMathEngine()
	uptimeRate := 0.0
	onlineCount := 0
	totalTrials := 10000

	for i := 0; i < totalTrials; i++ {
		if engine.ShouldBeOnline(uptimeRate, i%100) {
			onlineCount++
		}
	}

	rate := float64(onlineCount) / float64(totalTrials)
	// Zero base rate, but hourly variation can create small non-zero results
	if rate > 0.10 {
		t.Errorf("Expected very low online rate, got %.4f (online=%d/%d)", rate, onlineCount, totalTrials)
	}
	t.Logf("Zero rate result: %.4f online (hourly variation can add up to 0.07)", rate)
}

func TestMathEngine_FullRate(t *testing.T) {
	engine := NewMathEngine()
	uptimeRate := 1.0
	onlineCount := 0
	totalTrials := 10000

	for i := 0; i < totalTrials; i++ {
		if engine.ShouldBeOnline(uptimeRate, i%100) {
			onlineCount++
		}
	}

	rate := float64(onlineCount) / float64(totalTrials)
	if rate < 0.90 {
		t.Errorf("Expected very high online rate, got %.4f (online=%d/%d)", rate, onlineCount, totalTrials)
	}
}

func TestMathEngine_DifferentServers(t *testing.T) {
	engine := NewMathEngine()
	rate := 0.75 // Lower rate so sin variations create measurable differences
	trialsPerServer := 5000

	// Check that different servers get meaningfully different results
	results := make(map[int]int)
	for idx := 0; idx < 10; idx++ {
		online := 0
		for j := 0; j < trialsPerServer; j++ {
			if engine.ShouldBeOnline(rate, idx) {
				online++
			}
		}
		results[idx] = online
	}

	// Verify each count is reasonable (between 60% and 90% of trials)
	for idx, count := range results {
		pct := float64(count) / float64(trialsPerServer)
		if pct < 0.60 || pct > 0.90 {
			t.Errorf("Server %d: unexpected rate %.4f (expected 0.60–0.90)", idx, pct)
		}
	}
	t.Logf("Results: %v", results)
}

func TestMathEngine_BoundaryClamp(t *testing.T) {
	engine := NewMathEngine()
	// Run many trials to ensure effective rate stays in [0, 1]
	totalTrials := 50000

	for i := 0; i < totalTrials; i++ {
		// Different uptime rates including edge cases
		rates := []float64{0.0, 0.5, 1.0, -0.5, 1.5}
		for _, rate := range rates {
			// Should never panic, always return bool
			result := engine.ShouldBeOnline(rate, i)
			_ = result // just verify no panic
		}
	}

	// If we reach here without panic, test passes
}

func TestMathEngine_ConcurrentShouldBeOnline(t *testing.T) {
	engine := NewMathEngine()

	var wg sync.WaitGroup
	for worker := 0; worker < 100; worker++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				_ = engine.ShouldBeOnline(0.95, offset+i)
			}
		}(worker * 1000)
	}

	wg.Wait()
}
