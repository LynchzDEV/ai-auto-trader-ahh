package trader

import (
	"strings"
	"testing"
)

// TestSLOrderError4509Detection tests that the error -4509 is correctly detected
// as a "position closed" error, which should stop SL retry attempts
func TestSLOrderError4509Detection(t *testing.T) {
	testCases := []struct {
		name            string
		errorMsg        string
		shouldStopRetry bool
	}{
		{
			name:            "Standard -4509 error",
			errorMsg:        `API error (status 400): {"code":-4509,"msg":"Time in Force (TIF) GTE can only be used with open positions. Please ensure that positions are available."}`,
			shouldStopRetry: true,
		},
		{
			name:            "Short -4509 error",
			errorMsg:        `-4509: GTE requires open positions`,
			shouldStopRetry: true,
		},
		{
			name:            "Contains open positions message",
			errorMsg:        `Some error about open positions not being available`,
			shouldStopRetry: true,
		},
		{
			name:            "Different error - insufficient margin",
			errorMsg:        `API error (status 400): {"code":-2019,"msg":"Margin is insufficient."}`,
			shouldStopRetry: false,
		},
		{
			name:            "Network error",
			errorMsg:        `connection reset by peer`,
			shouldStopRetry: false,
		},
		{
			name:            "Rate limit error",
			errorMsg:        `API error (status 429): rate limit exceeded`,
			shouldStopRetry: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This is the exact check we use in placeStopLossOnly
			isPositionClosed := strings.Contains(tc.errorMsg, "-4509") ||
				strings.Contains(tc.errorMsg, "open positions")

			if isPositionClosed != tc.shouldStopRetry {
				t.Errorf("Expected shouldStopRetry=%v for error '%s', got %v",
					tc.shouldStopRetry, tc.errorMsg, isPositionClosed)
			}
		})
	}
}

// TestSLRetryPreventionLogic tests the logic that prevents SL retry
// when position no longer exists
func TestSLRetryPreventionLogic(t *testing.T) {
	// This test validates the retry prevention logic without actual Binance calls

	type positionState struct {
		symbol      string
		positionAmt float64
	}

	testCases := []struct {
		name         string
		positions    []positionState
		targetSymbol string
		shouldRetry  bool
	}{
		{
			name: "Position exists - should retry",
			positions: []positionState{
				{symbol: "BTCUSDT", positionAmt: 0.001},
				{symbol: "AIAUSDT", positionAmt: -381.0},
			},
			targetSymbol: "AIAUSDT",
			shouldRetry:  true,
		},
		{
			name: "Position closed (amt=0) - should not retry",
			positions: []positionState{
				{symbol: "BTCUSDT", positionAmt: 0.001},
				{symbol: "AIAUSDT", positionAmt: 0.0}, // Closed
			},
			targetSymbol: "AIAUSDT",
			shouldRetry:  false,
		},
		{
			name: "Position not in list - should not retry",
			positions: []positionState{
				{symbol: "BTCUSDT", positionAmt: 0.001},
			},
			targetSymbol: "AIAUSDT",
			shouldRetry:  false,
		},
		{
			name:         "Empty positions - should not retry",
			positions:    []positionState{},
			targetSymbol: "AIAUSDT",
			shouldRetry:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate the position check logic from placeStopLossOnly
			positionExists := false
			for _, pos := range tc.positions {
				if pos.symbol == tc.targetSymbol && pos.positionAmt != 0 {
					positionExists = true
					break
				}
			}

			if positionExists != tc.shouldRetry {
				t.Errorf("Expected shouldRetry=%v for %s, got positionExists=%v",
					tc.shouldRetry, tc.targetSymbol, positionExists)
			}
		})
	}
}

// TestRaceConditionScenario documents the race condition that was fixed
func TestRaceConditionScenario(t *testing.T) {
	/*
		This test documents the race condition that was occurring:

		Timeline:
		1. Position opened (AIAUSDT SHORT @ 0.2723)
		2. placeStopLossOnly goroutine starts
		3. WebSocket receives ORDER_TRADE_UPDATE - position is closing
		4. placeStopLossOnly attempt 1 fails with -4509 (position already closed!)
		5. Position is now 0.0 (confirmed by WebSocket)
		6. placeStopLossOnly attempt 2 fails with -4509
		7. placeStopLossOnly attempt 3 fails with -4509
		8. Bot unnecessarily tries to "close position for safety"

		Fix applied:
		- Before each SL retry, check if position still exists
		- If error is -4509, stop retrying immediately
	*/

	// Simulate the timeline
	type event struct {
		time  int // milliseconds
		event string
	}

	timeline := []event{
		{0, "Position opened: AIAUSDT SHORT -381 @ 0.2723"},
		{10, "placeStopLossOnly goroutine started"},
		{50, "WebSocket: ORDER_TRADE_UPDATE (position filling)"},
		{100, "WebSocket: position = -312 (partial fill of close)"},
		{150, "WebSocket: position = 0 (fully closed!)"},
		{200, "SL attempt 1: -4509 error (no open position)"},
		// OLD behavior: would continue retrying
		// NEW behavior: detects -4509, stops immediately
	}

	t.Log("Race condition timeline that was fixed:")
	for _, e := range timeline {
		t.Logf("  +%d ms: %s", e.time, e.event)
	}

	t.Log("")
	t.Log("OLD behavior: 3 failed SL attempts, then incorrect 'close position for safety'")
	t.Log("NEW behavior: First -4509 error stops retry loop immediately")
}
