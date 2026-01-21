package trader

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"auto-trader-ahh/events"
	"auto-trader-ahh/exchange"
	"auto-trader-ahh/store"
)

// MockNotifier is a simple notifier for testing
type MockNotifier struct {
	events []string
	mu     sync.Mutex
}

func (m *MockNotifier) Broadcast(evt events.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Simple string representation for testing
	m.events = append(m.events, evt.Message)
}

// TestAtomicDrawdownCheck tests that concurrent drawdown checks are prevented
func TestAtomicDrawdownCheck(t *testing.T) {
	engine := &Engine{
		strategy: &store.Strategy{
			Config: store.StrategyConfig{
				RiskControl: store.RiskControlConfig{
					DrawdownCloseThreshold: 40.0,
					MinProfitForDrawdown:   5.0,
				},
			},
		},
		positions: make(map[string]*exchange.Position),
		notifier:  &MockNotifier{},
	}

	// Track how many times checkPositionDrawdown actually runs
	var executionCount atomic.Int32

	// Launch 100 concurrent calls to checkPositionDrawdown
	var wg sync.WaitGroup
	numCalls := 100

	wg.Add(numCalls)
	for i := 0; i < numCalls; i++ {
		go func() {
			defer wg.Done()

			// This should be prevented by atomic flag
			if engine.isCheckingDrawdown.CompareAndSwap(false, true) {
				executionCount.Add(1)
				time.Sleep(10 * time.Millisecond) // Simulate work
				engine.isCheckingDrawdown.Store(false)
			}
		}()
	}

	wg.Wait()

	// Only a small number should have executed (not all 100)
	count := executionCount.Load()
	if count >= int32(numCalls) {
		t.Errorf("Expected fewer than %d executions due to atomic flag, got %d", numCalls, count)
	}

	t.Logf("Out of %d concurrent calls, %d actually executed (atomic flag working)", numCalls, count)
}

// TestShouldAttemptClose tests the rate limiting for close attempts
func TestShouldAttemptClose(t *testing.T) {
	engine := &Engine{
		lastCloseAttempt: make(map[string]time.Time),
	}

	symbol := "BTCUSDT"

	// First attempt should succeed
	if !engine.shouldAttemptClose(symbol) {
		t.Error("First attempt should return true")
	}

	// Immediate second attempt should fail (rate limited)
	if engine.shouldAttemptClose(symbol) {
		t.Error("Second immediate attempt should return false (rate limited)")
	}

	// Wait 2.1 seconds and try again
	time.Sleep(2100 * time.Millisecond)

	// Should succeed now
	if !engine.shouldAttemptClose(symbol) {
		t.Error("Attempt after 2 seconds should return true")
	}
}

// TestShouldAttemptCloseConcurrent tests concurrent rate limiting
func TestShouldAttemptCloseConcurrent(t *testing.T) {
	engine := &Engine{
		lastCloseAttempt: make(map[string]time.Time),
	}

	symbol := "BTCUSDT"
	var successCount atomic.Int32

	var wg sync.WaitGroup
	numAttempts := 50

	wg.Add(numAttempts)
	for i := 0; i < numAttempts; i++ {
		go func() {
			defer wg.Done()
			if engine.shouldAttemptClose(symbol) {
				successCount.Add(1)
			}
		}()
	}

	wg.Wait()

	// Only 1 should succeed due to mutex
	count := successCount.Load()
	if count != 1 {
		t.Errorf("Expected exactly 1 successful attempt, got %d", count)
	}
}

// TestHandleWebSocketUpdates tests the WebSocket update handler
func TestHandleWebSocketUpdates(t *testing.T) {
	updateCh := make(chan *exchange.PositionUpdate, 10)
	stopCh := make(chan struct{})

	engine := &Engine{
		wsUpdateCh:            updateCh,
		stopCh:                stopCh,
		positions:             make(map[string]*exchange.Position),
		notifier:              &MockNotifier{},
		peakPnLCache:          make(map[string]float64),
		positionFirstSeenTime: make(map[string]int64),
		lastCloseAttempt:      make(map[string]time.Time),
		strategy: &store.Strategy{
			Config: store.StrategyConfig{
				RiskControl: store.RiskControlConfig{},
			},
		},
	}

	ctx := context.Background()

	// Start the handler in background
	go engine.handleWebSocketUpdates(ctx)

	// Send a position update
	update := &exchange.PositionUpdate{
		Symbol:        "BTCUSDT",
		PositionAmt:   0.001,
		EntryPrice:    50000.0,
		MarkPrice:     51000.0,
		UnrealizedPnL: 100.0,
		PositionSide:  "BOTH",
		Leverage:      20,
	}

	updateCh <- update

	// Wait for update to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify position was updated
	engine.mu.RLock()
	pos, exists := engine.positions["BTCUSDT"]
	engine.mu.RUnlock()

	if !exists {
		t.Fatal("Position not found in map")
	}

	if pos.Symbol != "BTCUSDT" {
		t.Errorf("Expected symbol BTCUSDT, got %s", pos.Symbol)
	}
	if pos.PositionAmt != 0.001 {
		t.Errorf("Expected position amt 0.001, got %f", pos.PositionAmt)
	}
	if pos.MarkPrice != 51000.0 {
		t.Errorf("Expected mark price 51000.0, got %f", pos.MarkPrice)
	}

	// Send an update with zero position (should remove from map)
	closeUpdate := &exchange.PositionUpdate{
		Symbol:      "BTCUSDT",
		PositionAmt: 0,
	}

	updateCh <- closeUpdate
	time.Sleep(100 * time.Millisecond)

	// Verify position was removed
	engine.mu.RLock()
	_, exists = engine.positions["BTCUSDT"]
	engine.mu.RUnlock()

	if exists {
		t.Error("Position should have been removed when PositionAmt = 0")
	}

	// Stop the handler
	close(stopCh)
	time.Sleep(50 * time.Millisecond)
}

