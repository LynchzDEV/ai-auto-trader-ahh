package trader

import (
	"testing"
	"time"
)

// TestRSIConstants verifies RSI thresholds match research
// Reference: https://indicatorvault.com/5-basic-pullback-trading-strategy-with-the-rsi/
// "In an uptrend, look for RSI to pull back toward the 40–50 zone"
// "In a downtrend, watch RSI rise toward 50–60 before turning lower"
func TestRSIConstants(t *testing.T) {
	// LONG pullback zone should be 40-50
	if RSILongPullbackLower != 40.0 {
		t.Errorf("RSILongPullbackLower = %.1f, want 40.0", RSILongPullbackLower)
	}
	if RSILongPullbackUpper != 50.0 {
		t.Errorf("RSILongPullbackUpper = %.1f, want 50.0", RSILongPullbackUpper)
	}
	if RSILongPullbackIdeal != 45.0 {
		t.Errorf("RSILongPullbackIdeal = %.1f, want 45.0", RSILongPullbackIdeal)
	}

	// SHORT pullback zone should be 50-60
	if RSIShortPullbackLower != 50.0 {
		t.Errorf("RSIShortPullbackLower = %.1f, want 50.0", RSIShortPullbackLower)
	}
	if RSIShortPullbackUpper != 60.0 {
		t.Errorf("RSIShortPullbackUpper = %.1f, want 60.0", RSIShortPullbackUpper)
	}
	if RSIShortPullbackIdeal != 55.0 {
		t.Errorf("RSIShortPullbackIdeal = %.1f, want 55.0", RSIShortPullbackIdeal)
	}

	// Extreme levels
	if RSIOverbought != 70.0 {
		t.Errorf("RSIOverbought = %.1f, want 70.0", RSIOverbought)
	}
	if RSIOversold != 30.0 {
		t.Errorf("RSIOversold = %.1f, want 30.0", RSIOversold)
	}
}

// TestSignalStoreBasicOperations tests basic CRUD operations
func TestSignalStoreBasicOperations(t *testing.T) {
	store := NewSignalStore(30) // 30 minute expiry

	// Test AddSignal and GetSignal
	signal := &Signal{
		Symbol:       "BTCUSDT",
		Direction:    "LONG",
		EntryQuality: "GOOD",
		Confidence:   75,
		Reasoning:    "Test signal",
	}

	store.AddSignal(signal)

	retrieved := store.GetSignal("BTCUSDT")
	if retrieved == nil {
		t.Fatal("GetSignal returned nil for existing signal")
	}

	if retrieved.Symbol != "BTCUSDT" {
		t.Errorf("Symbol = %s, want BTCUSDT", retrieved.Symbol)
	}
	if retrieved.Direction != "LONG" {
		t.Errorf("Direction = %s, want LONG", retrieved.Direction)
	}
	if retrieved.Status != SignalWaiting {
		t.Errorf("Status = %s, want WAITING_TRIGGER", retrieved.Status)
	}

	// Test GetSignal for non-existent symbol
	nonExistent := store.GetSignal("ETHUSDT")
	if nonExistent != nil {
		t.Error("GetSignal should return nil for non-existent symbol")
	}

	// Test RemoveSignal
	store.RemoveSignal("BTCUSDT")
	removed := store.GetSignal("BTCUSDT")
	if removed != nil {
		t.Error("Signal should be nil after removal")
	}
}

// TestSignalStoreExpiry tests that expired signals are not returned
func TestSignalStoreExpiry(t *testing.T) {
	store := NewSignalStore(1) // 1 minute expiry for testing

	signal := &Signal{
		Symbol:       "BTCUSDT",
		Direction:    "LONG",
		EntryQuality: "GOOD",
		CreatedAt:    time.Now().Add(-2 * time.Minute), // Created 2 minutes ago
		ExpiresAt:    time.Now().Add(-1 * time.Minute), // Expired 1 minute ago
		Status:       SignalWaiting,
	}

	store.AddSignal(signal)

	// Expired signal should not be returned
	retrieved := store.GetSignal("BTCUSDT")
	if retrieved != nil {
		t.Error("GetSignal should return nil for expired signal")
	}
}

