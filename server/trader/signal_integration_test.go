package trader

import (
	"testing"

	"auto-trader-ahh/ai"
	"auto-trader-ahh/market"
)

// TestNewSignalIntegration verifies default configuration
func TestNewSignalIntegration(t *testing.T) {
	si := NewSignalIntegration(30)

	if si.ExpiryMinutes != 30 {
		t.Errorf("ExpiryMinutes = %d, want 30", si.ExpiryMinutes)
	}
	if si.MinConfidence != 50 {
		t.Errorf("MinConfidence = %d, want 50", si.MinConfidence)
	}
	if si.store == nil {
		t.Error("store should not be nil")
	}
	if si.triggerChecker == nil {
		t.Error("triggerChecker should not be nil")
	}
}

// TestProcessAssessmentExcellent verifies EXCELLENT entries execute immediately
func TestProcessAssessmentExcellent(t *testing.T) {
	si := NewSignalIntegration(30)

	assessment := &ai.EntryAssessment{
		Symbol:        "BTCUSDT",
		Direction:     "LONG",
		EntryQuality:  "EXCELLENT",
		Confidence:    90,
		WaitFor:       []string{},
		Reasoning:     "Perfect entry at EMA support",
		StopLossPct:   3.0,
		TakeProfitPct: 9.0,
	}

	decision := si.ProcessAssessment(assessment, 50000.0)

	if decision.Action != ActionExecuteNow {
		t.Errorf("Action = %s, want EXECUTE_NOW", decision.Action)
	}
	if decision.TradeDecision == nil {
		t.Fatal("TradeDecision should not be nil for EXECUTE_NOW")
	}
	if decision.TradeDecision.Action != "BUY" {
		t.Errorf("TradeDecision.Action = %s, want BUY", decision.TradeDecision.Action)
	}
	if decision.TradeDecision.Confidence != 90 {
		t.Errorf("Confidence = %.0f, want 90", decision.TradeDecision.Confidence)
	}
}

// TestProcessAssessmentExcellentShort verifies SHORT direction handling
func TestProcessAssessmentExcellentShort(t *testing.T) {
	si := NewSignalIntegration(30)

	assessment := &ai.EntryAssessment{
		Symbol:        "BTCUSDT",
		Direction:     "SHORT",
		EntryQuality:  "EXCELLENT",
		Confidence:    85,
		WaitFor:       []string{},
		Reasoning:     "Perfect short entry at resistance",
		StopLossPct:   3.0,
		TakeProfitPct: 9.0,
	}

	decision := si.ProcessAssessment(assessment, 50000.0)

	if decision.Action != ActionExecuteNow {
		t.Errorf("Action = %s, want EXECUTE_NOW", decision.Action)
	}
	if decision.TradeDecision.Action != "SELL" {
		t.Errorf("TradeDecision.Action = %s, want SELL for SHORT", decision.TradeDecision.Action)
	}
}

// TestProcessAssessmentGoodWithTriggers verifies GOOD entries create signals
func TestProcessAssessmentGoodWithTriggers(t *testing.T) {
	si := NewSignalIntegration(30)

	assessment := &ai.EntryAssessment{
		Symbol:        "BTCUSDT",
		Direction:     "LONG",
		EntryQuality:  "GOOD",
		Confidence:    75,
		WaitFor:       []string{"pullback_to_ema9"},
		Reasoning:     "Good setup, wait for pullback",
		StopLossPct:   3.0,
		TakeProfitPct: 9.0,
	}

	decision := si.ProcessAssessment(assessment, 50000.0)

	if decision.Action != ActionCreateSignal {
		t.Errorf("Action = %s, want CREATE_SIGNAL", decision.Action)
	}
	if decision.PendingSignal == nil {
		t.Fatal("PendingSignal should not be nil for CREATE_SIGNAL")
	}
	if len(decision.PendingSignal.TriggerConditions) != 1 {
		t.Errorf("TriggerConditions count = %d, want 1",
			len(decision.PendingSignal.TriggerConditions))
	}

	// Verify signal was added to store
	stored := si.GetSignal("BTCUSDT")
	if stored == nil {
		t.Error("Signal should be stored")
	}
}

