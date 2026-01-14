package intel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	tradingViewScanURL = "https://scanner.tradingview.com/crypto/scan"
)

// TradingView indicator columns to fetch
var tvIndicators = []string{
	"Recommend.All",    // Overall recommendation (-1 to 1)
	"Recommend.MA",     // Moving averages recommendation
	"Recommend.Other",  // Oscillators recommendation
	"RSI",
	"MACD.macd",
	"MACD.signal",
	"Stoch.K",
	"Stoch.D",
	"ADX",
	"CCI20",
	"AO",
	"Mom",
	"BB.upper",
	"BB.lower",
	"close",
	"volume",
	"change",
}

// TradingViewData contains technical analysis from TradingView
type TradingViewData struct {
	Symbol           string  `json:"symbol"`
	RecommendAll     float64 `json:"recommend_all"`     // -1 (strong sell) to 1 (strong buy)
	RecommendMA      float64 `json:"recommend_ma"`      // Moving averages recommendation
	RecommendOther   float64 `json:"recommend_other"`   // Oscillators recommendation
	RSI              float64 `json:"rsi"`
	MACDValue        float64 `json:"macd_value"`
	MACDSignal       float64 `json:"macd_signal"`
	StochK           float64 `json:"stoch_k"`
	StochD           float64 `json:"stoch_d"`
	ADX              float64 `json:"adx"`
	CCI              float64 `json:"cci"`
	AO               float64 `json:"ao"`
	Momentum         float64 `json:"momentum"`
	BBUpper          float64 `json:"bb_upper"`
	BBLower          float64 `json:"bb_lower"`
	Close            float64 `json:"close"`
	Volume           float64 `json:"volume"`
	Change           float64 `json:"change"`
	Summary          string  `json:"summary"`           // BUY, SELL, NEUTRAL, STRONG_BUY, STRONG_SELL
	MASignal         string  `json:"ma_signal"`
	OscillatorSignal string  `json:"oscillator_signal"`
}

// tvScanRequest represents the request payload
type tvScanRequest struct {
	Symbols struct {
		Tickers []string `json:"tickers"`
		Query   struct {
			Types []string `json:"types"`
		} `json:"query"`
	} `json:"symbols"`
	Columns []string `json:"columns"`
}

// tvScanResponse represents the response from TradingView
type tvScanResponse struct {
	Data []struct {
		Symbol string    `json:"s"`
		Data   []float64 `json:"d"`
	} `json:"data"`
}

// FetchTradingViewData fetches technical analysis from TradingView
// interval: "1h", "4h", "1d" (default)
func FetchTradingViewData(ctx context.Context, symbols []string, interval string) (map[string]*TradingViewData, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	// Convert symbols to TradingView format (BINANCE:BTCUSDT)
	tickers := make([]string, 0, len(symbols))
	for _, s := range symbols {
		// Assume Binance for crypto pairs
		ticker := "BINANCE:" + strings.ToUpper(s)
		tickers = append(tickers, ticker)
	}

	// Build interval suffix
	intervalSuffix := ""
	switch interval {
	case "1h":
		intervalSuffix = "|60"
	case "4h":
		intervalSuffix = "|240"
	case "1d", "":
		intervalSuffix = ""
	default:
		intervalSuffix = ""
	}

	// Build columns with interval suffix
	columns := make([]string, len(tvIndicators))
	for i, ind := range tvIndicators {
		columns[i] = ind + intervalSuffix
	}

	// Build request
	reqData := tvScanRequest{}
	reqData.Symbols.Tickers = tickers
	reqData.Symbols.Query.Types = []string{}
	reqData.Columns = columns

	jsonData, err := json.Marshal(reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tradingViewScanURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tradingview_ta/3.3.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch TradingView data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TradingView API returned status %d", resp.StatusCode)
	}

	var result tvScanResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Parse response
	dataMap := make(map[string]*TradingViewData)
	for _, item := range result.Data {
		if len(item.Data) < len(tvIndicators) {
			continue
		}

		// Extract symbol (remove BINANCE: prefix)
		symbol := strings.TrimPrefix(item.Symbol, "BINANCE:")

		data := &TradingViewData{
			Symbol:         symbol,
			RecommendAll:   item.Data[0],
			RecommendMA:    item.Data[1],
			RecommendOther: item.Data[2],
			RSI:            item.Data[3],
			MACDValue:      item.Data[4],
			MACDSignal:     item.Data[5],
			StochK:         item.Data[6],
			StochD:         item.Data[7],
			ADX:            item.Data[8],
			CCI:            item.Data[9],
			AO:             item.Data[10],
			Momentum:       item.Data[11],
			BBUpper:        item.Data[12],
			BBLower:        item.Data[13],
			Close:          item.Data[14],
			Volume:         item.Data[15],
			Change:         item.Data[16],
		}

		// Compute summary signals
		data.Summary = computeSignal(data.RecommendAll)
		data.MASignal = computeSignal(data.RecommendMA)
		data.OscillatorSignal = computeSignal(data.RecommendOther)

		dataMap[symbol] = data
	}

	return dataMap, nil
}

// computeSignal converts recommendation value to signal string
func computeSignal(value float64) string {
	if value >= 0.5 {
		return "STRONG_BUY"
	} else if value >= 0.1 {
		return "BUY"
	} else if value <= -0.5 {
		return "STRONG_SELL"
	} else if value <= -0.1 {
		return "SELL"
	}
	return "NEUTRAL"
}

// FormatTradingViewData formats TradingView data for AI consumption
func FormatTradingViewData(data map[string]*TradingViewData, symbols []string) string {
	if len(data) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## TradingView Technical Ratings\n\n")

	for _, symbol := range symbols {
		tv, ok := data[strings.ToUpper(symbol)]
		if !ok {
			continue
		}

		// Summary signal with emoji
		summaryEmoji := "⚖️"
		switch tv.Summary {
		case "STRONG_BUY":
			summaryEmoji = "🟢🟢"
		case "BUY":
			summaryEmoji = "🟢"
		case "STRONG_SELL":
			summaryEmoji = "🔴🔴"
		case "SELL":
			summaryEmoji = "🔴"
		}

		sb.WriteString(fmt.Sprintf("### %s\n", tv.Symbol))
		sb.WriteString(fmt.Sprintf("- Overall: %s %s (%.2f)\n", summaryEmoji, tv.Summary, tv.RecommendAll))
		sb.WriteString(fmt.Sprintf("- Moving Averages: %s (%.2f)\n", tv.MASignal, tv.RecommendMA))
		sb.WriteString(fmt.Sprintf("- Oscillators: %s (%.2f)\n", tv.OscillatorSignal, tv.RecommendOther))
		sb.WriteString(fmt.Sprintf("- RSI: %.1f", tv.RSI))
		if tv.RSI > 70 {
			sb.WriteString(" [OVERBOUGHT]")
		} else if tv.RSI < 30 {
			sb.WriteString(" [OVERSOLD]")
		}
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("- MACD: %.4f (Signal: %.4f)", tv.MACDValue, tv.MACDSignal))
		if tv.MACDValue > tv.MACDSignal {
			sb.WriteString(" [BULLISH]")
		} else {
			sb.WriteString(" [BEARISH]")
		}
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("- Stochastic: K=%.1f D=%.1f\n", tv.StochK, tv.StochD))
		sb.WriteString(fmt.Sprintf("- ADX: %.1f", tv.ADX))
		if tv.ADX > 25 {
			sb.WriteString(" [TRENDING]")
		} else {
			sb.WriteString(" [RANGING]")
		}
		sb.WriteString("\n\n")
	}

	return sb.String()
}
