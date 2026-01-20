package market

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"auto-trader-ahh/decision"
	"auto-trader-ahh/exchange"
)

type MarketData struct {
	Symbol         string
	CurrentPrice   float64
	Klines         []exchange.Kline
	EMA9           float64
	EMA21          float64
	RSI            float64
	MACD           float64
	MACDSignal     float64
	MACDHist       float64
	ATR            float64
	Volume24h      float64
	PriceChange24h float64
	Trend          string // BULLISH, BEARISH, NEUTRAL
	BTCPrice       float64
	BTCChange24h   float64
	// Funding rate info
	FundingRate     float64 // Current funding rate (e.g., 0.0001 = 0.01%)
	NextFundingTime int64   // Unix timestamp of next funding
	// Open Interest data (from Binance FREE API)
	OIValue       float64 // Total OI in USD
	OIChange1H    float64 // OI change in last 1 hour (%)
	OIChange4H    float64 // OI change in last 4 hours (%)
	OIChange24H   float64 // OI change in last 24 hours (%)
	OISignal      string  // BULLISH, BEARISH, REVERSAL_UP, REVERSAL_DOWN, NEUTRAL
	OIDescription string  // Human-readable interpretation
	LongRatio     float64 // % of traders long
	ShortRatio    float64 // % of traders short
	// Sentiment Analysis (Trend of retail positioning)
	SentimentTrend   string // BECOMING_BULLISH, BECOMING_BEARISH, STABLE
	SentimentMessage string // Description of sentiment shift (e.g. "Retail FOMO detected")
	// Inferred Liquidation Detection (FREE - derived from OI + Price)
	LiquidationPressure string // LONG_LIQUIDATION, SHORT_LIQUIDATION, NONE
	LiquidationSeverity string // HIGH, MEDIUM, LOW, NONE
	LiquidationMessage  string // Human-readable explanation
	// Move Maturity Indicator (Task 1.2)
	// Tracks how "old" the current trend move is based on EMA crossover
	// Reference: https://www.stockforecasttoday.com/post/swing-trading-examples-using-cycle-timing-and-price-structure
	// "Short-term cycle trades might last 3–7 sessions"
	MoveMaturity      string // EARLY (1-7 candles), MID (8-14), LATE (15-21), EXHAUSTED (>21)
	CandlesSinceCross int    // Number of candles since last EMA9/EMA21 crossover
	MoveMaturityScore int    // 1-4 score (1=EARLY best for entry, 4=EXHAUSTED worst)

	// Entry Timing Indicators (Task 3.1)
	// Reference: https://capital.com/en-int/learn/trading-strategies/pullback-trading
	DistanceFromEMA9Pct    float64 // How far price is from EMA9 (negative = below, positive = above)
	DistanceFromEMA21Pct   float64 // How far price is from EMA21
	RSIRateOfChange        float64 // RSI change over last 3 candles (positive = rising)
	CandlesSinceRSIExtreme int     // Candles since RSI > 70 or < 30
	PullbackDepthPct       float64 // Current pullback depth from recent high/low
	MomentumDecay          bool    // True if MACD histogram declining while price rises (divergence)
	MomentumDecayBars      int     // Number of bars showing momentum decay

	// Market Regime Detection (Task 4.1)
	// Reference: https://www.investopedia.com/terms/a/adx.asp
	MarketRegime      string  // TRENDING, RANGING, VOLATILE, EXHAUSTED
	ADX               float64 // Average Directional Index (>25 = trending)
	ADXTrend          string  // STRONG, MODERATE, WEAK
	BollingerSqueeze  bool    // True if Bollinger Bands are squeezed (low volatility, breakout coming)
	BollingerWidth    float64 // Current Bollinger Band width percentage
	ATRRatio          float64 // Current ATR vs 20-period average (>2.0 = high volatility)
	RegimeDescription string  // Human-readable regime explanation

	// Fibonacci Retracement Detection (Task 3.2)
	// Reference: https://www.investopedia.com/terms/f/fibonacciretracement.asp
	FibSwingHigh    float64 // Recent swing high
	FibSwingLow     float64 // Recent swing low
	FibLevel382     float64 // 38.2% retracement price level
	FibLevel500     float64 // 50% retracement price level
	FibLevel618     float64 // 61.8% retracement price level
	NearestFibLevel string  // Which Fib level price is closest to (38.2, 50, 61.8, NONE)
	FibDistancePct  float64 // Distance from nearest Fib level (%)
	AtFibSupport    bool    // True if price is at a Fib support level (for longs)
	AtFibResistance bool    // True if price is at a Fib resistance level (for shorts)

	// Volume Profile on Pullback (Task 3.3)
	// Reference: https://capital.com/en-int/learn/trading-strategies/pullback-trading
	PullbackVolumeRatio  float64 // Pullback volume vs trend volume (< 1.0 = healthy pullback)
	PullbackVolumeSignal string  // HEALTHY, UNHEALTHY, NEUTRAL
	VolumeConfirmation   bool    // True if volume pattern confirms entry
}

type DataProvider struct {
	binance *exchange.BinanceClient
}

func NewDataProvider(binance *exchange.BinanceClient) *DataProvider {
	return &DataProvider{
		binance: binance,
	}
}

// GetMarketData fetches and analyzes market data for a symbol (default config)
func (d *DataProvider) GetMarketData(ctx context.Context, symbol string) (*MarketData, error) {
	return d.GetMarketDataWithConfig(ctx, symbol, "5m", 100)
}