// TestProcessAssessmentGoodWithoutTriggers verifies GOOD entries without triggers execute
func TestProcessAssessmentGoodWithoutTriggers(t *testing.T) {
	si := NewSignalIntegration(30)

	assessment := &ai.EntryAssessment{
		Symbol:        "BTCUSDT",
		Direction:     "LONG",
		EntryQuality:  "GOOD",
		Confidence:    75,
		WaitFor:       []string{}, // No triggers specified
		Reasoning:     "Good setup, no wait needed",
		StopLossPct:   3.0,
		TakeProfitPct: 9.0,
	}

	decision := si.ProcessAssessment(assessment, 50000.0)

	if decision.Action != ActionExecuteNow {
		t.Errorf("Action = %s, want EXECUTE_NOW for GOOD without triggers", decision.Action)
	}
}

// TestProcessAssessmentWait verifies WAIT entries create signals with triggers
func TestProcessAssessmentWait(t *testing.T) {
	si := NewSignalIntegration(30)

	assessment := &ai.EntryAssessment{
		Symbol:        "BTCUSDT",
		Direction:     "LONG",
		EntryQuality:  "WAIT",
		Confidence:    60,
		WaitFor:       []string{"pullback_to_ema9", "rsi_long_pullback"},
		Reasoning:     "Trend identified but extended, wait for pullback",
		StopLossPct:   3.0,
		TakeProfitPct: 9.0,
	}

	decision := si.ProcessAssessment(assessment, 50000.0)

	if decision.Action != ActionCreateSignal {
		t.Errorf("Action = %s, want CREATE_SIGNAL", decision.Action)
	}
	if decision.PendingSignal == nil {
		t.Fatal("PendingSignal should not be nil")
	}
	if len(decision.PendingSignal.TriggerConditions) != 2 {
		t.Errorf("TriggerConditions count = %d, want 2",
			len(decision.PendingSignal.TriggerConditions))
	}
}

// TestProcessAssessmentWaitLowConfidence verifies low confidence WAIT entries are skipped
func TestProcessAssessmentWaitLowConfidence(t *testing.T) {
	si := NewSignalIntegration(30)

	assessment := &ai.EntryAssessment{
		Symbol:        "BTCUSDT",
		Direction:     "LONG",
		EntryQuality:  "WAIT",
		Confidence:    40, // Below MinConfidence of 50
		WaitFor:       []string{"pullback_to_ema9"},
		Reasoning:     "Low confidence setup",
		StopLossPct:   3.0,
		TakeProfitPct: 9.0,
	}

	decision := si.ProcessAssessment(assessment, 50000.0)

	if decision.Action != ActionSkip {
		t.Errorf("Action = %s, want SKIP for low confidence", decision.Action)
	}
}

// TestProcessAssessmentAvoid verifies AVOID entries are skipped
func TestProcessAssessmentAvoid(t *testing.T) {
	si := NewSignalIntegration(30)

	assessment := &ai.EntryAssessment{
		Symbol:        "BTCUSDT",
		Direction:     "NONE",
		EntryQuality:  "AVOID",
		Confidence:    30,
		WaitFor:       []string{},
		Reasoning:     "Conflicting signals, no clear direction",
		StopLossPct:   0,
		TakeProfitPct: 0,
	}

	decision := si.ProcessAssessment(assessment, 50000.0)

	if decision.Action != ActionSkip {
		t.Errorf("Action = %s, want SKIP for AVOID", decision.Action)
	}
}

// TestProcessAssessmentExistingSignal verifies duplicate signals are detected
func TestProcessAssessmentExistingSignal(t *testing.T) {
	si := NewSignalIntegration(30)

	// Create first signal
	assessment1 := &ai.EntryAssessment{
		Symbol:        "BTCUSDT",
		Direction:     "LONG",
		EntryQuality:  "WAIT",
		Confidence:    60,
		WaitFor:       []string{"pullback_to_ema9"},
		Reasoning:     "First signal",
		StopLossPct:   3.0,
		TakeProfitPct: 9.0,
	}
	si.ProcessAssessment(assessment1, 50000.0)

	// Try to create second signal for same symbol
	assessment2 := &ai.EntryAssessment{
		Symbol:        "BTCUSDT",
		Direction:     "LONG",
		EntryQuality:  "EXCELLENT",
		Confidence:    90,
		WaitFor:       []string{},
		Reasoning:     "Second signal attempt",
		StopLossPct:   3.0,
		TakeProfitPct: 9.0,
	}
	decision := si.ProcessAssessment(assessment2, 51000.0)

	if decision.Action != ActionExistingSignal {
		t.Errorf("Action = %s, want EXISTING for duplicate symbol", decision.Action)
	}
}