// TestSignalStoreHasActiveSignal tests active signal detection
func TestSignalStoreHasActiveSignal(t *testing.T) {
	store := NewSignalStore(30)

	// No signal yet
	if store.HasActiveSignal("BTCUSDT") {
		t.Error("HasActiveSignal should return false when no signal exists")
	}

	// Add active signal
	signal := &Signal{
		Symbol:    "BTCUSDT",
		Direction: "LONG",
		Status:    SignalWaiting,
	}
	store.AddSignal(signal)

	if !store.HasActiveSignal("BTCUSDT") {
		t.Error("HasActiveSignal should return true for waiting signal")
	}

	// Update to triggered
	store.UpdateSignalStatus("BTCUSDT", SignalTriggered)
	if !store.HasActiveSignal("BTCUSDT") {
		t.Error("HasActiveSignal should return true for triggered signal")
	}

	// Update to executed
	store.UpdateSignalStatus("BTCUSDT", SignalExecuted)
	if store.HasActiveSignal("BTCUSDT") {
		t.Error("HasActiveSignal should return false for executed signal")
	}
}

// TestSignalStoreTriggerConditions tests condition tracking
func TestSignalStoreTriggerConditions(t *testing.T) {
	store := NewSignalStore(30)

	signal := &Signal{
		Symbol:    "BTCUSDT",
		Direction: "LONG",
		Status:    SignalWaiting,
		TriggerConditions: []TriggerCondition{
			{Type: TriggerPullbackEMA9, Description: "Wait for pullback", Met: false},
			{Type: TriggerRSIBelow, Description: "Wait for RSI below 50", Met: false},
		},
	}
	store.AddSignal(signal)

	// Initially no conditions met
	if store.AllConditionsMet("BTCUSDT") {
		t.Error("AllConditionsMet should return false initially")
	}

	// Mark first condition met
	store.MarkConditionMet("BTCUSDT", TriggerPullbackEMA9)
	if store.AllConditionsMet("BTCUSDT") {
		t.Error("AllConditionsMet should return false with only 1/2 conditions met")
	}

	// Mark second condition met
	store.MarkConditionMet("BTCUSDT", TriggerRSIBelow)
	if !store.AllConditionsMet("BTCUSDT") {
		t.Error("AllConditionsMet should return true when all conditions met")
	}
}

// TestSignalStoreNoConditions tests signals with no trigger conditions
func TestSignalStoreNoConditions(t *testing.T) {
	store := NewSignalStore(30)

	signal := &Signal{
		Symbol:            "BTCUSDT",
		Direction:         "LONG",
		Status:            SignalWaiting,
		TriggerConditions: []TriggerCondition{}, // No conditions
	}
	store.AddSignal(signal)

	// No conditions = always ready
	if !store.AllConditionsMet("BTCUSDT") {
		t.Error("AllConditionsMet should return true when no conditions exist")
	}
}

// TestCreateSignalFromAssessment tests signal creation from AI assessment
func TestCreateSignalFromAssessment(t *testing.T) {
	tests := []struct {
		name          string
		direction     string
		entryQuality  string
		waitFor       []string
		expectedStatus SignalStatus
		expectedConditionCount int
	}{
		{
			name:          "EXCELLENT entry - immediate trigger",
			direction:     "LONG",
			entryQuality:  "EXCELLENT",
			waitFor:       []string{},
			expectedStatus: SignalTriggered,
			expectedConditionCount: 0,
		},
		{
			name:          "GOOD entry with EMA pullback",
			direction:     "LONG",
			entryQuality:  "GOOD",
			waitFor:       []string{"pullback_to_ema9"},
			expectedStatus: SignalWaiting,
			expectedConditionCount: 1,
		},
		{
			name:          "WAIT entry with multiple conditions",
			direction:     "LONG",
			entryQuality:  "WAIT",
			waitFor:       []string{"pullback_to_ema9", "rsi_long_pullback", "bullish_candle"},
			expectedStatus: SignalWaiting,
			expectedConditionCount: 3,
		},
		{
			name:          "SHORT with RSI pullback",
			direction:     "SHORT",
			entryQuality:  "WAIT",
			waitFor:       []string{"rsi_short_pullback"},
			expectedStatus: SignalWaiting,
			expectedConditionCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal := CreateSignalFromAssessment(
				"BTCUSDT",
				tt.direction,
				tt.entryQuality,
				75,
				tt.waitFor,
				"Test reasoning",
				3.0,  // stopLossPct
				9.0,  // takeProfitPct
				50000.0, // currentPrice
				30,   // expiryMins
			)

			if signal.Status != tt.expectedStatus {
				t.Errorf("Status = %s, want %s", signal.Status, tt.expectedStatus)
			}

			if len(signal.TriggerConditions) != tt.expectedConditionCount {
				t.Errorf("TriggerConditions count = %d, want %d", len(signal.TriggerConditions), tt.expectedConditionCount)
			}

			if signal.Direction != tt.direction {
				t.Errorf("Direction = %s, want %s", signal.Direction, tt.direction)
			}
		})
	}
}

