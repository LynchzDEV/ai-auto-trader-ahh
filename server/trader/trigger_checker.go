package trader

import (
	"fmt"
	"log"
	"math"
	"time"

	"auto-trader-ahh/exchange"
	"auto-trader-ahh/market"
)

// TriggerChecker evaluates trigger conditions for pending signals
// This implements the "Trigger" phase of the Signal → Trigger two-phase entry system
// Reference: https://tradersmastermind.com/fix-your-trade-timing-signal-trigger/
type TriggerChecker struct {
	// Configuration
	EMA9Tolerance  float64 // Percentage tolerance for EMA9 pullback (default 0.3%)
	EMA21Tolerance float64 // Percentage tolerance for EMA21 pullback (default 0.5%)
	VolumeSpikeMin float64 // Minimum volume increase ratio to confirm (default 1.5x)
}

// TriggerResult contains the outcome of checking trigger conditions
type TriggerResult struct {
	AllConditionsMet  bool                `json:"all_conditions_met"`
	ConditionsChecked int                 `json:"conditions_checked"`
	ConditionsMet     int                 `json:"conditions_met"`
	Details           []TriggerCheckDetail `json:"details"`
	ReadyToExecute    bool                `json:"ready_to_execute"`
	Reason            string              `json:"reason"`
}

// TriggerCheckDetail provides detail about each condition check
type TriggerCheckDetail struct {
	ConditionType string  `json:"condition_type"`
	Description   string  `json:"description"`
	Met           bool    `json:"met"`
	CurrentValue  float64 `json:"current_value,omitempty"`
	TargetValue   float64 `json:"target_value,omitempty"`
	Message       string  `json:"message"`
}

// NewTriggerChecker creates a new trigger checker with default configuration
func NewTriggerChecker() *TriggerChecker {
	return &TriggerChecker{
		EMA9Tolerance:  0.3,  // 0.3% tolerance for EMA9 pullback
		EMA21Tolerance: 0.5,  // 0.5% tolerance for EMA21 pullback
		VolumeSpikeMin: 1.5,  // 1.5x average volume for confirmation
	}
}

// CheckTriggers evaluates all trigger conditions for a signal against current market data
// Returns a TriggerResult indicating which conditions are met and if the signal is ready to execute
func (tc *TriggerChecker) CheckTriggers(signal *Signal, data *market.MarketData) *TriggerResult {
	result := &TriggerResult{
		AllConditionsMet:  false,
		ConditionsChecked: len(signal.TriggerConditions),
		ConditionsMet:     0,
		Details:           make([]TriggerCheckDetail, 0),
		ReadyToExecute:    false,
	}

	// If no conditions, signal is immediately ready
	if len(signal.TriggerConditions) == 0 {
		result.AllConditionsMet = true
		result.ReadyToExecute = true
		result.Reason = "No trigger conditions required (EXCELLENT entry)"
		return result
	}

	// Check each condition
	for i := range signal.TriggerConditions {
		cond := &signal.TriggerConditions[i]
		detail := tc.checkSingleCondition(cond, signal, data)
		result.Details = append(result.Details, detail)

		if detail.Met {
			result.ConditionsMet++
			// Update the condition in the signal if not already met
			if !cond.Met {
				cond.Met = true
				cond.MetAt = time.Now()
				log.Printf("[TriggerChecker] %s: Condition '%s' met - %s",
					signal.Symbol, cond.Type, detail.Message)
			}
		}
	}

	// Check if all conditions are met
	result.AllConditionsMet = result.ConditionsMet == result.ConditionsChecked
	result.ReadyToExecute = result.AllConditionsMet

	if result.ReadyToExecute {
		result.Reason = fmt.Sprintf("All %d trigger conditions met", result.ConditionsChecked)
	} else {
		result.Reason = fmt.Sprintf("%d/%d conditions met, waiting for remaining triggers",
			result.ConditionsMet, result.ConditionsChecked)
	}

	return result
}