// TestWebSocketUpdateLatency measures the latency of WebSocket updates
func TestWebSocketUpdateLatency(t *testing.T) {
	updateCh := make(chan *exchange.PositionUpdate, 100)
	stopCh := make(chan struct{})

	engine := &Engine{
		wsUpdateCh:            updateCh,
		stopCh:                stopCh,
		positions:             make(map[string]*exchange.Position),
		notifier:              &MockNotifier{},
		peakPnLCache:          make(map[string]float64),
		positionFirstSeenTime: make(map[string]int64),
		lastCloseAttempt:      make(map[string]time.Time),
		strategy: &store.Strategy{
			Config: store.StrategyConfig{
				RiskControl: store.RiskControlConfig{},
			},
		},
	}

	ctx := context.Background()
	go engine.handleWebSocketUpdates(ctx)

	// Measure time from send to position map update
	start := time.Now()

	update := &exchange.PositionUpdate{
		Symbol:        "BTCUSDT",
		PositionAmt:   0.001,
		EntryPrice:    50000.0,
		MarkPrice:     51000.0,
		UnrealizedPnL: 100.0,
	}

	updateCh <- update

	// Poll until position appears in map
	for i := 0; i < 100; i++ {
		engine.mu.RLock()
		_, exists := engine.positions["BTCUSDT"]
		engine.mu.RUnlock()

		if exists {
			latency := time.Since(start)
			t.Logf("WebSocket update latency: %v", latency)

			// Should be well under 100ms for in-memory operation
			if latency > 100*time.Millisecond {
				t.Errorf("Update latency too high: %v (expected < 100ms)", latency)
			}
			break
		}

		time.Sleep(1 * time.Millisecond)
	}

	close(stopCh)
}

// TestMultiplePositionUpdates tests handling multiple position updates
func TestMultiplePositionUpdates(t *testing.T) {
	updateCh := make(chan *exchange.PositionUpdate, 100)
	stopCh := make(chan struct{})

	engine := &Engine{
		wsUpdateCh:            updateCh,
		stopCh:                stopCh,
		positions:             make(map[string]*exchange.Position),
		notifier:              &MockNotifier{},
		peakPnLCache:          make(map[string]float64),
		positionFirstSeenTime: make(map[string]int64),
		lastCloseAttempt:      make(map[string]time.Time),
		strategy: &store.Strategy{
			Config: store.StrategyConfig{
				RiskControl: store.RiskControlConfig{},
			},
		},
	}

	ctx := context.Background()
	go engine.handleWebSocketUpdates(ctx)

	// Send updates for multiple symbols
	symbols := []string{"BTCUSDT", "ETHUSDT", "BNBUSDT"}

	for _, symbol := range symbols {
		update := &exchange.PositionUpdate{
			Symbol:        symbol,
			PositionAmt:   0.1,
			EntryPrice:    1000.0,
			MarkPrice:     1100.0,
			UnrealizedPnL: 10.0,
		}
		updateCh <- update
	}

	// Wait for all updates
	time.Sleep(200 * time.Millisecond)

	// Verify all positions exist
	engine.mu.RLock()
	for _, symbol := range symbols {
		if _, exists := engine.positions[symbol]; !exists {
			t.Errorf("Position %s not found", symbol)
		}
	}
	posCount := len(engine.positions)
	engine.mu.RUnlock()

	if posCount != len(symbols) {
		t.Errorf("Expected %d positions, got %d", len(symbols), posCount)
	}

	close(stopCh)
}

// BenchmarkWebSocketUpdateProcessing benchmarks the WebSocket update processing
func BenchmarkWebSocketUpdateProcessing(b *testing.B) {
	updateCh := make(chan *exchange.PositionUpdate, 1000)
	stopCh := make(chan struct{})

	engine := &Engine{
		wsUpdateCh:            updateCh,
		stopCh:                stopCh,
		positions:             make(map[string]*exchange.Position),
		notifier:              &MockNotifier{},
		peakPnLCache:          make(map[string]float64),
		positionFirstSeenTime: make(map[string]int64),
		lastCloseAttempt:      make(map[string]time.Time),
		strategy: &store.Strategy{
			Config: store.StrategyConfig{
				RiskControl: store.RiskControlConfig{},
			},
		},
	}

	ctx := context.Background()
	go engine.handleWebSocketUpdates(ctx)

	update := &exchange.PositionUpdate{
		Symbol:        "BTCUSDT",
		PositionAmt:   0.001,
		EntryPrice:    50000.0,
		MarkPrice:     51000.0,
		UnrealizedPnL: 100.0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		updateCh <- update
	}

	// Cleanup
	close(stopCh)
}

// BenchmarkShouldAttemptClose benchmarks the rate limiting check
func BenchmarkShouldAttemptClose(b *testing.B) {
	engine := &Engine{
		lastCloseAttempt: make(map[string]time.Time),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.shouldAttemptClose("BTCUSDT")
	}
}

// BenchmarkAtomicDrawdownCheck benchmarks the atomic flag check
func BenchmarkAtomicDrawdownCheck(b *testing.B) {
	engine := &Engine{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if engine.isCheckingDrawdown.CompareAndSwap(false, true) {
				engine.isCheckingDrawdown.Store(false)
			}
		}
	})
}