// TestCheckPendingSignals verifies trigger checking for pending signals
func TestCheckPendingSignals(t *testing.T) {
	si := NewSignalIntegration(30)

	// Create a signal with RSI condition
	assessment := &ai.EntryAssessment{
		Symbol:        "BTCUSDT",
		Direction:     "LONG",
		EntryQuality:  "WAIT",
		Confidence:    70,
		WaitFor:       []string{"rsi_long_pullback"}, // RSI below 50
		Reasoning:     "Wait for RSI pullback",
		StopLossPct:   3.0,
		TakeProfitPct: 9.0,
	}
	si.ProcessAssessment(assessment, 50000.0)

	// Mock data provider that returns RSI in pullback zone
	getData := func(symbol string) (*market.MarketData, error) {
		return &market.MarketData{
			Symbol:       symbol,
			CurrentPrice: 49900,
			EMA9:         50000,
			EMA21:        49500,
			RSI:          45, // In pullback zone (below 50)
		}, nil
	}

	readySignals := si.CheckPendingSignals(getData)

	if len(readySignals) != 1 {
		t.Errorf("ReadySignals count = %d, want 1", len(readySignals))
	}
}

// TestCheckPendingSignalsNotReady verifies signals don't trigger prematurely
func TestCheckPendingSignalsNotReady(t *testing.T) {
	si := NewSignalIntegration(30)

	assessment := &ai.EntryAssessment{
		Symbol:        "BTCUSDT",
		Direction:     "LONG",
		EntryQuality:  "WAIT",
		Confidence:    70,
		WaitFor:       []string{"rsi_long_pullback"}, // RSI below 50
		Reasoning:     "Wait for RSI pullback",
		StopLossPct:   3.0,
		TakeProfitPct: 9.0,
	}
	si.ProcessAssessment(assessment, 50000.0)

	// Mock data provider that returns RSI NOT in pullback zone
	getData := func(symbol string) (*market.MarketData, error) {
		return &market.MarketData{
			Symbol:       symbol,
			CurrentPrice: 51000,
			EMA9:         50000,
			RSI:          65, // Not in pullback zone (above 50)
		}, nil
	}

	readySignals := si.CheckPendingSignals(getData)

	if len(readySignals) != 0 {
		t.Errorf("ReadySignals count = %d, want 0 (conditions not met)", len(readySignals))
	}
}

// TestSignalToTradeDecision verifies conversion of triggered signal to trade
func TestSignalToTradeDecision(t *testing.T) {
	si := NewSignalIntegration(30)

	signal := &Signal{
		Symbol:        "BTCUSDT",
		Direction:     "LONG",
		Confidence:    75,
		Reasoning:     "Pullback entry triggered",
		StopLossPct:   3.0,
		TakeProfitPct: 9.0,
	}

	tradeDec := si.SignalToTradeDecision(signal, 50000.0)

	if tradeDec.Symbol != "BTCUSDT" {
		t.Errorf("Symbol = %s, want BTCUSDT", tradeDec.Symbol)
	}
	if tradeDec.Action != "BUY" {
		t.Errorf("Action = %s, want BUY for LONG", tradeDec.Action)
	}
	if tradeDec.Confidence != 75 {
		t.Errorf("Confidence = %.0f, want 75", tradeDec.Confidence)
	}
}

// TestSignalToTradeDecisionShort verifies SHORT signal conversion
func TestSignalToTradeDecisionShort(t *testing.T) {
	si := NewSignalIntegration(30)

	signal := &Signal{
		Symbol:    "BTCUSDT",
		Direction: "SHORT",
	}

	tradeDec := si.SignalToTradeDecision(signal, 50000.0)

	if tradeDec.Action != "SELL" {
		t.Errorf("Action = %s, want SELL for SHORT", tradeDec.Action)
	}
}

