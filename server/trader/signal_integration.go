package trader

import (
	"fmt"
	"log"

	"auto-trader-ahh/ai"
	"auto-trader-ahh/market"
)

// SignalDecision represents the outcome of processing an AI entry assessment
// This is the bridge between AI output and the two-phase entry system
type SignalDecision struct {
	// Action to take
	Action SignalAction `json:"action"`

	// For EXECUTE_NOW - the trade details
	TradeDecision *ai.TradingDecision `json:"trade_decision,omitempty"`

	// For CREATE_SIGNAL - the pending signal
	PendingSignal *Signal `json:"pending_signal,omitempty"`

	// Explanation
	Reason string `json:"reason"`
}

// SignalAction represents what action to take based on AI assessment
type SignalAction string

const (
	ActionExecuteNow   SignalAction = "EXECUTE_NOW"    // EXCELLENT entry - execute immediately
	ActionCreateSignal SignalAction = "CREATE_SIGNAL"  // GOOD/WAIT - create pending signal with triggers
	ActionSkip         SignalAction = "SKIP"           // AVOID - do not trade
	ActionExistingSignal SignalAction = "EXISTING"     // Already have a pending signal for this symbol
)

// SignalIntegration handles the two-phase entry system
// It processes AI assessments and manages the signal lifecycle
type SignalIntegration struct {
	store          *SignalStore
	triggerChecker *TriggerChecker

	// Configuration
	ExpiryMinutes  int     // How long signals stay valid (default 30)
	MinConfidence  int     // Minimum confidence for signal creation (default 50)
}

// NewSignalIntegration creates a new signal integration handler
func NewSignalIntegration(expiryMinutes int) *SignalIntegration {
	if expiryMinutes <= 0 {
		expiryMinutes = 30
	}
	return &SignalIntegration{
		store:          NewSignalStore(expiryMinutes),
		triggerChecker: NewTriggerChecker(),
		ExpiryMinutes:  expiryMinutes,
		MinConfidence:  50,
	}
}

// ProcessAssessment converts an AI EntryAssessment into a SignalDecision
// This is the main entry point for the two-phase system
//
// Workflow:
// 1. EXCELLENT entry quality → EXECUTE_NOW (immediate trade)
// 2. GOOD/WAIT entry quality → CREATE_SIGNAL (pending signal with triggers)
// 3. AVOID entry quality → SKIP (no action)
func (si *SignalIntegration) ProcessAssessment(
	assessment *ai.EntryAssessment,
	currentPrice float64,
) *SignalDecision {
	decision := &SignalDecision{
		Action: ActionSkip,
		Reason: "Default skip",
	}

	// Check for existing signal for this symbol
	if si.store.HasActiveSignal(assessment.Symbol) {
		decision.Action = ActionExistingSignal
		existingSignal := si.store.GetSignal(assessment.Symbol)
		if existingSignal != nil {
			decision.PendingSignal = existingSignal
			decision.Reason = fmt.Sprintf("Active signal already exists: %s", existingSignal.GetSummary())
		}
		return decision
	}

	// Handle based on entry quality
	switch assessment.EntryQuality {
	case "EXCELLENT":
		// Immediate execution - price is at ideal entry point
		decision.Action = ActionExecuteNow
		decision.TradeDecision = si.assessmentToTradeDecision(assessment)
		decision.Reason = "EXCELLENT entry quality - executing immediately"
		log.Printf("[SignalIntegration] %s: EXCELLENT entry detected, executing immediately",
			assessment.Symbol)

	case "GOOD":
		// Good entry but could be better - create signal with light trigger conditions
		signal := si.createSignalFromAssessment(assessment, currentPrice)

		// For GOOD entries, if no wait_for specified, execute immediately
		if len(assessment.WaitFor) == 0 {
			decision.Action = ActionExecuteNow
			decision.TradeDecision = si.assessmentToTradeDecision(assessment)
			decision.Reason = "GOOD entry with no trigger conditions - executing"
		} else {
			decision.Action = ActionCreateSignal
			decision.PendingSignal = signal
			si.store.AddSignal(signal)
			decision.Reason = fmt.Sprintf("GOOD entry - waiting for %d trigger conditions",
				len(signal.TriggerConditions))
			log.Printf("[SignalIntegration] %s: GOOD entry, created pending signal: %s",
				assessment.Symbol, signal.GetSummary())
		}

	case "WAIT":
		// Signal identified but entry timing not ideal - must wait for triggers
		if assessment.Confidence < si.MinConfidence {
			decision.Action = ActionSkip
			decision.Reason = fmt.Sprintf("WAIT entry with low confidence (%d < %d)",
				assessment.Confidence, si.MinConfidence)
			return decision
		}

		signal := si.createSignalFromAssessment(assessment, currentPrice)
		decision.Action = ActionCreateSignal
		decision.PendingSignal = signal
		si.store.AddSignal(signal)
		decision.Reason = fmt.Sprintf("WAIT entry - created pending signal with %d trigger conditions",
			len(signal.TriggerConditions))
		log.Printf("[SignalIntegration] %s: WAIT entry, created pending signal: %s",
			assessment.Symbol, signal.GetSummary())

	case "AVOID":
		decision.Action = ActionSkip
		decision.Reason = fmt.Sprintf("AVOID entry - %s", assessment.Reasoning)
		log.Printf("[SignalIntegration] %s: AVOID - %s", assessment.Symbol, assessment.Reasoning)

	default:
		decision.Action = ActionSkip
		decision.Reason = fmt.Sprintf("Unknown entry quality: %s", assessment.EntryQuality)
	}

	return decision
}