// GetMarketDataWithConfig fetches market data with custom timeframe and count
func (d *DataProvider) GetMarketDataWithConfig(ctx context.Context, symbol, timeframe string, count int) (*MarketData, error) {
	// Get klines
	klines, err := d.binance.GetKlines(ctx, symbol, timeframe, count)
	if err != nil {
		return nil, fmt.Errorf("failed to get klines: %w", err)
	}

	if len(klines) < 26 {
		return nil, fmt.Errorf("not enough kline data")
	}

	// Get current price
	ticker, err := d.binance.GetTicker(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to get ticker: %w", err)
	}

	// Calculate indicators
	closes := make([]float64, len(klines))
	highs := make([]float64, len(klines))
	lows := make([]float64, len(klines))
	volumes := make([]float64, len(klines))

	for i, k := range klines {
		closes[i] = k.Close
		highs[i] = k.High
		lows[i] = k.Low
		volumes[i] = k.Volume
	}

	ema9 := calculateEMA(closes, 9)
	ema21 := calculateEMA(closes, 21)
	rsi := calculateRSI(closes, 14)
	macd, signal, hist := calculateMACD(closes)
	atr := calculateATR(highs, lows, closes, 14)

	// Calculate 24h stats
	volume24h := 0.0
	for _, v := range volumes {
		volume24h += v
	}

	priceChange24h := 0.0
	if len(closes) > 0 && closes[0] != 0 {
		priceChange24h = ((closes[len(closes)-1] - closes[0]) / closes[0]) * 100
	}

	// Determine trend
	trend := "NEUTRAL"
	if ema9 > ema21 && rsi > 50 {
		trend = "BULLISH"
	} else if ema9 < ema21 && rsi < 50 {
		trend = "BEARISH"
	}

	// Fetch funding rate info (non-blocking, continue if fails)
	var fundingRate float64
	var nextFundingTime int64
	if fundingInfo, err := d.binance.GetFundingInfo(ctx, symbol); err == nil {
		fundingRate = fundingInfo.LastFundingRate
		nextFundingTime = fundingInfo.NextFundingTime
	}

	// Calculate Move Maturity (Task 1.2)
	// Count candles since last EMA9/EMA21 crossover to determine how "old" the move is
	// Reference: https://www.stockforecasttoday.com/post/swing-trading-etfs-with-cycle-timing-how-to-avoid-late-entries-near-market-tops
	// Note: Thresholds are scaled by timeframe - research was for daily charts (1 session = 1 day)
	candlesSinceCross, moveMaturity, maturityScore := calculateMoveMaturity(closes, 9, 21, timeframe)

	// Calculate Entry Timing Indicators (Task 3.1)
	distanceFromEMA9 := calculateDistanceFromEMA(ticker.Price, ema9)
	distanceFromEMA21 := calculateDistanceFromEMA(ticker.Price, ema21)
	rsiRateOfChange := calculateRSIRateOfChange(closes, 14, 3)
	candlesSinceRSIExtreme := calculateCandlesSinceRSIExtreme(closes, 14, 70, 30)
	isUptrend := ema9 > ema21
	pullbackDepth := calculatePullbackDepth(highs, lows, closes, 20, isUptrend)
	momentumDecay, momentumDecayBars := detectMomentumDecay(closes, 5)

	// Calculate Market Regime Indicators (Task 4.1)
	adx, adxTrend := calculateADX(highs, lows, closes, 14)
	bollingerWidth, bollingerSqueeze := calculateBollingerBands(closes, 20, 2.0)
	atrRatio := calculateATRRatio(highs, lows, closes, 14)
	marketRegime, regimeDescription := detectMarketRegime(adx, adxTrend, bollingerSqueeze, atrRatio, momentumDecay)

	// Calculate Fibonacci Levels (Task 3.2)
	fibLevels := calculateFibonacciLevels(highs, lows, ticker.Price, 40, isUptrend)

	// Analyze Volume Profile (Task 3.3)
	volumeProfile := analyzeVolumeProfile(volumes, closes, 20, isUptrend)

	return &MarketData{
		Symbol:            symbol,
		CurrentPrice:      ticker.Price,
		Klines:            klines,
		EMA9:              ema9,
		EMA21:             ema21,
		RSI:               rsi,
		MACD:              macd,
		MACDSignal:        signal,
		MACDHist:          hist,
		ATR:               atr,
		Volume24h:         volume24h,
		PriceChange24h:    priceChange24h,
		Trend:             trend,
		FundingRate:       fundingRate,
		NextFundingTime:   nextFundingTime,
		MoveMaturity:      moveMaturity,
		CandlesSinceCross: candlesSinceCross,
		MoveMaturityScore: maturityScore,
		// Entry Timing Indicators (Task 3.1)
		DistanceFromEMA9Pct:    distanceFromEMA9,
		DistanceFromEMA21Pct:   distanceFromEMA21,
		RSIRateOfChange:        rsiRateOfChange,
		CandlesSinceRSIExtreme: candlesSinceRSIExtreme,
		PullbackDepthPct:       pullbackDepth,
		MomentumDecay:          momentumDecay,
		MomentumDecayBars:      momentumDecayBars,
		// Market Regime Detection (Task 4.1)
		MarketRegime:      marketRegime,
		ADX:               adx,
		ADXTrend:          adxTrend,
		BollingerSqueeze:  bollingerSqueeze,
		BollingerWidth:    bollingerWidth,
		ATRRatio:          atrRatio,
		RegimeDescription: regimeDescription,
		// Fibonacci Levels (Task 3.2)
		FibSwingHigh:    fibLevels.SwingHigh,
		FibSwingLow:     fibLevels.SwingLow,
		FibLevel382:     fibLevels.Level382,
		FibLevel500:     fibLevels.Level500,
		FibLevel618:     fibLevels.Level618,
		NearestFibLevel: fibLevels.NearestLevel,
		FibDistancePct:  fibLevels.DistancePct,
		AtFibSupport:    fibLevels.AtSupport,
		AtFibResistance: fibLevels.AtResistance,
		// Volume Profile (Task 3.3)
		PullbackVolumeRatio:  volumeProfile.PullbackVolumeRatio,
		PullbackVolumeSignal: volumeProfile.Signal,
		VolumeConfirmation:   volumeProfile.Confirmation,
	}, nil
}

// AIPromptConfig contains configurable thresholds for AI prompt warnings
type AIPromptConfig struct {
	MinEMASpreadPct      float64 // Min EMA spread before warning (default: 0.15)
	MinVolumeRatioPct    float64 // Min volume ratio before warning (default: 40)
	ResistanceSupportPct float64 // Distance from high/low to warn (default: 1.0)
	EnableEntryWarnings  bool    // Enable/disable entry quality warnings (default: true)
}

// DefaultAIPromptConfig returns default AI prompt configuration
func DefaultAIPromptConfig() AIPromptConfig {
	return AIPromptConfig{
		MinEMASpreadPct:      0.15,
		MinVolumeRatioPct:    40.0,
		ResistanceSupportPct: 1.0,
		EnableEntryWarnings:  true,
	}
}

