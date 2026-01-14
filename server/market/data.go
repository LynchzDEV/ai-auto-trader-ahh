package market

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

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

	return &MarketData{
		Symbol:          symbol,
		CurrentPrice:    ticker.Price,
		Klines:          klines,
		EMA9:            ema9,
		EMA21:           ema21,
		RSI:             rsi,
		MACD:            macd,
		MACDSignal:      signal,
		MACDHist:        hist,
		ATR:             atr,
		Volume24h:       volume24h,
		PriceChange24h:  priceChange24h,
		Trend:           trend,
		FundingRate:     fundingRate,
		NextFundingTime: nextFundingTime,
	}, nil
}

// FormatForAI formats market data as a string for AI analysis
func (d *DataProvider) FormatForAI(data *MarketData) string {
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
		sb.WriteString(fmt.Sprintf("Recent High: $%.2f (current is %.1f%% below)\n", recentHigh, pctFromRecentHigh))
		sb.WriteString(fmt.Sprintf("Recent Low: $%.2f (current is %.1f%% above)\n", recentLow, pctFromRecentLow))
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
			sb.WriteString("🔴 HIGH POSITIVE FUNDING: LONG positions pay funding. Consider SHORT bias.\n")
		} else if fundingPct < -0.03 {
			sb.WriteString("🟢 HIGH NEGATIVE FUNDING: SHORT positions pay funding. Consider LONG bias.\n")
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
	if data.EMA9 > data.EMA21 {
		sb.WriteString(fmt.Sprintf("EMA Trend: BULLISH (EMA9 > EMA21 by %.2f%%)\n", emaSpread))
		if emaSpread > 0.8 {
			sb.WriteString("📈 Strong bullish trend. Good for LONG.\n")
		} else if emaSpread > 0.4 {
			sb.WriteString("📊 Moderate bullish trend. LONG possible with caution.\n")
		} else {
			sb.WriteString("🚫 VERY WEAK TREND (<0.4%). DO NOT OPEN NEW POSITIONS. Wait for stronger momentum.\n")
		}
	} else {
		sb.WriteString(fmt.Sprintf("EMA Trend: BEARISH (EMA9 < EMA21 by %.2f%%)\n", -emaSpread))
		if emaSpread < -0.8 {
			sb.WriteString("📉 Strong bearish trend. Good for SHORT.\n")
		} else if emaSpread < -0.4 {
			sb.WriteString("📊 Moderate bearish trend. SHORT possible with caution.\n")
		} else {
			sb.WriteString("🚫 VERY WEAK TREND (<0.4%). DO NOT OPEN NEW POSITIONS. Wait for stronger momentum.\n")
		}
	}

	// Add explicit trend strength gate (stricter for high volatility coins)
	if absEmaSpread < 0.4 {
		sb.WriteString(fmt.Sprintf("\n⛔ TREND STRENGTH GATE: EMA spread is only %.2f%% - TOO WEAK for new entries!\n", absEmaSpread))
		sb.WriteString("   Action: WAIT or HOLD existing positions. Do not open new trades.\n")
		sb.WriteString("   High volatility coins require at least 0.4%% EMA spread for reliable trends.\n\n")
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

	// Entry quality summary
	sb.WriteString("--- ENTRY QUALITY CHECK ---\n")
	longScore := 0
	shortScore := 0

	if data.EMA9 > data.EMA21 {
		longScore++
	} else {
		shortScore++
	}
	if data.RSI > 45 && data.RSI < 65 {
		longScore++
	}
	if data.RSI > 35 && data.RSI < 55 {
		shortScore++
	}
	if data.MACDHist > 0 {
		longScore++
	} else {
		shortScore++
	}
	if data.BTCChange24h > 0 {
		longScore++
	} else {
		shortScore++
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
		if wickPct > 60 {
			wickWarning = " [HIGH WICK ⚠️]"
		}

		// Add candle number for easier reference
		candleNum := i - startIdx + 1
		sb.WriteString(fmt.Sprintf("  #%02d O:%.2f H:%.2f L:%.2f C:%.2f [%s %.2f%%]%s\n",
			candleNum, k.Open, k.High, k.Low, k.Close, candle, change, wickWarning))
	}

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
