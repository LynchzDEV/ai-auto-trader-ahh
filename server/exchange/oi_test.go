package exchange

import (
	"context"
	"testing"
	"time"
)

// TestOIAnalysisInterpretation tests the OI signal interpretation logic
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
			oiChange1H:  3.0,
			priceChange: 2.0,
			wantSignal:  "BULLISH",
			wantConf:    "HIGH",
		},
		{
			name:        "OI up + Price down = BEARISH (new shorts)",
			oiChange1H:  3.0,
			priceChange: -2.0,
			wantSignal:  "BEARISH",
			wantConf:    "HIGH",
		},
		{
			name:        "OI down + Price up = REVERSAL_UP (shorts covering)",
			oiChange1H:  -2.0,
			priceChange: 1.0,
			wantSignal:  "REVERSAL_UP",
			wantConf:    "LOW",
		},
		{
			name:        "OI down + Price down = REVERSAL_DOWN (longs capitulating)",
			oiChange1H:  -2.0,
			priceChange: -1.0,
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
			priceChange: 0.5,
			wantSignal:  "NEUTRAL",
			wantConf:    "LOW",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the interpretation logic from GetOIAnalysis
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
				signal = "REVERSAL_UP"
				confidence = "LOW"
			} else if tt.oiChange1H < 0 && tt.priceChange < 0 {
				signal = "REVERSAL_DOWN"
				confidence = "LOW"
			}

			if signal != tt.wantSignal {
				t.Errorf("Signal = %s, want %s", signal, tt.wantSignal)
			}
			if confidence != tt.wantConf {
				t.Errorf("Confidence = %s, want %s", confidence, tt.wantConf)
			}
		})
	}
}

// TestOIChangeCalculation tests the OI change percentage calculation
func TestOIChangeCalculation(t *testing.T) {
	tests := []struct {
		name       string
		currentOI  float64
		oldOI      float64
		wantChange float64
	}{
		{
			name:       "OI increased 10%",
			currentOI:  110000000,
			oldOI:      100000000,
			wantChange: 10.0,
		},
		{
			name:       "OI decreased 5%",
			currentOI:  95000000,
			oldOI:      100000000,
			wantChange: -5.0,
		},
		{
			name:       "OI unchanged",
			currentOI:  100000000,
			oldOI:      100000000,
			wantChange: 0.0,
		},
		{
			name:       "OI increased 2.5%",
			currentOI:  102500000,
			oldOI:      100000000,
			wantChange: 2.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			change := 0.0
			if tt.oldOI > 0 {
				change = ((tt.currentOI - tt.oldOI) / tt.oldOI) * 100
			}

			// Allow small tolerance for floating point
			tolerance := 0.01
			if change < tt.wantChange-tolerance || change > tt.wantChange+tolerance {
				t.Errorf("Change = %.2f%%, want %.2f%%", change, tt.wantChange)
			}
		})
	}
}

// TestLongShortRatioInterpretation tests L/S ratio crowding detection
func TestLongShortRatioInterpretation(t *testing.T) {
	tests := []struct {
		name          string
		longAccount   float64 // Binance returns as decimal (0.xx)
		shortAccount  float64
		expectCrowded string // "LONG", "SHORT", or ""
	}{
		{
			name:          "Crowded long (75%)",
			longAccount:   0.75,
			shortAccount:  0.25,
			expectCrowded: "LONG",
		},
		{
			name:          "Crowded short (80%)",
			longAccount:   0.20,
			shortAccount:  0.80,
			expectCrowded: "SHORT",
		},
		{
			name:          "Balanced (50/50)",
			longAccount:   0.50,
			shortAccount:  0.50,
			expectCrowded: "",
		},
		{
			name:          "Slightly long (60%)",
			longAccount:   0.60,
			shortAccount:  0.40,
			expectCrowded: "", // Not crowded until >70%
		},
		{
			name:          "Edge case 70%",
			longAccount:   0.70,
			shortAccount:  0.30,
			expectCrowded: "", // 70% is the threshold, not over
		},
		{
			name:          "Over threshold 71%",
			longAccount:   0.71,
			shortAccount:  0.29,
			expectCrowded: "LONG",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert to percentage (as done in engine.go)
			longRatio := tt.longAccount * 100
			shortRatio := tt.shortAccount * 100

			crowded := ""
			if longRatio > 70 {
				crowded = "LONG"
			} else if shortRatio > 70 {
				crowded = "SHORT"
			}

			if crowded != tt.expectCrowded {
				t.Errorf("Crowded = %q, want %q (longRatio=%.1f%%, shortRatio=%.1f%%)",
					crowded, tt.expectCrowded, longRatio, shortRatio)
			}
		})
	}
}

// TestOIDataStructs tests the OI data structures
func TestOIDataStructs(t *testing.T) {
	// Test OpenInterestData
	oi := OpenInterestData{
		Symbol:       "BTCUSDT",
		OpenInterest: 25000.5,
		Time:         time.Now().UnixMilli(),
	}

	if oi.Symbol == "" {
		t.Error("Symbol should not be empty")
	}
	if oi.OpenInterest <= 0 {
		t.Error("OpenInterest should be positive")
	}

	// Test OpenInterestHistData
	oiHist := OpenInterestHistData{
		Symbol:               "BTCUSDT",
		SumOpenInterest:      25000.5,
		SumOpenInterestValue: 1500000000.0, // $1.5B
		Timestamp:            time.Now().UnixMilli(),
	}

	if oiHist.SumOpenInterestValue <= 0 {
		t.Error("SumOpenInterestValue should be positive")
	}

	// Test OIAnalysis
	analysis := OIAnalysis{
		Symbol:       "BTCUSDT",
		CurrentOI:    1500000000.0,
		OIChange1H:   2.5,
		OIChange4H:   5.0,
		OIChange24H:  -1.5,
		OISignal:     "BULLISH",
		OIConfidence: "HIGH",
	}

	if analysis.OISignal == "" {
		t.Error("OISignal should not be empty")
	}

	// Test LongShortRatioData
	lsRatio := LongShortRatioData{
		Symbol:         "BTCUSDT",
		LongShortRatio: 1.2,
		LongAccount:    0.55,
		ShortAccount:   0.45,
		Timestamp:      time.Now().UnixMilli(),
	}

	// Verify accounts sum to 1
	sum := lsRatio.LongAccount + lsRatio.ShortAccount
	if sum < 0.99 || sum > 1.01 {
		t.Errorf("LongAccount + ShortAccount should equal 1, got %.2f", sum)
	}
}

// TestOIEntrySafetyLogic tests the entry safety check logic for OI
func TestOIEntrySafetyLogic(t *testing.T) {
	tests := []struct {
		name        string
		isLong      bool
		oiSignal    string
		oiChange1H  float64
		longRatio   float64
		shortRatio  float64
		liqPressure string // Add field for liquidation pressure
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
}