// FormatForAI formats market data as a string for AI analysis
func (d *DataProvider) FormatForAI(data *MarketData, enableHighWickWarning bool, cfg AIPromptConfig) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("=== %s Market Analysis ===\n\n", data.Symbol))
	sb.WriteString(fmt.Sprintf("Current Price: $%.2f\n", data.CurrentPrice))
	sb.WriteString(fmt.Sprintf("24h Price Change: %.2f%%\n", data.PriceChange24h))
	sb.WriteString(fmt.Sprintf("24h Volume: $%.2f\n\n", data.Volume24h))

	// Provide recent high/low context (facts only, AI decides what to do)
	if len(data.Klines) >= 10 {
		recentCandles := data.Klines[len(data.Klines)-10:]

		// Find the highest high and lowest low in recent candles
		var recentHigh, recentLow float64
		recentHigh = recentCandles[0].High
		recentLow = recentCandles[0].Low

		for _, candle := range recentCandles {
			if candle.High > recentHigh {
				recentHigh = candle.High
			}
			if candle.Low < recentLow {
				recentLow = candle.Low
			}
		}

		// Check how far current price is from the recent high/low
		pctFromRecentHigh := ((recentHigh - data.CurrentPrice) / recentHigh) * 100
		pctFromRecentLow := ((data.CurrentPrice - recentLow) / recentLow) * 100

		sb.WriteString("--- Recent Range (Last 10 Candles) ---\n")
		sb.WriteString(fmt.Sprintf("Recent High: $%.2f (current is %.2f%% below)\n", recentHigh, pctFromRecentHigh))
		if pctFromRecentHigh < 0.2 {
			sb.WriteString("⚠️ DANGER: Price is AT RESISTANCE (Recent High). Do not FOMO BUY unless breakout is confirmed.\n")
		}

		sb.WriteString(fmt.Sprintf("Recent Low: $%.2f (current is %.2f%% above)\n", recentLow, pctFromRecentLow))
		if pctFromRecentLow < 0.2 {
			sb.WriteString("⚠️ DANGER: Price is AT SUPPORT (Recent Low). Do not FOMO SELL unless breakdown is confirmed.\n")
		}
		sb.WriteString("\n")
	}

	if data.BTCPrice > 0 {
		sb.WriteString("--- BTC Market Context ---\n")
		sb.WriteString(fmt.Sprintf("BTC Price: $%.2f\n", data.BTCPrice))
		sb.WriteString(fmt.Sprintf("BTC 24h Change: %.2f%%\n", data.BTCChange24h))
		sb.WriteString("\n")
	}

	// Funding Rate Info - Critical for profit calculation
	if data.FundingRate != 0 || data.NextFundingTime > 0 {
		sb.WriteString("--- Funding Rate Info ---\n")
		fundingPct := data.FundingRate * 100 // Convert to percentage
		sb.WriteString(fmt.Sprintf("Current Funding Rate: %.4f%%\n", fundingPct))

		// Calculate annualized rate (3 fundings per day × 365 days)
		annualizedRate := fundingPct * 3 * 365
		sb.WriteString(fmt.Sprintf("Annualized Rate: %.2f%%\n", annualizedRate))

		// Time until next funding
		if data.NextFundingTime > 0 {
			nextFundingUnix := data.NextFundingTime / 1000 // Convert ms to seconds
			now := time.Now().Unix()
			minsUntilFunding := (nextFundingUnix - now) / 60
			hoursUntilFunding := minsUntilFunding / 60
			minsRemainder := minsUntilFunding % 60
			sb.WriteString(fmt.Sprintf("Next Funding In: %dh %dm\n", hoursUntilFunding, minsRemainder))
		}

		// Trading cost guidance
		absFundingPct := fundingPct
		if absFundingPct < 0 {
			absFundingPct = -absFundingPct
		}

		// Trading fees (entry + exit) ≈ 0.08% for futures
		tradingFeePct := 0.08
		totalCostPct := absFundingPct + tradingFeePct

		sb.WriteString(fmt.Sprintf("Est. Trading Fees: %.2f%% (entry+exit)\n", tradingFeePct))
		sb.WriteString(fmt.Sprintf("⚠️ Minimum Profit to Break Even: %.2f%%\n", totalCostPct))

		if fundingPct > 0.03 {
			sb.WriteString("🔴 HIGH POSITIVE FUNDING: Longs pay fees. High fees = crowded trade. Watch for reversals.\n")
		} else if fundingPct < -0.03 {
			sb.WriteString("🟢 HIGH NEGATIVE FUNDING: Shorts pay fees. Watch for short squeezes.\n")
		} else {
			sb.WriteString("🟡 Neutral funding rate.\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("--- Technical Indicators ---\n")
	sb.WriteString(fmt.Sprintf("EMA 9: $%.2f\n", data.EMA9))
	sb.WriteString(fmt.Sprintf("EMA 21: $%.2f\n", data.EMA21))

	// Calculate trend strength
	emaSpread := ((data.EMA9 - data.EMA21) / data.EMA21) * 100
	absEmaSpread := emaSpread
	if absEmaSpread < 0 {
		absEmaSpread = -absEmaSpread
	}

	// Check Price vs EMA relation (Immediate Trend)

	if data.EMA9 > data.EMA21 {
		sb.WriteString(fmt.Sprintf("EMA Trend: BULLISH (EMA9 > EMA21 by %.2f%%)\n", emaSpread))

		// Add Price Action Context
		if data.CurrentPrice < data.EMA9 {
			sb.WriteString("⚠️ WARNING: Price is BELOW EMA9. Constructive pullback or starting reversal?\n")
		} else {
			sb.WriteString("✅ Price is ABOVE EMA9 (Strong Momentum).\n")
		}

		if emaSpread > 0.8 {
			sb.WriteString("📈 Strong bullish structure.\n")
		} else if emaSpread > 0.2 {
			sb.WriteString("📊 Moderate bullish structure.\n")
		} else {
			sb.WriteString("📊 Mild/Choppy trend - smaller positions recommended.\n")
		}
	} else {
		sb.WriteString(fmt.Sprintf("EMA Trend: BEARISH (EMA9 < EMA21 by %.2f%%)\n", -emaSpread))

		// Add Price Action Context
		if data.CurrentPrice > data.EMA9 {
			sb.WriteString("⚠️ WARNING: Price is ABOVE EMA9. Bearish relief rally or starting reversal?\n")
		} else {
			sb.WriteString("✅ Price is BELOW EMA9 (Strong Downside Momentum).\n")
		}

		if emaSpread < -0.8 {
			sb.WriteString("📉 Strong bearish structure.\n")
		} else if emaSpread < -0.2 {
			sb.WriteString("📊 Moderate bearish structure.\n")
		} else {
			sb.WriteString("📊 Mild/Choppy trend - smaller positions recommended.\n")
		}
	}

	// Add trend strength warning if configured and enabled
	if cfg.EnableEntryWarnings && absEmaSpread < cfg.MinEMASpreadPct {
		sb.WriteString(fmt.Sprintf("\n⚠️ Mild trend: EMA spread is only %.2f%% - consider smaller positions.\n\n", absEmaSpread))
	}

	// RSI with entry guidance
	sb.WriteString(fmt.Sprintf("RSI (14): %.2f", data.RSI))
	if data.RSI > 75 {
		sb.WriteString(" [OVERBOUGHT ⚠️ Risky for LONG]\n")
	} else if data.RSI > 65 {
		sb.WriteString(" [HIGH - Still OK for LONG with tight SL]\n")
	} else if data.RSI < 25 {
		sb.WriteString(" [OVERSOLD ⚠️ Risky for SHORT]\n")
	} else if data.RSI < 35 {
		sb.WriteString(" [LOW - Still OK for SHORT with tight SL]\n")
	} else if data.RSI > 45 && data.RSI <= 65 {
		sb.WriteString(" [BULLISH - Good for LONG]\n")
	} else if data.RSI >= 35 && data.RSI < 55 {
		sb.WriteString(" [BEARISH - Good for SHORT]\n")
	} else {
		sb.WriteString(" [NEUTRAL - Either direction OK]\n")
	}

	sb.WriteString(fmt.Sprintf("MACD: %.4f\n", data.MACD))
	sb.WriteString(fmt.Sprintf("MACD Signal: %.4f\n", data.MACDSignal))
	sb.WriteString(fmt.Sprintf("MACD Histogram: %.4f", data.MACDHist))
	if data.MACDHist > 0 && data.MACD > data.MACDSignal {
		sb.WriteString(" [BULLISH MOMENTUM ✅]\n")
	} else if data.MACDHist < 0 && data.MACD < data.MACDSignal {
		sb.WriteString(" [BEARISH MOMENTUM ✅]\n")
	} else {
		sb.WriteString(" [WEAKENING/TRANSITIONING ⚠️]\n")
	}
	sb.WriteString(fmt.Sprintf("ATR (14): %.4f (Volatility: %.2f%%)\n\n", data.ATR, (data.ATR/data.CurrentPrice)*100))

	// Overall trend assessment
	sb.WriteString(fmt.Sprintf("--- Overall Trend: %s ---\n", data.Trend))
	if data.Trend == "NEUTRAL" {
		sb.WriteString("⚠️ SIDEWAYS MARKET: NO CLEAR TREND. HOLDING IS RECOMMENDED.\n")
	}
	sb.WriteString("\n")

	// Move Maturity Indicator (Task 1.2)
	// Shows how "old" the current trend move is based on EMA crossover
	sb.WriteString("--- Move Maturity ---\n")
	sb.WriteString(fmt.Sprintf("Candles Since EMA Cross: %d\n", data.CandlesSinceCross))
	sb.WriteString(fmt.Sprintf("Move Maturity: %s", data.MoveMaturity))
	switch data.MoveMaturity {
	case "EARLY":
		sb.WriteString(" ✅ [BEST for entry - within 3-7 session sweet spot]\n")
	case "MID":
		sb.WriteString(" ✅ [OK for entry - trend confirmed but aging]\n")
	case "LATE":
		sb.WriteString(" ⚠️ [CAUTION - approaching cycle end, wait for pullback]\n")
	case "EXHAUSTED":
		sb.WriteString(" 🚫 [AVOID entry - cycle exhausted, high reversal risk]\n")
	default:
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// Open Interest Analysis (if available)
	if data.OIValue > 0 || data.OISignal != "" {
		sb.WriteString("--- OPEN INTEREST ANALYSIS ---\n")
		sb.WriteString("⚠️ OI reveals REAL money flow, not just price action!\n\n")

		if data.OIValue > 0 {
			sb.WriteString(fmt.Sprintf("Current OI: $%.2fM\n", data.OIValue/1_000_000))
		}
		sb.WriteString(fmt.Sprintf("OI Change (1H): %+.2f%%\n", data.OIChange1H))
		sb.WriteString(fmt.Sprintf("OI Change (4H): %+.2f%%\n", data.OIChange4H))
		sb.WriteString(fmt.Sprintf("OI Change (24H): %+.2f%%\n\n", data.OIChange24H))

		if data.OISignal != "" {
			sb.WriteString(fmt.Sprintf("📊 OI SIGNAL: %s\n", data.OISignal))
			if data.OIDescription != "" {
				sb.WriteString(fmt.Sprintf("Meaning: %s\n", data.OIDescription))
			}

			// Add trading guidance based on OI signal
			sb.WriteString("\n💡 OI TRADING GUIDANCE:\n")
			switch data.OISignal {
			case "BULLISH":
				sb.WriteString("✅ OI supports LONG entries - new money flowing into longs\n")
				sb.WriteString("- Trend is backed by real capital inflow\n")
			case "BEARISH":
				sb.WriteString("✅ OI supports SHORT entries - new money flowing into shorts\n")
				sb.WriteString("- Downtrend is backed by real capital inflow\n")
			case "REVERSAL_UP":
				sb.WriteString("⚠️ CAUTION for LONG entries - price up but OI down\n")
				sb.WriteString("- This is SHORT COVERING, not new longs buying\n")
				sb.WriteString("- Trend may reverse once covering completes\n")
			case "REVERSAL_DOWN":
				sb.WriteString("⚠️ CAUTION for SHORT entries - price down but OI down\n")
				sb.WriteString("- This is LONG CAPITULATION, not new shorts selling\n")
				sb.WriteString("- Trend may reverse once capitulation completes\n")
			default:
				sb.WriteString("⚠️ No clear OI signal - market in transition\n")
			}
		}

		// Long/Short Ratio
		// Long/Short Ratio
		if data.LongRatio > 0 || data.ShortRatio > 0 {
			sb.WriteString(fmt.Sprintf("\nL/S Ratio: %.1f%% Long | %.1f%% Short\n", data.LongRatio, data.ShortRatio))
			if data.LongRatio > 70 {
				sb.WriteString("⚠️ CROWDED LONG - potential for reversal down\n")
			} else if data.ShortRatio > 70 {
				sb.WriteString("⚠️ CROWDED SHORT - potential for squeeze up\n")
			}

			// Add Sentiment Shift (Trend)
			if data.SentimentTrend != "" && data.SentimentTrend != "STABLE" {
				sb.WriteString(fmt.Sprintf("📊 Sentiment Trend: %s\n", data.SentimentTrend))
				sb.WriteString(fmt.Sprintf("💡 Insight: %s\n", data.SentimentMessage))
			}
		}
		sb.WriteString("\n")
	}

	// Liquidation Analysis (inferred from OI + Price, FREE)
	if data.LiquidationPressure != "" && data.LiquidationPressure != "NONE" {
		sb.WriteString("--- LIQUIDATION ANALYSIS ---\n")
		sb.WriteString(fmt.Sprintf("⚠️ LIQUIDATION DETECTED: %s (Severity: %s)\n", data.LiquidationPressure, data.LiquidationSeverity))
		if data.LiquidationMessage != "" {
			sb.WriteString(data.LiquidationMessage + "\n")
		}

		// Add trading guidance based on liquidation type
		sb.WriteString("\n💡 LIQUIDATION TRADING GUIDANCE:\n")
		switch data.LiquidationPressure {
		case "LONG_LIQUIDATION":
			sb.WriteString("- Longs are being liquidated (forced selling)\n")
			sb.WriteString("- Price may find support once liquidations exhaust\n")
			sb.WriteString("- CAUTION on new LONG entries until liquidations settle\n")
			if data.LiquidationSeverity == "HIGH" {
				sb.WriteString("- Consider: May be near capitulation bottom\n")
			}
		case "SHORT_LIQUIDATION":
			sb.WriteString("- Shorts are being squeezed (forced covering)\n")
			sb.WriteString("- Rally may exhaust once short covering completes\n")
			sb.WriteString("- CAUTION on new LONG entries at these elevated prices\n")
			if data.LiquidationSeverity == "HIGH" {
				sb.WriteString("- Consider: Rally may be near exhaustion point\n")
			}
		}
		sb.WriteString("\n")
	}

	// Entry quality summary
	sb.WriteString("--- ENTRY QUALITY CHECK ---\n")
	longScore := 0
	shortScore := 0
	longWarnings := []string{}
	shortWarnings := []string{}

	// EMA structure
	if data.EMA9 > data.EMA21 {
		longScore++
	} else {
		shortScore++
	}

	// RSI in good range
	if data.RSI > 45 && data.RSI < 65 {
		longScore++
	}
	if data.RSI > 35 && data.RSI < 55 {
		shortScore++
	}

	// RSI extreme warnings
	if data.RSI > 75 {
		longWarnings = append(longWarnings, "RSI OVERBOUGHT (>75)")
	}
	if data.RSI < 25 {
		shortWarnings = append(shortWarnings, "RSI OVERSOLD (<25)")
	}

	// MACD momentum
	if data.MACDHist > 0 {
		longScore++
	} else {
		shortScore++
	}

	// BTC context
	if data.BTCChange24h > 0 {
		longScore++
	} else {
		shortScore++
	}

	// EXHAUSTION DETECTION: Price extended from EMA with weakening momentum
	if data.EMA9 > 0 {
		priceExtension := ((data.CurrentPrice - data.EMA9) / data.EMA9) * 100
		if priceExtension > 1.0 && data.MACDHist < 0 {
			longWarnings = append(longWarnings, fmt.Sprintf("EXHAUSTION: Extended %.1f%% above EMA9 with negative MACD", priceExtension))
		}
		if priceExtension < -1.0 && data.MACDHist > 0 {
			shortWarnings = append(shortWarnings, fmt.Sprintf("EXHAUSTION: Extended %.1f%% below EMA9 with positive MACD", -priceExtension))
		}
	}

	// EMA spread weakness (use configured threshold)
	if cfg.EnableEntryWarnings && data.EMA21 > 0 {
		emaSpread := ((data.EMA9 - data.EMA21) / data.EMA21) * 100
		absSpread := emaSpread
		if absSpread < 0 {
			absSpread = -absSpread
		}
		if absSpread < cfg.MinEMASpreadPct {
			longWarnings = append(longWarnings, fmt.Sprintf("Mild trend: EMA spread %.2f%%", absSpread))
			shortWarnings = append(shortWarnings, fmt.Sprintf("Mild trend: EMA spread %.2f%%", absSpread))
		}
	}

	// WICK REJECTION DETECTION
	if len(data.Klines) >= 5 {
		upperRejections := 0
		lowerRejections := 0
		for i := len(data.Klines) - 5; i < len(data.Klines); i++ {
			k := data.Klines[i]
			bodySize := k.Close - k.Open
			if bodySize < 0 {
				bodySize = -bodySize
			}
			if bodySize > 0 {
				maxOC := k.Open
				if k.Close > k.Open {
					maxOC = k.Close
				}
				minOC := k.Open
				if k.Close < k.Open {
					minOC = k.Close
				}
				upperWick := k.High - maxOC
				lowerWick := minOC - k.Low
				if upperWick > bodySize*0.5 {
					upperRejections++
				}
				if lowerWick > bodySize*0.5 {
					lowerRejections++
				}
			}
		}
		if upperRejections >= 3 {
			longWarnings = append(longWarnings, fmt.Sprintf("WICK REJECTION: %d/5 candles show sellers rejecting highs", upperRejections))
		}
		if lowerRejections >= 3 {
			shortWarnings = append(shortWarnings, fmt.Sprintf("WICK REJECTION: %d/5 candles show buyers defending lows", lowerRejections))
		}
	}

	// VOLUME DECLINE DETECTION (use configured threshold)
	if cfg.EnableEntryWarnings && len(data.Klines) >= 6 {
		recentVol := data.Klines[len(data.Klines)-1].Volume
		var avgVol float64
		for i := len(data.Klines) - 6; i < len(data.Klines)-1; i++ {
			avgVol += data.Klines[i].Volume
		}
		avgVol /= 5
		volRatioThreshold := cfg.MinVolumeRatioPct / 100.0 // Convert % to ratio
		if avgVol > 0 && recentVol < avgVol*volRatioThreshold {
			warning := fmt.Sprintf("LOW VOLUME: Current vol %.0f is %.0f%% below average", recentVol, (1-(recentVol/avgVol))*100)
			longWarnings = append(longWarnings, warning)
			shortWarnings = append(shortWarnings, warning)
		}
	}

	sb.WriteString(fmt.Sprintf("LONG Score: %d/4 | SHORT Score: %d/4\n", longScore, shortScore))
	if longScore >= 3 {
		sb.WriteString("✅ STRONG: CONDITIONS FAVOR LONG ENTRY\n")
	} else if shortScore >= 3 {
		sb.WriteString("✅ STRONG: CONDITIONS FAVOR SHORT ENTRY\n")
	} else if longScore >= 2 {
		sb.WriteString("📊 MODERATE: LONG entry possible with caution\n")
	} else if shortScore >= 2 {
		sb.WriteString("📊 MODERATE: SHORT entry possible with caution\n")
	} else {
		sb.WriteString("⚠️ WEAK: Mixed signals, higher risk entry\n")
	}

	// Output warnings
	if len(longWarnings) > 0 {
		sb.WriteString("\n🚨 LONG ENTRY WARNINGS:\n")
		for _, w := range longWarnings {
			sb.WriteString(fmt.Sprintf("   - %s\n", w))
		}
	}
	if len(shortWarnings) > 0 {
		sb.WriteString("\n🚨 SHORT ENTRY WARNINGS:\n")
		for _, w := range shortWarnings {
			sb.WriteString(fmt.Sprintf("   - %s\n", w))
		}
	}
	sb.WriteString("\n")

	// Recent price action - send more candles so AI can see the full picture
	candleCount := len(data.Klines)
	if candleCount > 30 {
		candleCount = 30
	}
	sb.WriteString(fmt.Sprintf("--- Recent Price Action (Last %d Candles) ---\n", candleCount))

	startIdx := len(data.Klines) - candleCount
	if startIdx < 0 {
		startIdx = 0
	}
	for i := startIdx; i < len(data.Klines); i++ {
		k := data.Klines[i]
		change := ((k.Close - k.Open) / k.Open) * 100
		candle := "GREEN"
		if k.Close < k.Open {
			candle = "RED"
		}
		// Calculate wick percentage (rejection indicator)
		bodySize := k.Close - k.Open
		if bodySize < 0 {
			bodySize = -bodySize
		}
		totalRange := k.High - k.Low
		wickPct := 0.0
		if totalRange > 0 {
			wickPct = ((totalRange - bodySize) / totalRange) * 100
		}
		wickWarning := ""
		if enableHighWickWarning && wickPct > 60 {
			wickWarning = " [HIGH WICK ⚠️]"
		}

		// Add candle number for easier reference
		candleNum := i - startIdx + 1
		sb.WriteString(fmt.Sprintf("  #%02d O:%.2f H:%.2f L:%.2f C:%.2f [%s %.2f%%]%s\n",
			candleNum, k.Open, k.High, k.Low, k.Close, candle, change, wickWarning))
	}

	// Append Data Dictionary (metrics explanation)
	sb.WriteString(decision.GetSchemaPrompt(decision.LangEnglish))

	return sb.String()
}

// calculateEMA calculates Exponential Moving Average
func calculateEMA(data []float64, period int) float64 {
	if len(data) < period {
		return 0
	}

	multiplier := 2.0 / float64(period+1)

	// Start with SMA for first EMA value
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += data[i]
	}
	ema := sum / float64(period)

	// Calculate EMA for remaining values
	for i := period; i < len(data); i++ {
		ema = (data[i]-ema)*multiplier + ema
	}

	return ema
}

// calculateRSI calculates Relative Strength Index
func calculateRSI(data []float64, period int) float64 {
	if len(data) < period+1 {
		return 50
	}

	gains := 0.0
	losses := 0.0

	for i := 1; i <= period; i++ {
		change := data[i] - data[i-1]
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	for i := period + 1; i < len(data); i++ {
		change := data[i] - data[i-1]
		if change > 0 {
			avgGain = (avgGain*float64(period-1) + change) / float64(period)
			avgLoss = (avgLoss * float64(period-1)) / float64(period)
		} else {
			avgGain = (avgGain * float64(period-1)) / float64(period)
			avgLoss = (avgLoss*float64(period-1) - change) / float64(period)
		}
	}

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

// calculateMACD calculates MACD, Signal, and Histogram
func calculateMACD(data []float64) (macd, signal, histogram float64) {
	ema12 := calculateEMA(data, 12)
	ema26 := calculateEMA(data, 26)
	macd = ema12 - ema26

	// For signal line, we need MACD values over time
	// Simplified: use current MACD approximation
	signal = macd * 0.9 // Approximation
	histogram = macd - signal

	return
}

// calculateATR calculates Average True Range
func calculateATR(highs, lows, closes []float64, period int) float64 {
	if len(highs) < period+1 {
		return 0
	}

	trSum := 0.0
	for i := 1; i <= period; i++ {
		tr := math.Max(
			highs[i]-lows[i],
			math.Max(
				math.Abs(highs[i]-closes[i-1]),
				math.Abs(lows[i]-closes[i-1]),
			),
		)
		trSum += tr
	}

	return trSum / float64(period)
}

// calculateMoveMaturity calculates how many candles since the last EMA crossover
// and classifies the move maturity as EARLY, MID, LATE, or EXHAUSTED
// This helps identify if we're entering early in a trend or chasing a late move
// Reference: https://www.stockforecasttoday.com/post/swing-trading-etfs-with-cycle-timing-how-to-avoid-late-entries-near-market-tops
//
// IMPORTANT: The original research is based on DAILY stock charts where "3-7 sessions" = 3-7 trading days.
// For lower timeframes (5m, 15m, 1h), we scale the thresholds accordingly:
// - Daily = base (1.0x)
// - 4h = 1.5x
// - 1h = 2.0x
// - 30m = 2.5x
// - 15m = 3.0x
// - 5m = 4.0x (21 * 4 = 84 candles ≈ 7 hours before EXHAUSTED)
// - 1m = 6.0x
func calculateMoveMaturity(closes []float64, shortPeriod, longPeriod int, timeframe string) (candlesSinceCross int, maturity string, score int) {
	if len(closes) < longPeriod+10 {
		return 0, "UNKNOWN", 0
	}

	// Calculate EMA series for each candle to find crossover point
	// We need to walk through the data to find where EMA9 crossed EMA21
	shortMultiplier := 2.0 / float64(shortPeriod+1)
	longMultiplier := 2.0 / float64(longPeriod+1)

	// Initialize SMAs
	var shortSum, longSum float64
	for i := 0; i < shortPeriod; i++ {
		shortSum += closes[i]
	}
	shortEMA := shortSum / float64(shortPeriod)

	for i := 0; i < longPeriod; i++ {
		longSum += closes[i]
	}
	longEMA := longSum / float64(longPeriod)

	// Track the last crossover position
	lastCrossIndex := longPeriod // Start from where we have both EMAs
	prevShortAbove := shortEMA > longEMA

	// Walk through remaining candles to find most recent crossover
	for i := longPeriod; i < len(closes); i++ {
		// Update EMAs
		shortEMA = (closes[i]-shortEMA)*shortMultiplier + shortEMA
		longEMA = (closes[i]-longEMA)*longMultiplier + longEMA

		currentShortAbove := shortEMA > longEMA

		// Detect crossover
		if currentShortAbove != prevShortAbove {
			lastCrossIndex = i
			prevShortAbove = currentShortAbove
		}
	}

	// Calculate candles since last crossover
	candlesSinceCross = len(closes) - 1 - lastCrossIndex

	// Get timeframe multiplier - lower timeframes need higher thresholds
	// Base research: "Short-term cycle trades might last 3-7 sessions" on DAILY charts
	// Reference: https://www.stockforecasttoday.com/post/swing-trading-examples-using-cycle-timing-and-price-structure
	multiplier := getTimeframeMultiplier(timeframe)

	// Scaled thresholds (base: EARLY=7, MID=14, LATE=21)
	earlyThreshold := int(7 * multiplier)
	midThreshold := int(14 * multiplier)
	lateThreshold := int(21 * multiplier)

	// Classify maturity based on scaled thresholds
	// For 5m with 4x multiplier: EARLY=28, MID=56, LATE=84, EXHAUSTED>84
	switch {
	case candlesSinceCross <= earlyThreshold:
		maturity = "EARLY"
		score = 1
	case candlesSinceCross <= midThreshold:
		maturity = "MID"
		score = 2
	case candlesSinceCross <= lateThreshold:
		maturity = "LATE"
		score = 3
	default:
		maturity = "EXHAUSTED"
		score = 4
	}

	return candlesSinceCross, maturity, score
}

// getTimeframeMultiplier returns a multiplier for move maturity thresholds based on timeframe
// Lower timeframes need higher thresholds because more candles form in the same time period
// Base thresholds (7/14/21) are designed for daily charts
func getTimeframeMultiplier(timeframe string) float64 {
	switch timeframe {
	case "1m":
		return 6.0 // 21*6=126 candles ≈ 2 hours before EXHAUSTED
	case "3m":
		return 5.0 // 21*5=105 candles ≈ 5.25 hours before EXHAUSTED
	case "5m":
		return 4.0 // 21*4=84 candles ≈ 7 hours before EXHAUSTED
	case "15m":
		return 3.0 // 21*3=63 candles ≈ 15.75 hours before EXHAUSTED
	case "30m":
		return 2.5 // 21*2.5=52 candles ≈ 26 hours before EXHAUSTED
	case "1h":
		return 2.0 // 21*2=42 candles ≈ 42 hours before EXHAUSTED
	case "2h":
		return 1.5 // 21*1.5=31 candles ≈ 62 hours before EXHAUSTED
	case "4h":
		return 1.2 // 21*1.2=25 candles ≈ 100 hours before EXHAUSTED
	case "1d", "1D":
		return 1.0 // Base: 21 candles = 21 days (as per research)
	default:
		// Default to conservative multiplier for unknown timeframes
		return 4.0
	}
}

// ========== Entry Timing Indicators (Task 3.1) ==========

// calculateDistanceFromEMA calculates percentage distance from an EMA
// Positive = price above EMA, Negative = price below EMA
func calculateDistanceFromEMA(currentPrice, ema float64) float64 {
	if ema <= 0 {
		return 0
	}
	return ((currentPrice - ema) / ema) * 100
}

// calculateRSIRateOfChange calculates how RSI is changing over recent candles
// Returns the average change per candle (positive = RSI rising, negative = falling)
// Reference: Used to detect RSI "hook" - when RSI turns direction
func calculateRSIRateOfChange(closes []float64, period int, lookback int) float64 {
	if len(closes) < period+lookback+1 {
		return 0
	}

	// Calculate RSI for the last 'lookback' candles
	rsiValues := make([]float64, lookback+1)
	for i := 0; i <= lookback; i++ {
		endIdx := len(closes) - lookback + i
		if endIdx < period+1 {
			continue
		}
		rsiValues[i] = calculateRSIAtIndex(closes[:endIdx], period)
	}

	// Calculate rate of change
	if len(rsiValues) < 2 {
		return 0
	}
	return (rsiValues[len(rsiValues)-1] - rsiValues[0]) / float64(lookback)
}

// calculateRSIAtIndex calculates RSI for data up to a specific index
func calculateRSIAtIndex(data []float64, period int) float64 {
	if len(data) < period+1 {
		return 50
	}

	gains := 0.0
	losses := 0.0

	for i := len(data) - period; i < len(data); i++ {
		change := data[i] - data[i-1]
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

// calculateCandlesSinceRSIExtreme counts candles since RSI was > 70 or < 30
// Returns the count and whether the extreme was overbought (true) or oversold (false)
func calculateCandlesSinceRSIExtreme(closes []float64, period int, overbought, oversold float64) int {
	if len(closes) < period+1 {
		return 0
	}

	// Walk backwards through data checking RSI at each point
	for i := 0; i < len(closes)-period-1; i++ {
		endIdx := len(closes) - i
		if endIdx < period+1 {
			break
		}
		rsi := calculateRSIAtIndex(closes[:endIdx], period)
		if rsi >= overbought || rsi <= oversold {
			return i
		}
	}

	return len(closes) - period - 1 // Never hit extreme in available data
}

// calculatePullbackDepth calculates the current pullback depth from recent swing high/low
// For uptrend: measures drop from recent high
// For downtrend: measures bounce from recent low
// Reference: https://capital.com/en-int/learn/trading-strategies/pullback-trading
func calculatePullbackDepth(highs, lows, closes []float64, lookback int, isUptrend bool) float64 {
	if len(highs) < lookback || len(lows) < lookback || len(closes) < 1 {
		return 0
	}

	currentPrice := closes[len(closes)-1]
	startIdx := len(highs) - lookback

	if isUptrend {
		// Find recent high
		recentHigh := highs[startIdx]
		for i := startIdx; i < len(highs); i++ {
			if highs[i] > recentHigh {
				recentHigh = highs[i]
			}
		}
		// Pullback depth = how far we've dropped from the high
		if recentHigh > 0 {
			return ((recentHigh - currentPrice) / recentHigh) * 100
		}
	} else {
		// Find recent low
		recentLow := lows[startIdx]
		for i := startIdx; i < len(lows); i++ {
			if lows[i] < recentLow {
				recentLow = lows[i]
			}
		}
		// Bounce depth = how far we've risen from the low
		if recentLow > 0 {
			return ((currentPrice - recentLow) / recentLow) * 100
		}
	}

	return 0
}

// detectMomentumDecay detects divergence where price rises but MACD histogram declines
// This is a warning sign of weakening momentum
// Returns: isDecaying, numberOfBarsDecaying
func detectMomentumDecay(closes []float64, lookback int) (bool, int) {
	if len(closes) < 26+lookback {
		return false, 0
	}

	decayBars := 0
	isDecaying := false

	// Calculate MACD histogram for recent candles
	for i := 0; i < lookback-1; i++ {
		endIdx := len(closes) - lookback + i + 1
		prevEndIdx := endIdx - 1

		if endIdx < 26 || prevEndIdx < 26 {
			continue
		}

		_, _, currentHist := calculateMACD(closes[:endIdx])
		_, _, prevHist := calculateMACD(closes[:prevEndIdx])

		priceRising := closes[endIdx-1] > closes[prevEndIdx-1]
		histDeclining := currentHist < prevHist

		if priceRising && histDeclining {
			decayBars++
		}
	}

	// Consider decay significant if 3+ bars show divergence
	if decayBars >= 3 {
		isDecaying = true
	}

	return isDecaying, decayBars
}

// ========== Market Regime Detection (Task 4.1) ==========

// calculateADX calculates the Average Directional Index
// ADX > 25 = trending market, ADX < 20 = ranging market
// Reference: https://www.investopedia.com/terms/a/adx.asp
func calculateADX(highs, lows, closes []float64, period int) (adx float64, trend string) {
	if len(highs) < period*2 || len(lows) < period*2 || len(closes) < period*2 {
		return 0, "UNKNOWN"
	}

	// Calculate +DM, -DM, and TR for each period
	plusDM := make([]float64, len(highs)-1)
	minusDM := make([]float64, len(highs)-1)
	tr := make([]float64, len(highs)-1)

	for i := 1; i < len(highs); i++ {
		highDiff := highs[i] - highs[i-1]
		lowDiff := lows[i-1] - lows[i]

		if highDiff > lowDiff && highDiff > 0 {
			plusDM[i-1] = highDiff
		} else {
			plusDM[i-1] = 0
		}

		if lowDiff > highDiff && lowDiff > 0 {
			minusDM[i-1] = lowDiff
		} else {
			minusDM[i-1] = 0
		}

		tr[i-1] = math.Max(
			highs[i]-lows[i],
			math.Max(
				math.Abs(highs[i]-closes[i-1]),
				math.Abs(lows[i]-closes[i-1]),
			),
		)
	}

	// Calculate smoothed averages
	smoothedPlusDM := wilder(plusDM, period)
	smoothedMinusDM := wilder(minusDM, period)
	smoothedTR := wilder(tr, period)

	if len(smoothedPlusDM) == 0 || len(smoothedTR) == 0 {
		return 0, "UNKNOWN"
	}

	// Calculate +DI and -DI
	plusDI := make([]float64, len(smoothedPlusDM))
	minusDI := make([]float64, len(smoothedMinusDM))
	dx := make([]float64, len(smoothedPlusDM))

	for i := range smoothedPlusDM {
		if smoothedTR[i] > 0 {
			plusDI[i] = (smoothedPlusDM[i] / smoothedTR[i]) * 100
			minusDI[i] = (smoothedMinusDM[i] / smoothedTR[i]) * 100
		}

		diSum := plusDI[i] + minusDI[i]
		if diSum > 0 {
			dx[i] = (math.Abs(plusDI[i]-minusDI[i]) / diSum) * 100
		}
	}

	// Calculate ADX as smoothed DX
	adxValues := wilder(dx, period)
	if len(adxValues) > 0 {
		adx = adxValues[len(adxValues)-1]
	}

	// Classify trend strength
	switch {
	case adx >= 25:
		trend = "STRONG"
	case adx >= 20:
		trend = "MODERATE"
	default:
		trend = "WEAK"
	}

	return adx, trend
}

// wilder calculates Wilder's smoothing (used in ADX calculation)
func wilder(data []float64, period int) []float64 {
	if len(data) < period {
		return nil
	}

	result := make([]float64, len(data)-period+1)

	// First value is simple average
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += data[i]
	}
	result[0] = sum / float64(period)

	// Subsequent values use Wilder's smoothing
	for i := period; i < len(data); i++ {
		result[i-period+1] = result[i-period] - (result[i-period] / float64(period)) + data[i]
	}

	return result
}

// calculateBollingerBands calculates Bollinger Bands and detects squeeze
// Squeeze = bands narrowing, indicating low volatility and potential breakout
// Reference: Bollinger squeeze precedes significant moves
func calculateBollingerBands(closes []float64, period int, stdDevMult float64) (width float64, isSqueeze bool) {
	if len(closes) < period*2 {
		return 0, false
	}

	// Current Bollinger width
	currentWidth := bollingerWidth(closes[len(closes)-period:], stdDevMult)

	// Historical average width (for comparison)
	var avgWidth float64
	count := 0
	for i := period; i < len(closes)-period; i++ {
		w := bollingerWidth(closes[i:i+period], stdDevMult)
		avgWidth += w
		count++
	}
	if count > 0 {
		avgWidth /= float64(count)
	}

	// Squeeze if current width is less than 50% of average
	isSqueeze = avgWidth > 0 && currentWidth < avgWidth*0.5

	return currentWidth, isSqueeze
}

// bollingerWidth calculates the Bollinger Band width as percentage of middle band
func bollingerWidth(data []float64, stdDevMult float64) float64 {
	if len(data) == 0 {
		return 0
	}

	// Calculate SMA
	sum := 0.0
	for _, v := range data {
		sum += v
	}
	sma := sum / float64(len(data))

	// Calculate standard deviation
	variance := 0.0
	for _, v := range data {
		diff := v - sma
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(len(data)))

	// Width as percentage
	if sma > 0 {
		return (stdDev * stdDevMult * 2 / sma) * 100
	}
	return 0
}

// calculateATRRatio calculates current ATR vs historical average
// Ratio > 2.0 indicates high volatility
func calculateATRRatio(highs, lows, closes []float64, period int) float64 {
	if len(highs) < period*3 {
		return 1.0
	}

	// Current ATR
	currentATR := calculateATR(highs[len(highs)-period-1:], lows[len(lows)-period-1:], closes[len(closes)-period-1:], period)

	// Historical average ATR
	var avgATR float64
	count := 0
	for i := period + 1; i < len(highs)-period; i++ {
		atr := calculateATR(highs[i-period-1:i], lows[i-period-1:i], closes[i-period-1:i], period)
		avgATR += atr
		count++
	}
	if count > 0 {
		avgATR /= float64(count)
	}

	if avgATR > 0 {
		return currentATR / avgATR
	}
	return 1.0
}

// detectMarketRegime determines the current market regime
// Returns: regime type, description
// Reference: https://arxiv.org/html/2510.15949v2 (ATLAS Framework)
func detectMarketRegime(adx float64, adxTrend string, bollingerSqueeze bool, atrRatio float64, momentumDecay bool) (regime, description string) {
	switch {
	case momentumDecay && adx >= 20:
		regime = "EXHAUSTED"
		description = "Trend showing signs of exhaustion - momentum divergence detected"
	case atrRatio >= 2.0:
		regime = "VOLATILE"
		description = fmt.Sprintf("High volatility environment (ATR %.1fx average) - reduce position size", atrRatio)
	case adx >= 25:
		regime = "TRENDING"
		description = fmt.Sprintf("Strong trend in place (ADX %.1f) - momentum strategies favored", adx)
	case adx < 20 || bollingerSqueeze:
		regime = "RANGING"
		if bollingerSqueeze {
			description = "Low volatility squeeze - potential breakout brewing"
		} else {
			description = fmt.Sprintf("Ranging/choppy market (ADX %.1f) - mean reversion or wait", adx)
		}
	default:
		regime = "TRANSITIONING"
		description = "Market in transition - wait for clearer signal"
	}

	return regime, description
}

// ========== Fibonacci Retracement Detection (Task 3.2) ==========

// FibonacciLevels holds the calculated Fibonacci retracement levels and analysis
type FibonacciLevels struct {
	SwingHigh    float64
	SwingLow     float64
	Level382     float64 // 38.2% retracement
	Level500     float64 // 50% retracement
	Level618     float64 // 61.8% retracement
	NearestLevel string  // "38.2", "50", "61.8", "NONE"
	DistancePct  float64 // Distance from nearest level
	AtSupport    bool    // True if near a Fib level in uptrend (support)
	AtResistance bool    // True if near a Fib level in downtrend (resistance)
}

// calculateFibonacciLevels calculates Fibonacci retracement levels from recent swing high/low
// Reference: https://www.investopedia.com/terms/f/fibonacciretracement.asp
// "The most commonly used Fibonacci retracement levels are 38.2%, 50%, and 61.8%"
func calculateFibonacciLevels(highs, lows []float64, currentPrice float64, lookback int, isUptrend bool) FibonacciLevels {
	result := FibonacciLevels{
		NearestLevel: "NONE",
	}

	if len(highs) < lookback || len(lows) < lookback {
		return result
	}

	startIdx := len(highs) - lookback

	// Find swing high and low in the lookback period
	swingHigh := highs[startIdx]
	swingLow := lows[startIdx]
	for i := startIdx; i < len(highs); i++ {
		if highs[i] > swingHigh {
			swingHigh = highs[i]
		}
		if lows[i] < swingLow {
			swingLow = lows[i]
		}
	}

	result.SwingHigh = swingHigh
	result.SwingLow = swingLow

	// Calculate range
	priceRange := swingHigh - swingLow
	if priceRange <= 0 {
		return result
	}

	// Calculate Fibonacci levels
	// In an uptrend, retracements are measured from swing high down
	// In a downtrend, retracements are measured from swing low up
	if isUptrend {
		// Uptrend: measure pullback from high
		result.Level382 = swingHigh - (priceRange * 0.382)
		result.Level500 = swingHigh - (priceRange * 0.500)
		result.Level618 = swingHigh - (priceRange * 0.618)
	} else {
		// Downtrend: measure bounce from low
		result.Level382 = swingLow + (priceRange * 0.382)
		result.Level500 = swingLow + (priceRange * 0.500)
		result.Level618 = swingLow + (priceRange * 0.618)
	}

	// Find nearest Fib level
	tolerance := priceRange * 0.02 // 2% tolerance

	dist382 := math.Abs(currentPrice - result.Level382)
	dist500 := math.Abs(currentPrice - result.Level500)
	dist618 := math.Abs(currentPrice - result.Level618)

	minDist := dist382
	result.NearestLevel = "38.2"
	if dist500 < minDist {
		minDist = dist500
		result.NearestLevel = "50"
	}
	if dist618 < minDist {
		minDist = dist618
		result.NearestLevel = "61.8"
	}

	result.DistancePct = (minDist / currentPrice) * 100

	// Check if at Fib support/resistance (within tolerance)
	if minDist <= tolerance {
		if isUptrend {
			result.AtSupport = true
		} else {
			result.AtResistance = true
		}
	} else {
		result.NearestLevel = "NONE"
	}

	return result
}

// ========== Volume Profile on Pullback (Task 3.3) ==========

// VolumeProfile holds volume analysis during pullbacks
type VolumeProfile struct {
	PullbackVolumeRatio float64 // Pullback volume / Trend volume
	Signal              string  // HEALTHY, UNHEALTHY, NEUTRAL
	Confirmation        bool    // True if volume confirms entry
}

// analyzeVolumeProfile analyzes volume patterns during pullback
// Reference: https://capital.com/en-int/learn/trading-strategies/pullback-trading
// "Volume often drops and momentum slows down" during healthy pullbacks
func analyzeVolumeProfile(volumes []float64, closes []float64, lookback int, isUptrend bool) VolumeProfile {
	result := VolumeProfile{
		PullbackVolumeRatio: 1.0,
		Signal:              "NEUTRAL",
		Confirmation:        false,
	}

	if len(volumes) < lookback || len(closes) < lookback {
		return result
	}

	startIdx := len(volumes) - lookback

	// Identify trend and pullback phases
	// Find the peak/trough and calculate volumes before and after
	var trendVolume, pullbackVolume float64
	var trendBars, pullbackBars int

	if isUptrend {
		// Find the high point
		peakIdx := startIdx
		for i := startIdx; i < len(closes); i++ {
			if closes[i] > closes[peakIdx] {
				peakIdx = i
			}
		}

		// Volume before peak (trend phase)
		for i := startIdx; i < peakIdx && i < len(volumes); i++ {
			trendVolume += volumes[i]
			trendBars++
		}

		// Volume after peak (pullback phase)
		for i := peakIdx; i < len(volumes); i++ {
			pullbackVolume += volumes[i]
			pullbackBars++
		}
	} else {
		// Find the low point
		troughIdx := startIdx
		for i := startIdx; i < len(closes); i++ {
			if closes[i] < closes[troughIdx] {
				troughIdx = i
			}
		}

		// Volume before trough (trend phase)
		for i := startIdx; i < troughIdx && i < len(volumes); i++ {
			trendVolume += volumes[i]
			trendBars++
		}

		// Volume after trough (bounce phase)
		for i := troughIdx; i < len(volumes); i++ {
			pullbackVolume += volumes[i]
			pullbackBars++
		}
	}

	// Calculate average volumes
	avgTrendVol := 0.0
	avgPullbackVol := 0.0
	if trendBars > 0 {
		avgTrendVol = trendVolume / float64(trendBars)
	}
	if pullbackBars > 0 {
		avgPullbackVol = pullbackVolume / float64(pullbackBars)
	}

	// Calculate ratio
	if avgTrendVol > 0 {
		result.PullbackVolumeRatio = avgPullbackVol / avgTrendVol
	}

	// Determine signal
	// < 0.7: Volume dropping significantly during pullback - HEALTHY
	// 0.7-1.3: Normal volume - NEUTRAL
	// > 1.3: Volume increasing during pullback - UNHEALTHY (may continue)
	switch {
	case result.PullbackVolumeRatio < 0.7:
		result.Signal = "HEALTHY"
		result.Confirmation = true
	case result.PullbackVolumeRatio > 1.3:
		result.Signal = "UNHEALTHY"
		result.Confirmation = false
	default:
		result.Signal = "NEUTRAL"
		result.Confirmation = false
	}

	return result
}