// TestMarkSignalExecuted verifies signal state update after execution
func TestMarkSignalExecuted(t *testing.T) {
	si := NewSignalIntegration(30)

	// Create and add a signal
	assessment := &ai.EntryAssessment{
		Symbol:       "BTCUSDT",
		Direction:    "LONG",
		EntryQuality: "WAIT",
		Confidence:   70,
		WaitFor:      []string{},
	}
	si.ProcessAssessment(assessment, 50000.0)

	// Mark as executed
	si.MarkSignalExecuted("BTCUSDT")

	// Signal should still exist but status changed
	signal := si.GetSignal("BTCUSDT")
	if signal == nil {
		t.Fatal("Signal should still exist after marking executed")
	}
	if signal.Status != SignalExecuted {
		t.Errorf("Status = %s, want EXECUTED", signal.Status)
	}
}

// TestCancelSignal verifies signal cancellation
func TestCancelSignal(t *testing.T) {
	si := NewSignalIntegration(30)

	assessment := &ai.EntryAssessment{
		Symbol:       "BTCUSDT",
		Direction:    "LONG",
		EntryQuality: "WAIT",
		Confidence:   70,
		WaitFor:      []string{"pullback_to_ema9"},
	}
	si.ProcessAssessment(assessment, 50000.0)

	// Cancel the signal
	si.CancelSignal("BTCUSDT")

	// Signal should be removed
	signal := si.GetSignal("BTCUSDT")
	if signal != nil {
		t.Error("Signal should be nil after cancellation")
	}
}

// TestGetPendingSignals verifies retrieval of waiting signals
func TestGetPendingSignals(t *testing.T) {
	si := NewSignalIntegration(30)

	// Create signals for multiple symbols
	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	for _, symbol := range symbols {
		assessment := &ai.EntryAssessment{
			Symbol:       symbol,
			Direction:    "LONG",
			EntryQuality: "WAIT",
			Confidence:   70,
			WaitFor:      []string{"pullback_to_ema9"},
		}
		si.ProcessAssessment(assessment, 50000.0)
	}

	pending := si.GetPendingSignals()
	if len(pending) != 3 {
		t.Errorf("Pending signals count = %d, want 3", len(pending))
	}
}

// TestTwoPhaseEntryWorkflow tests the complete two-phase workflow
func TestTwoPhaseEntryWorkflow(t *testing.T) {
	si := NewSignalIntegration(30)

	// Phase 1: AI identifies opportunity but timing not ideal
	assessment := &ai.EntryAssessment{
		Symbol:        "BTCUSDT",
		Direction:     "LONG",
		EntryQuality:  "WAIT",
		Confidence:    70,
		WaitFor:       []string{"pullback_to_ema9", "rsi_long_pullback"},
		Reasoning:     "Uptrend but price extended, wait for pullback",
		StopLossPct:   3.0,
		TakeProfitPct: 9.0,
	}

	decision := si.ProcessAssessment(assessment, 51000.0)
	if decision.Action != ActionCreateSignal {
		t.Fatalf("Step 1: Expected CREATE_SIGNAL, got %s", decision.Action)
	}

	// Phase 2a: Check triggers - conditions NOT met yet
	getDataNotReady := func(symbol string) (*market.MarketData, error) {
		return &market.MarketData{
			Symbol:       symbol,
			CurrentPrice: 51000, // Still extended
			EMA9:         50000,
			RSI:          65, // Still high
		}, nil
	}

	readySignals := si.CheckPendingSignals(getDataNotReady)
	if len(readySignals) != 0 {
		t.Fatal("Step 2a: No signals should be ready yet")
	}

	// Phase 2b: Market pulls back, conditions met
	getDataReady := func(symbol string) (*market.MarketData, error) {
		return &market.MarketData{
			Symbol:       symbol,
			CurrentPrice: 50000, // At EMA9
			EMA9:         50000,
			RSI:          45, // In pullback zone
		}, nil
	}

	readySignals = si.CheckPendingSignals(getDataReady)
	if len(readySignals) != 1 {
		t.Fatalf("Step 2b: Expected 1 ready signal, got %d", len(readySignals))
	}

	// Phase 3: Convert to trade and execute
	tradeDec := si.SignalToTradeDecision(readySignals[0], 50000.0)
	if tradeDec.Action != "BUY" {
		t.Errorf("Step 3: Expected BUY action, got %s", tradeDec.Action)
	}

	// Phase 4: Mark as executed
	si.MarkSignalExecuted("BTCUSDT")
	signal := si.GetSignal("BTCUSDT")
	if signal.Status != SignalExecuted {
		t.Errorf("Step 4: Expected EXECUTED status, got %s", signal.Status)
	}
}
