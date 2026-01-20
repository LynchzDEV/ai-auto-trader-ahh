package trader

import (
	"fmt"
	"sync"
	"time"
)

// SignalStatus represents the current state of a trading signal
type SignalStatus string

const (
	SignalWaiting  SignalStatus = "WAITING_TRIGGER" // Signal identified, waiting for trigger conditions
	SignalTriggered SignalStatus = "TRIGGERED"       // All trigger conditions met, ready to execute
	SignalExpired  SignalStatus = "EXPIRED"          // Signal timed out without trigger
	SignalExecuted SignalStatus = "EXECUTED"         // Trade was executed
	SignalCanceled SignalStatus = "CANCELED"         // Signal was manually canceled or invalidated
)

// TriggerCondition represents a single condition that must be met before entry
// Reference: https://tradersmastermind.com/fix-your-trade-timing-signal-trigger/
type TriggerCondition struct {
	Type        string  `json:"type"`         // Type of condition (see TriggerType constants)
	TargetValue float64 `json:"target_value"` // Target value for the condition
	Description string  `json:"description"`  // Human-readable description
	Met         bool    `json:"met"`          // Whether this condition has been met
	MetAt       time.Time `json:"met_at,omitempty"` // When the condition was met
}

// TriggerType constants for different trigger conditions
const (
	TriggerPullbackEMA9  = "pullback_to_ema9"  // Price pulls back to EMA9
	TriggerPullbackEMA21 = "pullback_to_ema21" // Price pulls back to EMA21
	TriggerRSIBelow      = "rsi_below"         // RSI drops below target
	TriggerRSIAbove      = "rsi_above"         // RSI rises above target
	TriggerBullishCandle = "bullish_candle"    // Bullish confirmation candle
	TriggerBearishCandle = "bearish_candle"    // Bearish confirmation candle
	TriggerVolumeSpike   = "volume_increase"   // Volume increases significantly
	TriggerMACDCross     = "macd_cross"        // MACD crosses signal line
)

// RSI Thresholds based on research
// Reference: https://indicatorvault.com/5-basic-pullback-trading-strategy-with-the-rsi/
// "In an uptrend, look for RSI to pull back toward the 40–50 zone, then bounce upward"
// "In a downtrend, watch RSI rise toward 50–60 before turning lower again"
const (
	// For LONG entries: wait for RSI to pull back to this zone
	RSILongPullbackUpper = 50.0 // RSI should drop below this
	RSILongPullbackLower = 40.0 // Ideal entry zone lower bound
	RSILongPullbackIdeal = 45.0 // Sweet spot for LONG pullback entry

	// For SHORT entries: wait for RSI to bounce to this zone
	RSIShortPullbackLower = 50.0 // RSI should rise above this
	RSIShortPullbackUpper = 60.0 // Ideal entry zone upper bound
	RSIShortPullbackIdeal = 55.0 // Sweet spot for SHORT pullback entry

	// Extreme levels (avoid entry)
	RSIOverbought = 70.0 // Avoid LONG entries above this
	RSIOversold   = 30.0 // Avoid SHORT entries below this
)

// Signal represents a trading signal waiting for trigger conditions
// Two-phase entry: Signal (AI identifies opportunity) → Trigger (code determines timing)
type Signal struct {
	ID              string             `json:"id"`
	Symbol          string             `json:"symbol"`
	Direction       string             `json:"direction"`      // "LONG" or "SHORT"
	EntryQuality    string             `json:"entry_quality"`  // From AI: "EXCELLENT", "GOOD", "WAIT"
	Confidence      int                `json:"confidence"`
	Reasoning       string             `json:"reasoning"`

	// Trigger conditions from AI
	TriggerConditions []TriggerCondition `json:"trigger_conditions"`

	// Risk management from AI
	StopLossPct   float64 `json:"stop_loss_pct"`
	TakeProfitPct float64 `json:"take_profit_pct"`

	// Timing
	CreatedAt   time.Time    `json:"created_at"`
	ExpiresAt   time.Time    `json:"expires_at"`
	TriggeredAt time.Time    `json:"triggered_at,omitempty"`
	ExecutedAt  time.Time    `json:"executed_at,omitempty"`

	// State
	Status      SignalStatus `json:"status"`

	// Price at signal creation (for tracking)
	SignalPrice float64 `json:"signal_price"`
}

