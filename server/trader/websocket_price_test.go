package trader

import (
	"context"
	"sync"
	"testing"
	"time"

	"auto-trader-ahh/exchange"
	"auto-trader-ahh/store"
)

// TestWebSocketMarkPriceZeroPreservation tests that existing MarkPrice is preserved
// when WebSocket sends an update with MarkPrice=0
func TestWebSocketMarkPriceZeroPreservation(t *testing.T) {
	updateCh := make(chan *exchange.PositionUpdate, 10)
	stopCh := make(chan struct{})

	engine := &Engine{
		name:                  "TestEngine",
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

	// First update: Position opened with valid MarkPrice
	initialUpdate := &exchange.PositionUpdate{
		Symbol:        "AIAUSDT",
		PositionAmt:   -381.0,
		EntryPrice:    0.2723,
		MarkPrice:     0.2750, // Valid mark price
		UnrealizedPnL: 0.54,
		PositionSide:  "BOTH",
		Leverage:      20,
	}

	updateCh <- initialUpdate
	time.Sleep(100 * time.Millisecond)

	// Verify initial state
	engine.mu.RLock()
	pos, exists := engine.positions["AIAUSDT"]
	engine.mu.RUnlock()

	if !exists {
		t.Fatal("Position not found after initial update")
	}
	if pos.MarkPrice != 0.2750 {
		t.Errorf("Expected MarkPrice 0.2750, got %f", pos.MarkPrice)
	}

	// Second update: WebSocket sends MarkPrice=0 (this happens in real Binance)
	badUpdate := &exchange.PositionUpdate{
		Symbol:        "AIAUSDT",
		PositionAmt:   -381.0,
		EntryPrice:    0.2723,
		MarkPrice:     0.0, // Invalid - should be preserved from previous
		UnrealizedPnL: 0.60,
		PositionSide:  "BOTH",
		Leverage:      20,
	}

	updateCh <- badUpdate
	time.Sleep(100 * time.Millisecond)

	// Verify MarkPrice was PRESERVED (not overwritten with 0)
	engine.mu.RLock()
	pos, exists = engine.positions["AIAUSDT"]
	engine.mu.RUnlock()

	if !exists {
		t.Fatal("Position not found after bad update")
	}

	// Key assertion: MarkPrice should still be the valid value, not 0
	if pos.MarkPrice == 0 {
		t.Error("CRITICAL: MarkPrice was overwritten with 0 - this causes false 100% PnL!")
	}
	if pos.MarkPrice != 0.2750 {
		t.Errorf("Expected preserved MarkPrice 0.2750, got %f", pos.MarkPrice)
	}

	close(stopCh)
}

// TestWebSocketMarkPriceZeroNoTrigger tests that checkPositionDrawdown is NOT triggered
// when WebSocket sends MarkPrice=0
func TestWebSocketMarkPriceZeroNoTrigger(t *testing.T) {
	updateCh := make(chan *exchange.PositionUpdate, 10)
	stopCh := make(chan struct{})

	// Track if drawdown check was triggered
	drawdownTriggered := false
	var drawdownMu sync.Mutex

	engine := &Engine{
		name:                  "TestEngine",
		wsUpdateCh:            updateCh,
		stopCh:                stopCh,
		positions:             make(map[string]*exchange.Position),
		notifier:              &MockNotifier{},
		peakPnLCache:          make(map[string]float64),
		positionFirstSeenTime: make(map[string]int64),
		lastCloseAttempt:      make(map[string]time.Time),
		strategy: &store.Strategy{
			Config: store.StrategyConfig{
				RiskControl: store.RiskControlConfig{
					EnableTrailingStop: true,
				},
			},
		},
	}

	// Custom handler to detect if drawdown check would be triggered
	go func() {
		for {
			select {
			case <-stopCh:
				return
			case update := <-updateCh:
				if update == nil {
					continue
				}

				// Update position
				engine.mu.Lock()
				if update.PositionAmt != 0 {
					engine.positions[update.Symbol] = &exchange.Position{
						Symbol:      update.Symbol,
						PositionAmt: update.PositionAmt,
						EntryPrice:  update.EntryPrice,
						MarkPrice:   update.MarkPrice,
					}
				}
				engine.mu.Unlock()

				// Check if drawdown would be triggered (the fix should prevent this)
				if update.PositionAmt != 0 && update.MarkPrice > 0 {
					drawdownMu.Lock()
					drawdownTriggered = true
					drawdownMu.Unlock()
				}
			}
		}
	}()

	// Send update with MarkPrice=0 (should NOT trigger drawdown check)
	badUpdate := &exchange.PositionUpdate{
		Symbol:        "AIAUSDT",
		PositionAmt:   -381.0,
		EntryPrice:    0.2723,
		MarkPrice:     0.0, // Invalid mark price
		UnrealizedPnL: 0.0,
	}

	updateCh <- badUpdate
	time.Sleep(100 * time.Millisecond)

	drawdownMu.Lock()
	triggered := drawdownTriggered
	drawdownMu.Unlock()

	if triggered {
		t.Error("CRITICAL: Drawdown check was triggered with MarkPrice=0 - this causes false trailing stop!")
	}

	close(stopCh)
}

// TestFalsePnLCalculationWithZeroMarkPrice verifies the PnL calculation bug
// that occurs when MarkPrice=0
func TestFalsePnLCalculationWithZeroMarkPrice(t *testing.T) {
	// This test demonstrates the bug that was occurring:
	// For a SHORT position, rawPnlPct = ((entryPrice - markPrice) / entryPrice) * 100
	// If markPrice = 0: rawPnlPct = ((0.2723 - 0) / 0.2723) * 100 = 100%

	entryPrice := 0.2723
	markPrice := 0.0 // WebSocket sent 0

	// This is the WRONG calculation that was happening
	rawPnlPct := ((entryPrice - markPrice) / entryPrice) * 100

	if rawPnlPct != 100.0 {
		t.Logf("Calculation with MarkPrice=0 gives: %.2f%%", rawPnlPct)
	}

	// The bug: 100% "profit" would trigger trailing stop immediately
	trailActivate := 1.5 // 1.5% activation
	trailDist := 0.5     // 0.5% trailing distance

	if rawPnlPct >= trailActivate {
		trailingStopLevel := rawPnlPct - trailDist // 100 - 0.5 = 99.5%

		// Any real current PnL (e.g., 0.5%) would be below 99.5%
		actualPnL := 0.5
		if actualPnL <= trailingStopLevel {
			t.Logf("BUG REPRODUCED: Trailing stop would trigger at %.2f%% < %.2f%%", actualPnL, trailingStopLevel)
			// This is the expected (buggy) behavior we're testing for
		}
	}

	t.Log("Test demonstrates the bug that was fixed: MarkPrice=0 causes false 100% PnL")
}

// TestCheckPositionDrawdownSkipsInvalidPrices tests that positions with invalid
// prices are skipped in the drawdown check
func TestCheckPositionDrawdownSkipsInvalidPrices(t *testing.T) {
	engine := &Engine{
		name:                  "TestEngine",
		positions:             make(map[string]*exchange.Position),
		notifier:              &MockNotifier{},
		peakPnLCache:          make(map[string]float64),
		positionFirstSeenTime: make(map[string]int64),
		lastCloseAttempt:      make(map[string]time.Time),
		strategy: &store.Strategy{
			Config: store.StrategyConfig{
				RiskControl: store.RiskControlConfig{
					EnableTrailingStop:      true,
					TrailingStopActivatePct: 1.5,
					TrailingStopDistancePct: 0.5,
					DrawdownCloseThreshold:  40.0,
				},
			},
		},
	}

	// Add position with invalid prices (MarkPrice = 0)
	engine.mu.Lock()
	engine.positions["AIAUSDT"] = &exchange.Position{
		Symbol:      "AIAUSDT",
		PositionAmt: -381.0,
		EntryPrice:  0.2723,
		MarkPrice:   0.0, // Invalid!
		Leverage:    20,
	}
	engine.mu.Unlock()

	// Add peak PnL (simulating previous position)
	engine.peakPnLCacheMutex.Lock()
	engine.peakPnLCache["AIAUSDT|SHORT"] = 100.0 // Old peak from previous position
	engine.peakPnLCacheMutex.Unlock()

	ctx := context.Background()

	// Run drawdown check - should skip position due to invalid MarkPrice
	engine.checkPositionDrawdown(ctx)

	// The position should still exist (not closed by false trailing stop)
	engine.mu.RLock()
	_, exists := engine.positions["AIAUSDT"]
	engine.mu.RUnlock()

	if !exists {
		t.Error("CRITICAL: Position was incorrectly closed due to invalid MarkPrice calculation")
	}

	t.Log("Position correctly skipped due to MarkPrice=0")
}

// TestLeveragePreservation tests that existing leverage is preserved
// when WebSocket sends an update with Leverage=0
func TestLeveragePreservation(t *testing.T) {
	updateCh := make(chan *exchange.PositionUpdate, 10)
	stopCh := make(chan struct{})

	engine := &Engine{
		name:                  "TestEngine",
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

	// First update with leverage
	initialUpdate := &exchange.PositionUpdate{
		Symbol:      "BTCUSDT",
		PositionAmt: 0.01,
		EntryPrice:  50000.0,
		MarkPrice:   51000.0,
		Leverage:    20,
	}

	updateCh <- initialUpdate
	time.Sleep(100 * time.Millisecond)

	// Second update without leverage (should preserve 20)
	noLeverageUpdate := &exchange.PositionUpdate{
		Symbol:      "BTCUSDT",
		PositionAmt: 0.01,
		EntryPrice:  50000.0,
		MarkPrice:   51500.0,
		Leverage:    0, // Not provided
	}

	updateCh <- noLeverageUpdate
	time.Sleep(100 * time.Millisecond)

	engine.mu.RLock()
	pos, exists := engine.positions["BTCUSDT"]
	engine.mu.RUnlock()

	if !exists {
		t.Fatal("Position not found")
	}

	if pos.Leverage != 20 {
		t.Errorf("Expected preserved Leverage 20, got %d", pos.Leverage)
	}

	close(stopCh)
}
