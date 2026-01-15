package coinank

import "fmt"

// OIInterpretation represents the market interpretation of OI + Price changes
type OIInterpretation struct {
	Signal      string // BULLISH, BEARISH, REVERSAL_UP, REVERSAL_DOWN
	Description string
	Confidence  string // HIGH, MEDIUM, LOW
	Warning     string // Optional warning message
}

// InterpretOI analyzes OI change combined with price change to determine market sentiment
// This is the core logic that reveals real money flow, not just price action
func InterpretOI(oiChange float64, priceChange float64) OIInterpretation {
	// OI Up + Price Up = Strong bullish (new longs opening, capital flowing into long positions)
	if oiChange > 0 && priceChange > 0 {
		confidence := "MEDIUM"
		if oiChange > 2 && priceChange > 1 {
			confidence = "HIGH"
		}
		return OIInterpretation{
			Signal:      "BULLISH",
			Description: "New longs opening - capital flowing into long positions (strong bullish trend)",
			Confidence:  confidence,
		}
	}

	// OI Up + Price Down = Strong bearish (new shorts opening, capital flowing into short positions)
	if oiChange > 0 && priceChange < 0 {
		confidence := "MEDIUM"
		if oiChange > 2 && priceChange < -1 {
			confidence = "HIGH"
		}
		return OIInterpretation{
			Signal:      "BEARISH",
			Description: "New shorts opening - capital flowing into short positions (strong bearish trend)",
			Confidence:  confidence,
		}
	}

	// OI Down + Price Up = Shorts covering (potential reversal, short squeeze)
	if oiChange < 0 && priceChange > 0 {
		warning := ""
		if oiChange < -3 {
			warning = "⚠️ Large short covering - be cautious of reversal after squeeze completes"
		}
		return OIInterpretation{
			Signal:      "REVERSAL_UP",
			Description: "Shorts covering/closing - potential short squeeze, but NOT new buying",
			Confidence:  "LOW",
			Warning:     warning,
		}
	}

	// OI Down + Price Down = Longs closing (potential reversal, long capitulation)
	if oiChange < 0 && priceChange < 0 {
		warning := ""
		if oiChange < -3 {
			warning = "⚠️ Large long capitulation - price may bounce after selling exhaustion"
		}
		return OIInterpretation{
			Signal:      "REVERSAL_DOWN",
			Description: "Longs closing/stopping out - capitulation, but NOT new shorting",
			Confidence:  "LOW",
			Warning:     warning,
		}
	}

	// Neutral case
	return OIInterpretation{
		Signal:      "NEUTRAL",
		Description: "No significant OI movement - market indecision",
		Confidence:  "LOW",
	}
}

// FormatOIForAI formats OI data for AI consumption with clear interpretation
func FormatOIForAI(oi *OIData, priceChange float64) string {
	if oi == nil {
		return ""
	}

	interpretation := InterpretOI(oi.Change1H, priceChange)

	result := "--- OPEN INTEREST ANALYSIS ---\n"
	result += "⚠️ OI reveals REAL money flow, not just price action!\n\n"

	result += "Current OI: $" + formatLargeNumber(oi.OpenInterest) + "\n"
	result += "OI Change (1H): " + formatPercent(oi.Change1H) + "\n"
	result += "OI Change (4H): " + formatPercent(oi.Change4H) + "\n"
	result += "OI Change (24H): " + formatPercent(oi.Change24H) + "\n\n"

	result += "📊 INTERPRETATION:\n"
	result += "Signal: " + interpretation.Signal + " (" + interpretation.Confidence + " confidence)\n"
	result += "Meaning: " + interpretation.Description + "\n"

	if interpretation.Warning != "" {
		result += interpretation.Warning + "\n"
	}

	// Add trading guidance based on interpretation
	result += "\n💡 TRADING GUIDANCE:\n"
	switch interpretation.Signal {
	case "BULLISH":
		result += "✅ OI supports LONG entries - new money flowing into longs\n"
		result += "- Trend is backed by real capital inflow\n"
		result += "- Safe to enter LONG if other conditions align\n"
	case "BEARISH":
		result += "✅ OI supports SHORT entries - new money flowing into shorts\n"
		result += "- Downtrend is backed by real capital inflow\n"
		result += "- Safe to enter SHORT if other conditions align\n"
	case "REVERSAL_UP":
		result += "⚠️ CAUTION for LONG entries - price up but OI down\n"
		result += "- This is SHORT COVERING, not new longs buying\n"
		result += "- Trend may reverse once covering completes\n"
		result += "- Prefer to WAIT for OI to turn positive before entering\n"
	case "REVERSAL_DOWN":
		result += "⚠️ CAUTION for SHORT entries - price down but OI down\n"
		result += "- This is LONG CAPITULATION, not new shorts selling\n"
		result += "- Trend may reverse once capitulation completes\n"
		result += "- Prefer to WAIT for OI to turn positive before entering\n"
	default:
		result += "⚠️ No clear OI signal - market in transition\n"
		result += "- Wait for clearer OI direction before trading\n"
	}

	result += "\n"
	return result
}