// SignalStore manages pending signals with thread-safe operations
type SignalStore struct {
	signals map[string]*Signal // Key: symbol (one signal per symbol)
	mu      sync.RWMutex

	// Configuration
	defaultExpiryMins int // Default signal expiry in minutes
}

// NewSignalStore creates a new signal store
func NewSignalStore(defaultExpiryMins int) *SignalStore {
	if defaultExpiryMins <= 0 {
		defaultExpiryMins = 30 // Default 30 minutes
	}
	return &SignalStore{
		signals:           make(map[string]*Signal),
		defaultExpiryMins: defaultExpiryMins,
	}
}

// AddSignal adds a new signal or replaces existing one for the symbol
func (s *SignalStore) AddSignal(signal *Signal) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate ID if not set
	if signal.ID == "" {
		signal.ID = fmt.Sprintf("%s-%d", signal.Symbol, time.Now().UnixNano())
	}

	// Set timestamps if not set
	if signal.CreatedAt.IsZero() {
		signal.CreatedAt = time.Now()
	}
	if signal.ExpiresAt.IsZero() {
		signal.ExpiresAt = signal.CreatedAt.Add(time.Duration(s.defaultExpiryMins) * time.Minute)
	}

	// Set initial status
	if signal.Status == "" {
		signal.Status = SignalWaiting
	}

	s.signals[signal.Symbol] = signal
}

// GetSignal returns the signal for a symbol (if exists and not expired)
func (s *SignalStore) GetSignal(symbol string) *Signal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	signal, exists := s.signals[symbol]
	if !exists {
		return nil
	}

	// Check expiry
	if time.Now().After(signal.ExpiresAt) && signal.Status == SignalWaiting {
		return nil // Expired
	}

	return signal
}

// GetWaitingSignals returns all signals in WAITING_TRIGGER status
func (s *SignalStore) GetWaitingSignals() []*Signal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var waiting []*Signal
	now := time.Now()

	for _, signal := range s.signals {
		if signal.Status == SignalWaiting && now.Before(signal.ExpiresAt) {
			waiting = append(waiting, signal)
		}
	}

	return waiting
}

// UpdateSignalStatus updates the status of a signal
func (s *SignalStore) UpdateSignalStatus(symbol string, status SignalStatus) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	signal, exists := s.signals[symbol]
	if !exists {
		return false
	}

	signal.Status = status

	switch status {
	case SignalTriggered:
		signal.TriggeredAt = time.Now()
	case SignalExecuted:
		signal.ExecutedAt = time.Now()
	}

	return true
}

// MarkConditionMet marks a specific trigger condition as met
func (s *SignalStore) MarkConditionMet(symbol string, conditionType string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	signal, exists := s.signals[symbol]
	if !exists {
		return false
	}

	for i := range signal.TriggerConditions {
		if signal.TriggerConditions[i].Type == conditionType && !signal.TriggerConditions[i].Met {
			signal.TriggerConditions[i].Met = true
			signal.TriggerConditions[i].MetAt = time.Now()
			return true
		}
	}

	return false
}

// AllConditionsMet checks if all trigger conditions for a signal are met
func (s *SignalStore) AllConditionsMet(symbol string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	signal, exists := s.signals[symbol]
	if !exists {
		return false
	}

	// If no conditions, always ready (EXCELLENT entry)
	if len(signal.TriggerConditions) == 0 {
		return true
	}

	for _, cond := range signal.TriggerConditions {
		if !cond.Met {
			return false
		}
	}

	return true
}

// RemoveSignal removes a signal for a symbol
func (s *SignalStore) RemoveSignal(symbol string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.signals, symbol)
}

// CleanupExpired removes all expired signals and returns count removed
func (s *SignalStore) CleanupExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	removed := 0

	for symbol, signal := range s.signals {
		if signal.Status == SignalWaiting && now.After(signal.ExpiresAt) {
			signal.Status = SignalExpired
			delete(s.signals, symbol)
			removed++
		}
	}

	return removed
}

// HasActiveSignal checks if there's an active (non-expired, non-executed) signal for symbol
func (s *SignalStore) HasActiveSignal(symbol string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	signal, exists := s.signals[symbol]
	if !exists {
		return false
	}

	// Check if signal is still active
	switch signal.Status {
	case SignalWaiting, SignalTriggered:
		return time.Now().Before(signal.ExpiresAt)
	default:
		return false
	}
}

