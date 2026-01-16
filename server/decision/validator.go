package decision

import (
	"fmt"
	"log"
)

// ValidActions is the set of valid trading actions
var ValidActions = map[string]bool{
	ActionOpenLong:   true,
	ActionOpenShort:  true,
	ActionCloseLong:  true,
	ActionCloseShort: true,
	ActionHold:       true,
	ActionWait:       true,
}

// ValidateDecisions validates all decisions
func ValidateDecisions(decisions []Decision, cfg *ValidationConfig) error {
	if cfg == nil {
		cfg = DefaultValidationConfig()
	}

	for i := range decisions {
		if err := ValidateDecision(&decisions[i], cfg); err != nil {
			return fmt.Errorf("decision #%d validation failed: %w", i+1, err)
		}
	}
	return nil
}

// ValidateDecision validates a single decision
func ValidateDecision(d *Decision, cfg *ValidationConfig) error {
	// Validate action
	if !ValidActions[d.Action] {
		return fmt.Errorf("invalid action: %s", d.Action)
	}

	// Only validate opening actions
	if d.Action == ActionOpenLong || d.Action == ActionOpenShort {
		return validateOpeningDecision(d, cfg)
	}

	return nil
}

// validateOpeningDecision validates decisions that open new positions
func validateOpeningDecision(d *Decision, cfg *ValidationConfig) error {
	// CRITICAL: Reject "ALL" symbol for opening positions
	// "ALL" is only valid for wait/hold actions, never for actual trades
	if d.Symbol == "ALL" || d.Symbol == "" {
		return fmt.Errorf("invalid symbol '%s' for opening position - cannot trade on ALL/empty symbol", d.Symbol)
	}

	// Determine max leverage and position ratio based on symbol
	maxLeverage := cfg.AltcoinLeverage
	posRatio := cfg.AltcoinPosRatio
	minPositionSize := cfg.MinPositionAlt
	maxPositionValue := cfg.AccountEquity * posRatio

	if isBTCOrETH(d.Symbol) {
		maxLeverage = cfg.BTCETHLeverage
		posRatio = cfg.BTCETHPosRatio
		minPositionSize = cfg.MinPositionBTCETH
		maxPositionValue = cfg.AccountEquity * posRatio
	}

	// Leverage validation - REJECT instead of auto-adjust to prevent silent risk changes
	if d.Leverage <= 0 {
		return fmt.Errorf("leverage must be greater than 0: %d", d.Leverage)
	}
	if d.Leverage > maxLeverage {
		return fmt.Errorf("%s leverage %dx exceeds maximum %dx - rejecting trade to prevent unintended risk",
			d.Symbol, d.Leverage, maxLeverage)
	}

	// Position size validation
	if d.PositionSizeUSD <= 0 {
		return fmt.Errorf("position size must be greater than 0: %.2f", d.PositionSizeUSD)
	}

	// Minimum position size checks
	if d.PositionSizeUSD < minPositionSize {
		if isBTCOrETH(d.Symbol) {
			return fmt.Errorf("%s opening amount too small (%.2f USDT), must be >= %.2f USDT",
				d.Symbol, d.PositionSizeUSD, minPositionSize)
		}
		return fmt.Errorf("opening amount too small (%.2f USDT), must be >= %.2f USDT",
			d.PositionSizeUSD, minPositionSize)
	}

	// SECURITY: Maximum position value validation (STRICT - no tolerance)
	// Removed 1% tolerance to prevent consistent over-leverage abuse
	if d.PositionSizeUSD > maxPositionValue {
		if isBTCOrETH(d.Symbol) {
			return fmt.Errorf("BTC/ETH single coin position value cannot exceed %.2f USDT (%.1fx account equity), actual: %.2f",
				maxPositionValue, posRatio, d.PositionSizeUSD)
		}
		return fmt.Errorf("altcoin single coin position value cannot exceed %.2f USDT (%.1fx account equity), actual: %.2f",
			maxPositionValue, posRatio, d.PositionSizeUSD)
	}

	// Stop-loss and take-profit validation
	if d.StopLoss <= 0 || d.TakeProfit <= 0 {
		return fmt.Errorf("stop loss and take profit must be greater than 0")
	}

	// Direction-specific SL/TP checks
	if d.Action == ActionOpenLong {
		if d.StopLoss >= d.TakeProfit {
			return fmt.Errorf("for long positions, stop loss price must be less than take profit price")
		}
	} else {
		if d.StopLoss <= d.TakeProfit {
			return fmt.Errorf("for short positions, stop loss price must be greater than take profit price")
		}
	}

	// Risk/Reward ratio validation
	if err := validateRiskReward(d, cfg.MinRiskReward); err != nil {
		return err
	}

	return nil
}