// checkSingleCondition evaluates a single trigger condition
func (tc *TriggerChecker) checkSingleCondition(cond *TriggerCondition, signal *Signal, data *market.MarketData) TriggerCheckDetail {
	detail := TriggerCheckDetail{
		ConditionType: cond.Type,
		Description:   cond.Description,
		Met:           false,
	}

	switch cond.Type {
	case TriggerPullbackEMA9:
		detail = tc.checkPullbackToEMA9(signal, data)
	case TriggerPullbackEMA21:
		detail = tc.checkPullbackToEMA21(signal, data)
	case TriggerRSIBelow:
		detail = tc.checkRSIBelow(cond.TargetValue, data)
	case TriggerRSIAbove:
		detail = tc.checkRSIAbove(cond.TargetValue, data)
	case TriggerBullishCandle:
		detail = tc.checkBullishCandle(data)
	case TriggerBearishCandle:
		detail = tc.checkBearishCandle(data)
	case TriggerVolumeSpike:
		detail = tc.checkVolumeSpike(data)
	case TriggerMACDCross:
		detail = tc.checkMACDCross(signal.Direction, data)
	default:
		detail.Message = fmt.Sprintf("Unknown trigger type: %s", cond.Type)
	}

	// Preserve the original description if not set by the check
	if detail.Description == "" {
		detail.Description = cond.Description
	}
	detail.ConditionType = cond.Type

	return detail
}

// checkPullbackToEMA9 checks if price has pulled back to EMA9
// For LONG: price should be at or below EMA9 (buying the dip)
// For SHORT: price should be at or above EMA9 (selling the rally)
// Reference: https://capital.com/en-int/learn/trading-strategies/pullback-trading
func (tc *TriggerChecker) checkPullbackToEMA9(signal *Signal, data *market.MarketData) TriggerCheckDetail {
	detail := TriggerCheckDetail{
		ConditionType: TriggerPullbackEMA9,
		Description:   "Price pullback to EMA9",
		CurrentValue:  data.CurrentPrice,
		TargetValue:   data.EMA9,
	}

	if data.EMA9 <= 0 {
		detail.Message = "EMA9 not available"
		return detail
	}

	// Calculate distance from EMA9 as percentage
	distancePct := ((data.CurrentPrice - data.EMA9) / data.EMA9) * 100

	if signal.Direction == "LONG" {
		// For LONG: price should be at or below EMA9 (within tolerance)
		// This is the "buy the dip" condition
		if distancePct <= tc.EMA9Tolerance {
			detail.Met = true
			detail.Message = fmt.Sprintf("Price at EMA9 support (%.2f%% from EMA9)", distancePct)
		} else {
			detail.Message = fmt.Sprintf("Price still %.2f%% above EMA9, waiting for pullback", distancePct)
		}
	} else {
		// For SHORT: price should be at or above EMA9 (within tolerance)
		// This is the "sell the rally" condition
		if distancePct >= -tc.EMA9Tolerance {
			detail.Met = true
			detail.Message = fmt.Sprintf("Price at EMA9 resistance (%.2f%% from EMA9)", distancePct)
		} else {
			detail.Message = fmt.Sprintf("Price still %.2f%% below EMA9, waiting for bounce", distancePct)
		}
	}

	return detail
}

