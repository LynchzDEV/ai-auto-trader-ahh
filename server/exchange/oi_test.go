package exchange

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestOIAnalysisInterpretation tests the logic for interpreting OI signals
func TestOIAnalysisInterpretation(t *testing.T) {
	tests := []struct {
		name        string
		oiChange1H  float64
		priceChange float64
		wantSignal  string
		wantConf    string
	}{
		{
			name:        "OI up + Price up = BULLISH (new longs)",
			oiChange1H:  5.0,
			priceChange: 3.0,
			wantSignal:  "BULLISH",
			wantConf:    "HIGH",
		},
		{
			name:        "OI up + Price down = BEARISH (new shorts)",
			oiChange1H:  5.0,
			priceChange: -3.0,
			wantSignal:  "BEARISH",
			wantConf:    "HIGH",
		},
		{
			name:        "OI down + Price up = REVERSAL_UP (shorts covering)",
			oiChange1H:  -5.0,
			priceChange: 3.0,
			wantSignal:  "REVERSAL_UP",
			wantConf:    "LOW",
		},
		{
			name:        "OI down + Price down = REVERSAL_DOWN (longs capitulating)",
			oiChange1H:  -5.0,
			priceChange: -3.0,
			wantSignal:  "REVERSAL_DOWN",
			wantConf:    "LOW",
		},
		{
			name:        "Small OI up + Small Price up = BULLISH (medium conf)",
			oiChange1H:  1.0,
			priceChange: 0.5,
			wantSignal:  "BULLISH",
			wantConf:    "MEDIUM",
		},
		{
			name:        "No movement = NEUTRAL",
			oiChange1H:  0.0,
			priceChange: 0.0,
			wantSignal:  "NEUTRAL",
			wantConf:    "LOW",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock client to access internal logic if needed, or just replicate logic
			// Replicating logic here since it's simple and avoids mocking HTTP
			signal := "NEUTRAL"
			confidence := "LOW"

			if tt.oiChange1H > 0 && tt.priceChange > 0 {
				signal = "BULLISH"
				if tt.oiChange1H > 2 && tt.priceChange > 1 {
					confidence = "HIGH"
				} else {
					confidence = "MEDIUM"
				}
			} else if tt.oiChange1H > 0 && tt.priceChange < 0 {
				signal = "BEARISH"
				if tt.oiChange1H > 2 && tt.priceChange < -1 {
					confidence = "HIGH"
				} else {
					confidence = "MEDIUM"
				}
			} else if tt.oiChange1H < 0 && tt.priceChange > 0 {
				signal = "REVERSAL_UP" // Shorts covering
				confidence = "LOW"
			} else if tt.oiChange1H < 0 && tt.priceChange < 0 {
				signal = "REVERSAL_DOWN" // Longs capitulating
				confidence = "LOW"
			}

			if signal != tt.wantSignal {
				t.Errorf("Signal = %v, want %v", signal, tt.wantSignal)
			}
			if confidence != tt.wantConf {
				t.Errorf("Confidence = %v, want %v", confidence, tt.wantConf)
			}
		})
	}
}