// FormatLiquidationForAI formats liquidation data for AI consumption
func FormatLiquidationForAI(liq *LiquidationData) string {
	if liq == nil {
		return ""
	}

	result := "--- LIQUIDATION ANALYSIS (24H) ---\n"
	result += "Long Liquidations: $" + formatLargeNumber(liq.LongLiquidation) + " (" + formatPercent(liq.LongRatio) + ")\n"
	result += "Short Liquidations: $" + formatLargeNumber(liq.ShortLiquidation) + " (" + formatPercent(liq.ShortRatio) + ")\n"
	result += "Total Liquidations: $" + formatLargeNumber(liq.TotalLiquidation) + "\n\n"

	// Add interpretation
	if liq.LongRatio > 70 {
		result += "⚠️ HEAVY LONG LIQUIDATIONS - potential bottom forming (forced longs exhausted)\n"
		result += "💡 Consider: May be near a reversal point as weak longs are flushed out\n"
	} else if liq.ShortRatio > 70 {
		result += "⚠️ HEAVY SHORT LIQUIDATIONS - potential top forming (shorts squeezed)\n"
		result += "💡 Consider: May be near a reversal point as shorts are squeezed out\n"
	} else {
		result += "📊 Balanced liquidations - no extreme positioning\n"
	}

	result += "\n"
	return result
}

// FormatLongShortRatioForAI formats long/short ratio for AI consumption
func FormatLongShortRatioForAI(lsr *LongShortRatioData) string {
	if lsr == nil {
		return ""
	}

	result := "--- LONG/SHORT RATIO ---\n"
	result += "Longs: " + formatPercent(lsr.LongRatio) + " | Shorts: " + formatPercent(lsr.ShortRatio) + "\n"
	result += "L/S Ratio: " + formatFloat(lsr.LongShort) + "\n\n"

	// Add crowding warning
	if lsr.LongRatio > 70 {
		result += "⚠️ CROWDED LONG - " + formatPercent(lsr.LongRatio) + " of traders are long\n"
		result += "💡 Contrarian signal: Too many longs = potential for reversal down\n"
		result += "💡 Recommendation: Be cautious with LONG entries, consider waiting for pullback\n"
	} else if lsr.ShortRatio > 70 {
		result += "⚠️ CROWDED SHORT - " + formatPercent(lsr.ShortRatio) + " of traders are short\n"
		result += "💡 Contrarian signal: Too many shorts = potential for squeeze up\n"
		result += "💡 Recommendation: Be cautious with SHORT entries, shorts may get squeezed\n"
	} else if lsr.LongRatio > 60 {
		result += "📊 Slightly long-heavy positioning - moderate bullish sentiment\n"
	} else if lsr.ShortRatio > 60 {
		result += "📊 Slightly short-heavy positioning - moderate bearish sentiment\n"
	} else {
		result += "📊 Balanced positioning - no extreme crowding\n"
	}

	result += "\n"
	return result
}

// Helper functions
func formatLargeNumber(n float64) string {
	if n >= 1_000_000_000 {
		return formatFloat(n/1_000_000_000) + "B"
	}
	if n >= 1_000_000 {
		return formatFloat(n/1_000_000) + "M"
	}
	if n >= 1_000 {
		return formatFloat(n/1_000) + "K"
	}
	return formatFloat(n)
}

func formatPercent(p float64) string {
	if p >= 0 {
		return "+" + formatFloat(p) + "%"
	}
	return formatFloat(p) + "%"
}

func formatFloat(f float64) string {
	return fmt.Sprintf("%.2f", f)
}