// TestCreateSignalRSIPullbackConditions tests RSI trigger conditions are set correctly
func TestCreateSignalRSIPullbackConditions(t *testing.T) {
	// Test LONG RSI pullback
	longSignal := CreateSignalFromAssessment(
		"BTCUSDT", "LONG", "WAIT", 75,
		[]string{"rsi_long_pullback"},
		"Test", 3.0, 9.0, 50000.0, 30,
	)

	if len(longSignal.TriggerConditions) != 1 {
		t.Fatal("Expected 1 trigger condition")
	}

	cond := longSignal.TriggerConditions[0]
	if cond.Type != TriggerRSIBelow {
		t.Errorf("Type = %s, want %s", cond.Type, TriggerRSIBelow)
	}
	if cond.TargetValue != RSILongPullbackUpper {
		t.Errorf("TargetValue = %.1f, want %.1f", cond.TargetValue, RSILongPullbackUpper)
	}

	// Test SHORT RSI pullback
	shortSignal := CreateSignalFromAssessment(
		"BTCUSDT", "SHORT", "WAIT", 75,
		[]string{"rsi_short_pullback"},
		"Test", 3.0, 9.0, 50000.0, 30,
	)

	if len(shortSignal.TriggerConditions) != 1 {
		t.Fatal("Expected 1 trigger condition")
	}

	cond = shortSignal.TriggerConditions[0]
	if cond.Type != TriggerRSIAbove {
		t.Errorf("Type = %s, want %s", cond.Type, TriggerRSIAbove)
	}
	if cond.TargetValue != RSIShortPullbackLower {
		t.Errorf("TargetValue = %.1f, want %.1f", cond.TargetValue, RSIShortPullbackLower)
	}
}

// TestSignalGetSummary tests the summary generation
func TestSignalGetSummary(t *testing.T) {
	signal := &Signal{
		Symbol:       "BTCUSDT",
		Direction:    "LONG",
		EntryQuality: "GOOD",
		Confidence:   75,
		ExpiresAt:    time.Now().Add(30 * time.Minute),
		TriggerConditions: []TriggerCondition{
			{Type: TriggerPullbackEMA9, Met: true},
			{Type: TriggerRSIBelow, Met: false},
		},
	}

	summary := signal.GetSummary()

	if summary == "" {
		t.Error("GetSummary returned empty string")
	}

	// Should contain key information
	if !contains(summary, "BTCUSDT") {
		t.Error("Summary should contain symbol")
	}
	if !contains(summary, "LONG") {
		t.Error("Summary should contain direction")
	}
	if !contains(summary, "1/2") {
		t.Error("Summary should contain condition progress (1/2)")
	}
}

// TestGetWaitingSignals tests retrieval of all waiting signals
func TestGetWaitingSignals(t *testing.T) {
	store := NewSignalStore(30)

	// Add multiple signals with different statuses
	store.AddSignal(&Signal{Symbol: "BTCUSDT", Status: SignalWaiting})
	store.AddSignal(&Signal{Symbol: "ETHUSDT", Status: SignalWaiting})
	store.AddSignal(&Signal{Symbol: "SOLUSDT", Status: SignalTriggered})

	// Manually update status (since AddSignal sets default)
	store.UpdateSignalStatus("SOLUSDT", SignalTriggered)

	waiting := store.GetWaitingSignals()

	// Should only get BTCUSDT and ETHUSDT
	if len(waiting) != 2 {
		t.Errorf("GetWaitingSignals returned %d signals, want 2", len(waiting))
	}
}

// TestCleanupExpired tests expired signal cleanup
func TestCleanupExpired(t *testing.T) {
	store := NewSignalStore(1)

	// Add expired signal
	store.AddSignal(&Signal{
		Symbol:    "BTCUSDT",
		Status:    SignalWaiting,
		CreatedAt: time.Now().Add(-5 * time.Minute),
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	})

	// Add active signal
	store.AddSignal(&Signal{
		Symbol:    "ETHUSDT",
		Status:    SignalWaiting,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	})

	removed := store.CleanupExpired()

	if removed != 1 {
		t.Errorf("CleanupExpired removed %d signals, want 1", removed)
	}

	// BTCUSDT should be gone
	if store.GetSignal("BTCUSDT") != nil {
		t.Error("Expired signal should be removed")
	}

	// ETHUSDT should still exist
	if store.GetSignal("ETHUSDT") == nil {
		t.Error("Active signal should not be removed")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