// GetSignalSummary returns a human-readable summary of a signal
func (sig *Signal) GetSummary() string {
	conditionStatus := ""
	if len(sig.TriggerConditions) > 0 {
		met := 0
		for _, c := range sig.TriggerConditions {
			if c.Met {
				met++
			}
		}
		conditionStatus = fmt.Sprintf(" [%d/%d conditions met]", met, len(sig.TriggerConditions))
	}

	return fmt.Sprintf("%s %s signal (quality=%s, conf=%d%%, expires=%s)%s",
		sig.Symbol,
		sig.Direction,
		sig.EntryQuality,
		sig.Confidence,
		sig.ExpiresAt.Format("15:04:05"),
		conditionStatus,
	)
}

// CreateSignalFromAssessment creates a Signal from an AI EntryAssessment
// This bridges the AI output to the signal state machine
func CreateSignalFromAssessment(symbol string, direction string, entryQuality string, confidence int,
	waitFor []string, reasoning string, stopLossPct float64, takeProfitPct float64,
	currentPrice float64, expiryMins int) *Signal {

	signal := &Signal{
		Symbol:        symbol,
		Direction:     direction,
		EntryQuality:  entryQuality,
		Confidence:    confidence,
		Reasoning:     reasoning,
		StopLossPct:   stopLossPct,
		TakeProfitPct: takeProfitPct,
		SignalPrice:   currentPrice,
		CreatedAt:     time.Now(),
		Status:        SignalWaiting,
	}

	// Set expiry
	if expiryMins <= 0 {
		expiryMins = 30
	}
	signal.ExpiresAt = signal.CreatedAt.Add(time.Duration(expiryMins) * time.Minute)

	// Convert wait_for strings to TriggerConditions
	for _, waitType := range waitFor {
		condition := TriggerCondition{
			Type: waitType,
			Met:  false,
		}

		// Set description and target values based on type
		// RSI thresholds based on research: https://indicatorvault.com/5-basic-pullback-trading-strategy-with-the-rsi/
		switch waitType {
		case TriggerPullbackEMA9:
			condition.Description = "Wait for price pullback to EMA9"
		case TriggerPullbackEMA21:
			condition.Description = "Wait for price pullback to EMA21"
		case TriggerBullishCandle:
			condition.Description = "Wait for bullish confirmation candle (hammer, engulfing, pin bar)"
		case TriggerBearishCandle:
			condition.Description = "Wait for bearish confirmation candle"
		case TriggerVolumeSpike:
			condition.Description = "Wait for volume increase confirmation"
		case TriggerMACDCross:
			condition.Description = "Wait for MACD to cross signal line"
		case "rsi_long_pullback":
			// Research: "In an uptrend, look for RSI to pull back toward the 40–50 zone"
			condition.Type = TriggerRSIBelow
			condition.TargetValue = RSILongPullbackUpper // 50
			condition.Description = fmt.Sprintf("Wait for RSI to pull back below %.0f (ideal zone: %.0f-%.0f)",
				RSILongPullbackUpper, RSILongPullbackLower, RSILongPullbackUpper)
		case "rsi_short_pullback":
			// Research: "In a downtrend, watch RSI rise toward 50–60 before turning lower"
			condition.Type = TriggerRSIAbove
			condition.TargetValue = RSIShortPullbackLower // 50
			condition.Description = fmt.Sprintf("Wait for RSI to bounce above %.0f (ideal zone: %.0f-%.0f)",
				RSIShortPullbackLower, RSIShortPullbackLower, RSIShortPullbackUpper)
		default:
			// Handle legacy RSI conditions with target value (rsi_below_X, rsi_above_X)
			if len(waitType) > 10 && waitType[:10] == "rsi_below_" {
				condition.Description = fmt.Sprintf("Wait for RSI below %s", waitType[10:])
			} else if len(waitType) > 10 && waitType[:10] == "rsi_above_" {
				condition.Description = fmt.Sprintf("Wait for RSI above %s", waitType[10:])
			} else {
				condition.Description = waitType
			}
		}

		signal.TriggerConditions = append(signal.TriggerConditions, condition)
	}

	// If no trigger conditions (EXCELLENT entry), signal is immediately ready
	if len(signal.TriggerConditions) == 0 && entryQuality == "EXCELLENT" {
		signal.Status = SignalTriggered
		signal.TriggeredAt = time.Now()
	}

	return signal
}