// checkPullbackToEMA21 checks if price has pulled back to EMA21 (deeper pullback)
func (tc *TriggerChecker) checkPullbackToEMA21(signal *Signal, data *market.MarketData) TriggerCheckDetail {
	detail := TriggerCheckDetail{
		ConditionType: TriggerPullbackEMA21,
		Description:   "Price pullback to EMA21",
		CurrentValue:  data.CurrentPrice,
		TargetValue:   data.EMA21,
	}

	if data.EMA21 <= 0 {
		detail.Message = "EMA21 not available"
		return detail
	}

	distancePct := ((data.CurrentPrice - data.EMA21) / data.EMA21) * 100

	if signal.Direction == "LONG" {
		if distancePct <= tc.EMA21Tolerance {
			detail.Met = true
			detail.Message = fmt.Sprintf("Price at EMA21 support (%.2f%% from EMA21)", distancePct)
		} else {
			detail.Message = fmt.Sprintf("Price still %.2f%% above EMA21, waiting for deeper pullback", distancePct)
		}
	} else {
		if distancePct >= -tc.EMA21Tolerance {
			detail.Met = true
			detail.Message = fmt.Sprintf("Price at EMA21 resistance (%.2f%% from EMA21)", distancePct)
		} else {
			detail.Message = fmt.Sprintf("Price still %.2f%% below EMA21, waiting for bounce", distancePct)
		}
	}

	return detail
}

// checkRSIBelow checks if RSI is below target value (for LONG pullback entries)
// Reference: "In an uptrend, look for RSI to pull back toward the 40–50 zone"
func (tc *TriggerChecker) checkRSIBelow(targetValue float64, data *market.MarketData) TriggerCheckDetail {
	detail := TriggerCheckDetail{
		ConditionType: TriggerRSIBelow,
		Description:   fmt.Sprintf("RSI below %.0f", targetValue),
		CurrentValue:  data.RSI,
		TargetValue:   targetValue,
	}

	if targetValue <= 0 {
		targetValue = RSILongPullbackUpper // Default to 50
	}

	if data.RSI <= targetValue {
		detail.Met = true
		detail.Message = fmt.Sprintf("RSI at %.1f (below %.0f target) - pullback zone reached", data.RSI, targetValue)
	} else {
		detail.Message = fmt.Sprintf("RSI at %.1f, waiting for drop below %.0f", data.RSI, targetValue)
	}

	return detail
}

// checkRSIAbove checks if RSI is above target value (for SHORT pullback entries)
// Reference: "In a downtrend, watch RSI rise toward 50–60 before turning lower"
func (tc *TriggerChecker) checkRSIAbove(targetValue float64, data *market.MarketData) TriggerCheckDetail {
	detail := TriggerCheckDetail{
		ConditionType: TriggerRSIAbove,
		Description:   fmt.Sprintf("RSI above %.0f", targetValue),
		CurrentValue:  data.RSI,
		TargetValue:   targetValue,
	}

	if targetValue <= 0 {
		targetValue = RSIShortPullbackLower // Default to 50
	}

	if data.RSI >= targetValue {
		detail.Met = true
		detail.Message = fmt.Sprintf("RSI at %.1f (above %.0f target) - bounce zone reached", data.RSI, targetValue)
	} else {
		detail.Message = fmt.Sprintf("RSI at %.1f, waiting for rise above %.0f", data.RSI, targetValue)
	}

	return detail
}

// checkBullishCandle checks for bullish confirmation candlestick patterns
// Patterns: Hammer, Bullish Engulfing, Pin Bar (showing strong rejection of lower prices)
// Reference: https://capital.com/en-int/learn/trading-strategies/pullback-trading
func (tc *TriggerChecker) checkBullishCandle(data *market.MarketData) TriggerCheckDetail {
	detail := TriggerCheckDetail{
		ConditionType: TriggerBullishCandle,
		Description:   "Bullish confirmation candle",
		Met:           false,
	}

	if len(data.Klines) < 2 {
		detail.Message = "Not enough candle data"
		return detail
	}

	// Get the last completed candle (not the current forming one)
	lastCandle := data.Klines[len(data.Klines)-2]
	prevCandle := data.Klines[len(data.Klines)-3]

	// Check for bullish patterns
	patterns := tc.detectBullishPatterns(lastCandle, prevCandle)

	if len(patterns) > 0 {
		detail.Met = true
		detail.Message = fmt.Sprintf("Bullish pattern detected: %v", patterns)
	} else {
		detail.Message = "No bullish confirmation pattern yet"
	}

	return detail
}

