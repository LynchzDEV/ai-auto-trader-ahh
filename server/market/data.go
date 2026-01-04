package market

import (
	"context"
	"fmt"
	"math"
	"strings"

	"auto-trader-ahh/exchange"
)

type MarketData struct {
	Symbol        string
	CurrentPrice  float64
	Klines        []exchange.Kline
	EMA9          float64
	EMA21         float64
	RSI           float64
	MACD          float64
	MACDSignal    float64
	MACDHist      float64
	ATR           float64
	Volume24h     float64
	PriceChange24h float64
	Trend         string // BULLISH, BEARISH, NEUTRAL
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

	return &MarketData{
		Symbol:         symbol,
		CurrentPrice:   ticker.Price,
		Klines:         klines,
		EMA9:           ema9,
		EMA21:          ema21,
		RSI:            rsi,
		MACD:           macd,
		MACDSignal:     signal,
		MACDHist:       hist,
		ATR:            atr,
		Volume24h:      volume24h,
		PriceChange24h: priceChange24h,
		Trend:          trend,
	}, nil
}

// FormatForAI formats market data as a string for AI analysis
func (d *DataProvider) FormatForAI(data *MarketData) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("=== %s Market Analysis ===\n\n", data.Symbol))
	sb.WriteString(fmt.Sprintf("Current Price: $%.2f\n", data.CurrentPrice))
	sb.WriteString(fmt.Sprintf("24h Price Change: %.2f%%\n", data.PriceChange24h))
	sb.WriteString(fmt.Sprintf("24h Volume: $%.2f\n\n", data.Volume24h))

	sb.WriteString("--- Technical Indicators ---\n")
	sb.WriteString(fmt.Sprintf("EMA 9: $%.2f\n", data.EMA9))
	sb.WriteString(fmt.Sprintf("EMA 21: $%.2f\n", data.EMA21))
	sb.WriteString(fmt.Sprintf("EMA Trend: %s\n", map[bool]string{true: "BULLISH (EMA9 > EMA21)", false: "BEARISH (EMA9 < EMA21)"}[data.EMA9 > data.EMA21]))
	sb.WriteString(fmt.Sprintf("RSI (14): %.2f", data.RSI))
	if data.RSI > 70 {
		sb.WriteString(" [OVERBOUGHT]\n")
	} else if data.RSI < 30 {
		sb.WriteString(" [OVERSOLD]\n")
	} else {
		sb.WriteString(" [NEUTRAL]\n")
	}
	sb.WriteString(fmt.Sprintf("MACD: %.4f\n", data.MACD))
	sb.WriteString(fmt.Sprintf("MACD Signal: %.4f\n", data.MACDSignal))
	sb.WriteString(fmt.Sprintf("MACD Histogram: %.4f", data.MACDHist))
	if data.MACDHist > 0 {
		sb.WriteString(" [BULLISH]\n")
	} else {
		sb.WriteString(" [BEARISH]\n")
	}
	sb.WriteString(fmt.Sprintf("ATR (14): %.4f (Volatility: %.2f%%)\n\n", data.ATR, (data.ATR/data.CurrentPrice)*100))

	sb.WriteString(fmt.Sprintf("--- Overall Trend: %s ---\n\n", data.Trend))

	// Recent price action (all candles from kline_count setting)
	sb.WriteString(fmt.Sprintf("--- Recent Price Action (Last %d Candles) ---\n", len(data.Klines)))
	for i := 0; i < len(data.Klines); i++ {
		k := data.Klines[i]
		change := ((k.Close - k.Open) / k.Open) * 100
		candle := "GREEN"
		if k.Close < k.Open {
			candle = "RED"
		}
		sb.WriteString(fmt.Sprintf("  O:%.2f H:%.2f L:%.2f C:%.2f [%s %.2f%%]\n",
			k.Open, k.High, k.Low, k.Close, candle, change))
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