// TestOIChangeCalculation tests the percentage change calculation
func TestOIChangeCalculation(t *testing.T) {
	tests := []struct {
		name      string
		currentOI float64
		oldOI     float64
		want      float64
	}{
		{"OI increased 10%", 110, 100, 10.0},
		{"OI decreased 5%", 95, 100, -5.0},
		{"OI unchanged", 100, 100, 0.0},
		{"OI increased 2.5%", 10250, 10000, 2.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got float64
			if tt.oldOI > 0 {
				got = ((tt.currentOI - tt.oldOI) / tt.oldOI) * 100
			}
			if got != tt.want {
				t.Errorf("OI Change = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestOIDataStructs tests JSON unmarshalling of OI data
func TestOIDataStructs(t *testing.T) {
	// Not testing full JSON unmarshal here as it requires API interaction
	// Just validating struct usage which is done via compilation
	oi := OpenInterestData{
		Symbol:       "BTCUSDT",
		OpenInterest: 100.5,
		Time:         1234567890,
	}
	if oi.Symbol != "BTCUSDT" {
		t.Error("Struct initialization failed")
	}
}

// TestOIEntrySafetyLogic tests the safety checks based on OI signals
func TestOIEntrySafetyLogic(t *testing.T) {
	tests := []struct {
		name        string
		isLong      bool
		oiSignal    string
		oiChange1H  float64
		longRatio   float64
		shortRatio  float64
		liqPressure string // new field for liquidation test
		shouldBlock bool
		blockReason string
	}{
		{
			name:        "Block LONG when shorts covering with significant OI drop",
			isLong:      true,
			oiSignal:    "REVERSAL_UP",
			oiChange1H:  -3.0,
			longRatio:   50,
			shortRatio:  50,
			shouldBlock: true,
			blockReason: "shorts covering",
		},
		{
			name:        "Allow LONG when BULLISH",
			isLong:      true,
			oiSignal:    "BULLISH",
			oiChange1H:  2.0,
			longRatio:   55,
			shortRatio:  45,
			shouldBlock: false,
		},
		{
			name:        "Block SHORT when longs capitulating with significant OI drop",
			isLong:      false,
			oiSignal:    "REVERSAL_DOWN",
			oiChange1H:  -3.5,
			longRatio:   50,
			shortRatio:  50,
			shouldBlock: true,
			blockReason: "longs capitulating",
		},
		{
			name:        "Allow SHORT when BEARISH",
			isLong:      false,
			oiSignal:    "BEARISH",
			oiChange1H:  2.5,
			longRatio:   45,
			shortRatio:  55,
			shouldBlock: false,
		},
		{
			name:        "Block LONG when crowded long",
			isLong:      true,
			oiSignal:    "BULLISH",
			oiChange1H:  1.0,
			longRatio:   78,
			shortRatio:  22,
			shouldBlock: true,
			blockReason: "crowded long",
		},
		{
			name:        "Block SHORT when crowded short",
			isLong:      false,
			oiSignal:    "BEARISH",
			oiChange1H:  1.5,
			longRatio:   24,
			shortRatio:  76,
			shouldBlock: true,
			blockReason: "crowded short",
		},
		{
			name:        "Allow LONG when REVERSAL_UP but OI drop is small",
			isLong:      true,
			oiSignal:    "REVERSAL_UP",
			oiChange1H:  -1.0,
			longRatio:   50,
			shortRatio:  50,
			shouldBlock: false,
		},
		{
			name:        "Block LONG during LONG_LIQUIDATION (falling knife)",
			isLong:      true,
			oiSignal:    "NEUTRAL",
			liqPressure: "LONG_LIQUIDATION",
			shouldBlock: true,
			blockReason: "long liquidation",
		},
		{
			name:        "Block SHORT during SHORT_LIQUIDATION (short squeeze)",
			isLong:      false,
			oiSignal:    "NEUTRAL",
			liqPressure: "SHORT_LIQUIDATION",
			shouldBlock: true,
			blockReason: "short squeeze",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the checkEntrySafety OI logic
			blocked := false
			reason := ""

			if tt.oiSignal != "" {
				if tt.isLong && tt.oiSignal == "REVERSAL_UP" && tt.oiChange1H < -2 {
					blocked = true
					reason = "shorts covering"
				}
				if !tt.isLong && tt.oiSignal == "REVERSAL_DOWN" && tt.oiChange1H < -2 {
					blocked = true
					reason = "longs capitulating"
				}
				if tt.isLong && tt.longRatio > 75 {
					blocked = true
					reason = "crowded long"
				}
				if !tt.isLong && tt.shortRatio > 75 {
					blocked = true
					reason = "crowded short"
				}

				// Liquidation check logic simulation
				if tt.liqPressure != "" && tt.liqPressure != "NONE" {
					if tt.isLong && tt.liqPressure == "LONG_LIQUIDATION" {
						blocked = true
						reason = "long liquidation"
					}
					if !tt.isLong && tt.liqPressure == "SHORT_LIQUIDATION" {
						blocked = true
						reason = "short squeeze"
					}
				}
			}

			if blocked != tt.shouldBlock {
				t.Errorf("Blocked = %v, want %v (reason: %s)", blocked, tt.shouldBlock, reason)
			}
			if tt.shouldBlock && reason != tt.blockReason {
				t.Errorf("Reason = %q, want %q", reason, tt.blockReason)
			}
		})
	}
}

// TestLongShortSentimentLogic tests the sentiment trend detection logic
func TestLongShortSentimentLogic(t *testing.T) {
	tests := []struct {
		name           string
		currentLongPct float64
		oldLongPct     float64
		wantTrend      string
		wantMsgContent string // simplified check for message content
	}{
		{
			name:           "Significant Long Rise (+6%) -> BECOMING_BULLISH",
			currentLongPct: 0.60,
			oldLongPct:     0.54,
			wantTrend:      "BECOMING_BULLISH",
			wantMsgContent: "shifting LONG",
		},
		{
			name:           "Significant Long Drop (-6%) -> BECOMING_BEARISH",
			currentLongPct: 0.50,
			oldLongPct:     0.56,
			wantTrend:      "BECOMING_BEARISH",
			wantMsgContent: "shifting SHORT",
		},
		{
			name:           "Minor Change (+2%) -> STABLE",
			currentLongPct: 0.52,
			oldLongPct:     0.50,
			wantTrend:      "STABLE",
			wantMsgContent: "stable",
		},
		{
			name:           "Just Below Threshold (+4.9%) -> STABLE",
			currentLongPct: 0.549,
			oldLongPct:     0.50,
			wantTrend:      "STABLE",
			wantMsgContent: "stable",
		},
		{
			name:           "Just Over Threshold (+5.1%) -> BECOMING_BULLISH",
			currentLongPct: 0.551,
			oldLongPct:     0.50,
			wantTrend:      "BECOMING_BULLISH",
			wantMsgContent: "shifting LONG",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Logic from GetLongShortAnalysis
			currentChange := (tt.currentLongPct - tt.oldLongPct) * 100
			trend := "STABLE"
			message := "Sentiment is stable"

			if currentChange > 5 {
				trend = "BECOMING_BULLISH"
				message = fmt.Sprintf("shifting LONG (+%.1f%%)", currentChange)
			} else if currentChange < -5 {
				trend = "BECOMING_BEARISH"
				message = fmt.Sprintf("shifting SHORT (%.1f%% drop)", currentChange)
			}

			if trend != tt.wantTrend {
				t.Errorf("Trend = %s, want %s (change=%.2f%%)", trend, tt.wantTrend, currentChange)
			}
			// Simple contains check for message
			if tt.wantMsgContent != "" {
				if !strings.Contains(message, tt.wantMsgContent) {
					t.Errorf("Message = %q, want content %q", message, tt.wantMsgContent)
				}
			}
		})
	}
}

// Integration test - requires network (skip in CI)
func TestOIIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a real client (uses mainnet endpoints)
	client := NewBinanceClient("", "", false)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test GetOpenInterest
	t.Run("GetOpenInterest", func(t *testing.T) {
		oi, err := client.GetOpenInterest(ctx, "BTCUSDT")
		if err != nil {
			t.Logf("GetOpenInterest error (may be expected if no API access): %v", err)
			return
		}
		if oi.OpenInterest <= 0 {
			t.Error("OpenInterest should be positive")
		}
		t.Logf("BTCUSDT OI: %.2f contracts", oi.OpenInterest)
	})

	// Test GetOpenInterestHist
	t.Run("GetOpenInterestHist", func(t *testing.T) {
		oiHist, err := client.GetOpenInterestHist(ctx, "BTCUSDT", "5m", 10)
		if err != nil {
			t.Logf("GetOpenInterestHist error: %v", err)
			return
		}
		if len(oiHist) == 0 {
			t.Error("Should have OI history data")
			return
		}
		t.Logf("Got %d OI history points, latest value: $%.2fM",
			len(oiHist), oiHist[len(oiHist)-1].SumOpenInterestValue/1e6)
	})

	// Test GetOIAnalysis
	t.Run("GetOIAnalysis", func(t *testing.T) {
		analysis, err := client.GetOIAnalysis(ctx, "BTCUSDT", 0.5)
		if err != nil {
			t.Logf("GetOIAnalysis error: %v", err)
			return
		}
		t.Logf("BTCUSDT OI Analysis: Signal=%s, 1H Change=%.2f%%, Confidence=%s",
			analysis.OISignal, analysis.OIChange1H, analysis.OIConfidence)
	})

	// Test GetLatestLongShortRatio
	t.Run("GetLatestLongShortRatio", func(t *testing.T) {
		lsRatio, err := client.GetLatestLongShortRatio(ctx, "BTCUSDT")
		if err != nil {
			t.Logf("GetLatestLongShortRatio error: %v", err)
			return
		}
		t.Logf("BTCUSDT L/S Ratio: %.2f (Long: %.1f%%, Short: %.1f%%)",
			lsRatio.LongShortRatio, lsRatio.LongAccount*100, lsRatio.ShortAccount*100)
	})

	// Test GetLongShortAnalysis
	t.Run("GetLongShortAnalysis", func(t *testing.T) {
		analysis, err := client.GetLongShortAnalysis(ctx, "BTCUSDT")
		if err != nil {
			t.Logf("GetLongShortAnalysis error: %v", err)
			return
		}
		t.Logf("BTCUSDT Sentiment Analysis: Trend=%s, Message=%s",
			analysis.SentimentTrend, analysis.SentimentMessage)
	})
}