// checkBearishCandle checks for bearish confirmation candlestick patterns
// Patterns: Shooting Star, Bearish Engulfing (showing strong rejection of higher prices)
func (tc *TriggerChecker) checkBearishCandle(data *market.MarketData) TriggerCheckDetail {
	detail := TriggerCheckDetail{
		ConditionType: TriggerBearishCandle,
		Description:   "Bearish confirmation candle",
		Met:           false,
	}

	if len(data.Klines) < 2 {
		detail.Message = "Not enough candle data"
		return detail
	}

	lastCandle := data.Klines[len(data.Klines)-2]
	prevCandle := data.Klines[len(data.Klines)-3]

	patterns := tc.detectBearishPatterns(lastCandle, prevCandle)

	if len(patterns) > 0 {
		detail.Met = true
		detail.Message = fmt.Sprintf("Bearish pattern detected: %v", patterns)
	} else {
		detail.Message = "No bearish confirmation pattern yet"
	}

	return detail
}

// checkVolumeSpike checks for volume increase confirming trend resumption
// Reference: "Volume spikes signal trend resumption conviction"
func (tc *TriggerChecker) checkVolumeSpike(data *market.MarketData) TriggerCheckDetail {
	detail := TriggerCheckDetail{
		ConditionType: TriggerVolumeSpike,
		Description:   "Volume increase confirmation",
		Met:           false,
	}

	if len(data.Klines) < 10 {
		detail.Message = "Not enough candle data for volume analysis"
		return detail
	}

	// Calculate average volume over last 10 candles (excluding current)
	var avgVolume float64
	for i := len(data.Klines) - 11; i < len(data.Klines)-1; i++ {
		avgVolume += data.Klines[i].Volume
	}
	avgVolume /= 10

	// Get current/last candle volume
	lastVolume := data.Klines[len(data.Klines)-1].Volume

	detail.CurrentValue = lastVolume
	detail.TargetValue = avgVolume * tc.VolumeSpikeMin

	volumeRatio := lastVolume / avgVolume

	if volumeRatio >= tc.VolumeSpikeMin {
		detail.Met = true
		detail.Message = fmt.Sprintf("Volume spike detected: %.1fx average", volumeRatio)
	} else {
		detail.Message = fmt.Sprintf("Volume at %.1fx average, waiting for %.1fx spike", volumeRatio, tc.VolumeSpikeMin)
	}

	return detail
}

// checkMACDCross checks if MACD has crossed the signal line in the trade direction
// For LONG: MACD crosses above signal line
// For SHORT: MACD crosses below signal line
func (tc *TriggerChecker) checkMACDCross(direction string, data *market.MarketData) TriggerCheckDetail {
	detail := TriggerCheckDetail{
		ConditionType: TriggerMACDCross,
		Description:   "MACD cross confirmation",
		CurrentValue:  data.MACDHist,
		Met:           false,
	}

	// MACD Histogram > 0 means MACD > Signal (bullish)
	// MACD Histogram < 0 means MACD < Signal (bearish)

	if direction == "LONG" {
		if data.MACDHist > 0 {
			detail.Met = true
			detail.Message = fmt.Sprintf("MACD above signal (histogram: %.4f) - bullish momentum", data.MACDHist)
		} else {
			detail.Message = fmt.Sprintf("MACD below signal (histogram: %.4f), waiting for bullish cross", data.MACDHist)
		}
	} else {
		if data.MACDHist < 0 {
			detail.Met = true
			detail.Message = fmt.Sprintf("MACD below signal (histogram: %.4f) - bearish momentum", data.MACDHist)
		} else {
			detail.Message = fmt.Sprintf("MACD above signal (histogram: %.4f), waiting for bearish cross", data.MACDHist)
		}
	}

	return detail
}