// validateRiskReward validates the risk/reward ratio of a decision
// For decisions with absolute SL/TP prices, we estimate R:R using the midpoint as entry
func validateRiskReward(d *Decision, minRatio float64) error {
	if minRatio <= 0 {
		return nil // Validation disabled
	}

	// Skip if SL/TP not provided (will use percentage-based defaults at execution)
	if d.StopLoss <= 0 || d.TakeProfit <= 0 {
		return nil
	}

	// For absolute prices, estimate entry as midpoint and validate R:R
	// This catches obviously bad R:R ratios (e.g., SL far from TP in wrong direction)
	if d.Action == ActionOpenLong {
		// For long: SL should be below TP (already validated in validateOpeningDecision)
		// Estimate entry as geometric mean of SL and TP for R:R calculation
		entryEstimate := (d.StopLoss + d.TakeProfit) / 2
		risk := entryEstimate - d.StopLoss
		reward := d.TakeProfit - entryEstimate

		if risk <= 0 {
			return fmt.Errorf("invalid long position: SL (%.2f) should be below estimated entry (%.2f)", d.StopLoss, entryEstimate)
		}

		ratio := reward / risk
		if ratio < minRatio {
			log.Printf("WARNING: Long R:R ratio %.2f:1 below minimum %.2f:1 (SL=%.2f, TP=%.2f, est.entry=%.2f)",
				ratio, minRatio, d.StopLoss, d.TakeProfit, entryEstimate)
			return fmt.Errorf("risk-reward ratio %.2f:1 below minimum %.2f:1 for long position", ratio, minRatio)
		}
	} else if d.Action == ActionOpenShort {
		// For short: SL should be above TP (already validated in validateOpeningDecision)
		// Estimate entry as midpoint
		entryEstimate := (d.StopLoss + d.TakeProfit) / 2
		risk := d.StopLoss - entryEstimate
		reward := entryEstimate - d.TakeProfit

		if risk <= 0 {
			return fmt.Errorf("invalid short position: SL (%.2f) should be above estimated entry (%.2f)", d.StopLoss, entryEstimate)
		}

		ratio := reward / risk
		if ratio < minRatio {
			log.Printf("WARNING: Short R:R ratio %.2f:1 below minimum %.2f:1 (SL=%.2f, TP=%.2f, est.entry=%.2f)",
				ratio, minRatio, d.StopLoss, d.TakeProfit, entryEstimate)
			return fmt.Errorf("risk-reward ratio %.2f:1 below minimum %.2f:1 for short position", ratio, minRatio)
		}
	}

	return nil
}

// isBTCOrETH checks if symbol is BTC or ETH
func isBTCOrETH(symbol string) bool {
	return symbol == "BTCUSDT" || symbol == "ETHUSDT" ||
		symbol == "BTC-USDT" || symbol == "ETH-USDT" ||
		symbol == "BTCUSD" || symbol == "ETHUSD"
}

// IsOpeningAction checks if action opens a new position
func IsOpeningAction(action string) bool {
	return action == ActionOpenLong || action == ActionOpenShort
}

// IsClosingAction checks if action closes a position
func IsClosingAction(action string) bool {
	return action == ActionCloseLong || action == ActionCloseShort
}

// IsPassiveAction checks if action is passive (hold/wait)
func IsPassiveAction(action string) bool {
	return action == ActionHold || action == ActionWait
}

// GetActionDirection returns "long" or "short" for an action
func GetActionDirection(action string) string {
	switch action {
	case ActionOpenLong, ActionCloseLong:
		return "long"
	case ActionOpenShort, ActionCloseShort:
		return "short"
	default:
		return ""
	}
}