// CheckPendingSignals checks all pending signals for trigger conditions
// Returns signals that are ready to execute
func (si *SignalIntegration) CheckPendingSignals(
	getData func(symbol string) (*market.MarketData, error),
) []*Signal {
	readySignals := make([]*Signal, 0)

	waitingSignals := si.store.GetWaitingSignals()
	for _, signal := range waitingSignals {
		data, err := getData(signal.Symbol)
		if err != nil {
			log.Printf("[SignalIntegration] Error getting data for %s: %v", signal.Symbol, err)
			continue
		}

		result := si.triggerChecker.CheckTriggers(signal, data)

		if result.ReadyToExecute {
			si.store.UpdateSignalStatus(signal.Symbol, SignalTriggered)
			readySignals = append(readySignals, signal)
			log.Printf("[SignalIntegration] %s: Triggers met! Ready for execution: %s",
				signal.Symbol, signal.GetSummary())
		}
	}

	return readySignals
}

// SignalToTradeDecision converts a triggered signal to a TradingDecision for execution
func (si *SignalIntegration) SignalToTradeDecision(signal *Signal, currentPrice float64) *ai.TradingDecision {
	action := "BUY"
	if signal.Direction == "SHORT" {
		action = "SELL"
	}

	return &ai.TradingDecision{
		Symbol:        signal.Symbol,
		Action:        action,
		Confidence:    float64(signal.Confidence),
		Reasoning:     fmt.Sprintf("[Two-Phase Entry] %s", signal.Reasoning),
		StopLossPct:   signal.StopLossPct,
		TakeProfitPct: signal.TakeProfitPct,
	}
}

// MarkSignalExecuted marks a signal as executed after trade placement
func (si *SignalIntegration) MarkSignalExecuted(symbol string) {
	si.store.UpdateSignalStatus(symbol, SignalExecuted)
}

// CancelSignal cancels a pending signal
func (si *SignalIntegration) CancelSignal(symbol string) {
	si.store.UpdateSignalStatus(symbol, SignalCanceled)
	si.store.RemoveSignal(symbol)
}

// GetPendingSignals returns all waiting signals
func (si *SignalIntegration) GetPendingSignals() []*Signal {
	return si.store.GetWaitingSignals()
}

// GetSignal returns the signal for a symbol
func (si *SignalIntegration) GetSignal(symbol string) *Signal {
	return si.store.GetSignal(symbol)
}

// CleanupExpired removes expired signals and returns count
func (si *SignalIntegration) CleanupExpired() int {
	return si.store.CleanupExpired()
}

// assessmentToTradeDecision converts EntryAssessment to TradingDecision for immediate execution
func (si *SignalIntegration) assessmentToTradeDecision(assessment *ai.EntryAssessment) *ai.TradingDecision {
	action := "BUY"
	if assessment.Direction == "SHORT" {
		action = "SELL"
	}

	return &ai.TradingDecision{
		Symbol:        assessment.Symbol,
		Action:        action,
		Confidence:    float64(assessment.Confidence),
		Reasoning:     assessment.Reasoning,
		StopLossPct:   assessment.StopLossPct,
		TakeProfitPct: assessment.TakeProfitPct,
	}
}

// createSignalFromAssessment creates a Signal from an EntryAssessment
func (si *SignalIntegration) createSignalFromAssessment(
	assessment *ai.EntryAssessment,
	currentPrice float64,
) *Signal {
	return CreateSignalFromAssessment(
		assessment.Symbol,
		assessment.Direction,
		assessment.EntryQuality,
		assessment.Confidence,
		assessment.WaitFor,
		assessment.Reasoning,
		assessment.StopLossPct,
		assessment.TakeProfitPct,
		currentPrice,
		si.ExpiryMinutes,
	)
}

// GetTriggerChecker returns the trigger checker for external use
func (si *SignalIntegration) GetTriggerChecker() *TriggerChecker {
	return si.triggerChecker
}

// GetSignalStore returns the signal store for external use
func (si *SignalIntegration) GetSignalStore() *SignalStore {
	return si.store
}