// detectBullishPatterns identifies bullish candlestick patterns
func (tc *TriggerChecker) detectBullishPatterns(current, previous exchange.Kline) []string {
	var patterns []string

	body := current.Close - current.Open
	upperWick := current.High - math.Max(current.Open, current.Close)
	lowerWick := math.Min(current.Open, current.Close) - current.Low
	candleRange := current.High - current.Low

	if candleRange == 0 {
		return patterns
	}

	// Hammer: Small body at top, long lower wick (>2x body), small upper wick
	// Shows strong rejection of lower prices
	absBody := math.Abs(body)
	if lowerWick >= 2*absBody && upperWick <= absBody*0.5 && body > 0 {
		patterns = append(patterns, "hammer")
	}

	// Bullish Engulfing: Current bullish candle completely engulfs previous bearish candle
	prevBody := previous.Close - previous.Open
	if prevBody < 0 && body > 0 { // Previous bearish, current bullish
		if current.Open <= previous.Close && current.Close >= previous.Open {
			patterns = append(patterns, "bullish_engulfing")
		}
	}

	// Pin Bar / Doji with long lower wick: Shows rejection
	if lowerWick >= candleRange*0.6 && absBody <= candleRange*0.2 {
		patterns = append(patterns, "bullish_pin_bar")
	}

	// Strong bullish candle (>60% of range is body, closes near high)
	if body > 0 && body >= candleRange*0.6 && (current.High-current.Close) <= candleRange*0.1 {
		patterns = append(patterns, "strong_bullish")
	}

	return patterns
}

// detectBearishPatterns identifies bearish candlestick patterns
func (tc *TriggerChecker) detectBearishPatterns(current, previous exchange.Kline) []string {
	var patterns []string

	body := current.Close - current.Open
	upperWick := current.High - math.Max(current.Open, current.Close)
	lowerWick := math.Min(current.Open, current.Close) - current.Low
	candleRange := current.High - current.Low

	if candleRange == 0 {
		return patterns
	}

	absBody := math.Abs(body)

	// Shooting Star: Small body at bottom, long upper wick (>2x body), small lower wick
	if upperWick >= 2*absBody && lowerWick <= absBody*0.5 && body < 0 {
		patterns = append(patterns, "shooting_star")
	}

	// Bearish Engulfing: Current bearish candle completely engulfs previous bullish candle
	prevBody := previous.Close - previous.Open
	if prevBody > 0 && body < 0 { // Previous bullish, current bearish
		if current.Open >= previous.Close && current.Close <= previous.Open {
			patterns = append(patterns, "bearish_engulfing")
		}
	}

	// Pin Bar / Doji with long upper wick: Shows rejection
	if upperWick >= candleRange*0.6 && absBody <= candleRange*0.2 {
		patterns = append(patterns, "bearish_pin_bar")
	}

	// Strong bearish candle (>60% of range is body, closes near low)
	if body < 0 && absBody >= candleRange*0.6 && (current.Close-current.Low) <= candleRange*0.1 {
		patterns = append(patterns, "strong_bearish")
	}

	return patterns
}

// CheckAllPendingSignals checks trigger conditions for all waiting signals in the store
// Returns a map of symbol -> TriggerResult for signals that are now ready to execute
func (tc *TriggerChecker) CheckAllPendingSignals(store *SignalStore, getData func(symbol string) (*market.MarketData, error)) map[string]*TriggerResult {
	readySignals := make(map[string]*TriggerResult)

	waitingSignals := store.GetWaitingSignals()
	for _, signal := range waitingSignals {
		// Get current market data for this symbol
		data, err := getData(signal.Symbol)
		if err != nil {
			log.Printf("[TriggerChecker] Error getting data for %s: %v", signal.Symbol, err)
			continue
		}

		// Check triggers
		result := tc.CheckTriggers(signal, data)

		if result.ReadyToExecute {
			readySignals[signal.Symbol] = result
			store.UpdateSignalStatus(signal.Symbol, SignalTriggered)
			log.Printf("[TriggerChecker] %s: All triggers met! Signal ready for execution. %s",
				signal.Symbol, signal.GetSummary())
		}
	}

	return readySignals
}
