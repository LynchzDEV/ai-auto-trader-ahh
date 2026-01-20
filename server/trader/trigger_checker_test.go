package trader

import (
	"testing"

	"auto-trader-ahh/exchange"
	"auto-trader-ahh/market"
)

// TestNewTriggerChecker verifies default configuration
func TestNewTriggerChecker(t *testing.T) {
	tc := NewTriggerChecker()

	if tc.EMA9Tolerance != 0.3 {
		t.Errorf("EMA9Tolerance = %.2f, want 0.3", tc.EMA9Tolerance)
	}
	if tc.EMA21Tolerance != 0.5 {
		t.Errorf("EMA21Tolerance = %.2f, want 0.5", tc.EMA21Tolerance)
	}
	if tc.VolumeSpikeMin != 1.5 {
		t.Errorf("VolumeSpikeMin = %.2f, want 1.5", tc.VolumeSpikeMin)
	}
}

// TestCheckTriggersNoConditions verifies signals with no conditions are immediately ready
func TestCheckTriggersNoConditions(t *testing.T) {
	tc := NewTriggerChecker()

	signal := &Signal{
		Symbol:            "BTCUSDT",
		Direction:         "LONG",
		TriggerConditions: []TriggerCondition{},
	}

	data := &market.MarketData{
		CurrentPrice: 50000,
		EMA9:         49500,
		EMA21:        49000,
		RSI:          55,
	}

	result := tc.CheckTriggers(signal, data)

	if !result.AllConditionsMet {
		t.Error("AllConditionsMet should be true for signal with no conditions")
	}
	if !result.ReadyToExecute {
		t.Error("ReadyToExecute should be true for signal with no conditions")
	}
	if result.ConditionsChecked != 0 {
		t.Errorf("ConditionsChecked = %d, want 0", result.ConditionsChecked)
	}
}

// TestCheckPullbackToEMA9Long verifies EMA9 pullback detection for LONG entries
func TestCheckPullbackToEMA9Long(t *testing.T) {
	tc := NewTriggerChecker()

	tests := []struct {
		name         string
		currentPrice float64
		ema9         float64
		expectMet    bool
	}{
		{
			name:         "Price at EMA9 - should trigger",
			currentPrice: 50000,
			ema9:         50000,
			expectMet:    true,
		},
		{
			name:         "Price below EMA9 - should trigger (buying the dip)",
			currentPrice: 49800,
			ema9:         50000,
			expectMet:    true,
		},
		{
			name:         "Price within tolerance above EMA9 - should trigger",
			currentPrice: 50100,
			ema9:         50000,
			expectMet:    true, // 0.2% above, within 0.3% tolerance
		},
		{
			name:         "Price too far above EMA9 - should NOT trigger",
			currentPrice: 51000,
			ema9:         50000,
			expectMet:    false, // 2% above, not a pullback
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal := &Signal{
				Symbol:    "BTCUSDT",
				Direction: "LONG",
				TriggerConditions: []TriggerCondition{
					{Type: TriggerPullbackEMA9},
				},
			}

			data := &market.MarketData{
				CurrentPrice: tt.currentPrice,
				EMA9:         tt.ema9,
			}

			result := tc.CheckTriggers(signal, data)

			if result.AllConditionsMet != tt.expectMet {
				t.Errorf("AllConditionsMet = %v, want %v. Price=%.0f, EMA9=%.0f",
					result.AllConditionsMet, tt.expectMet, tt.currentPrice, tt.ema9)
			}
		})
	}
}

// TestCheckPullbackToEMA9Short verifies EMA9 pullback detection for SHORT entries
func TestCheckPullbackToEMA9Short(t *testing.T) {
	tc := NewTriggerChecker()

	tests := []struct {
		name         string
		currentPrice float64
		ema9         float64
		expectMet    bool
	}{
		{
			name:         "Price at EMA9 - should trigger",
			currentPrice: 50000,
			ema9:         50000,
			expectMet:    true,
		},
		{
			name:         "Price above EMA9 - should trigger (selling the rally)",
			currentPrice: 50200,
			ema9:         50000,
			expectMet:    true,
		},
		{
			name:         "Price within tolerance below EMA9 - should trigger",
			currentPrice: 49900,
			ema9:         50000,
			expectMet:    true, // -0.2% below, within tolerance
		},
		{
			name:         "Price too far below EMA9 - should NOT trigger",
			currentPrice: 49000,
			ema9:         50000,
			expectMet:    false, // 2% below, not a bounce
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal := &Signal{
				Symbol:    "BTCUSDT",
				Direction: "SHORT",
				TriggerConditions: []TriggerCondition{
					{Type: TriggerPullbackEMA9},
				},
			}

			data := &market.MarketData{
				CurrentPrice: tt.currentPrice,
				EMA9:         tt.ema9,
			}

			result := tc.CheckTriggers(signal, data)

			if result.AllConditionsMet != tt.expectMet {
				t.Errorf("AllConditionsMet = %v, want %v. Price=%.0f, EMA9=%.0f",
					result.AllConditionsMet, tt.expectMet, tt.currentPrice, tt.ema9)
			}
		})
	}
}

// TestCheckRSIBelow verifies RSI below target for LONG pullback entries
// Reference: "In an uptrend, look for RSI to pull back toward the 40–50 zone"
func TestCheckRSIBelow(t *testing.T) {
	tc := NewTriggerChecker()

	tests := []struct {
		name        string
		rsi         float64
		targetValue float64
		expectMet   bool
	}{
		{
			name:        "RSI in pullback zone - should trigger",
			rsi:         45,
			targetValue: 50,
			expectMet:   true,
		},
		{
			name:        "RSI at target - should trigger",
			rsi:         50,
			targetValue: 50,
			expectMet:   true,
		},
		{
			name:        "RSI above target - should NOT trigger",
			rsi:         65,
			targetValue: 50,
			expectMet:   false,
		},
		{
			name:        "RSI deeply oversold - should trigger",
			rsi:         30,
			targetValue: 50,
			expectMet:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal := &Signal{
				Symbol:    "BTCUSDT",
				Direction: "LONG",
				TriggerConditions: []TriggerCondition{
					{Type: TriggerRSIBelow, TargetValue: tt.targetValue},
				},
			}

			data := &market.MarketData{
				RSI: tt.rsi,
			}

			result := tc.CheckTriggers(signal, data)

			if result.AllConditionsMet != tt.expectMet {
				t.Errorf("AllConditionsMet = %v, want %v. RSI=%.1f, Target=%.1f",
					result.AllConditionsMet, tt.expectMet, tt.rsi, tt.targetValue)
			}
		})
	}
}

// TestCheckRSIAbove verifies RSI above target for SHORT pullback entries
// Reference: "In a downtrend, watch RSI rise toward 50–60 before turning lower"
func TestCheckRSIAbove(t *testing.T) {
	tc := NewTriggerChecker()

	tests := []struct {
		name        string
		rsi         float64
		targetValue float64
		expectMet   bool
	}{
		{
			name:        "RSI in bounce zone - should trigger",
			rsi:         55,
			targetValue: 50,
			expectMet:   true,
		},
		{
			name:        "RSI at target - should trigger",
			rsi:         50,
			targetValue: 50,
			expectMet:   true,
		},
		{
			name:        "RSI below target - should NOT trigger",
			rsi:         35,
			targetValue: 50,
			expectMet:   false,
		},
		{
			name:        "RSI overbought - should trigger",
			rsi:         70,
			targetValue: 50,
			expectMet:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal := &Signal{
				Symbol:    "BTCUSDT",
				Direction: "SHORT",
				TriggerConditions: []TriggerCondition{
					{Type: TriggerRSIAbove, TargetValue: tt.targetValue},
				},
			}

			data := &market.MarketData{
				RSI: tt.rsi,
			}

			result := tc.CheckTriggers(signal, data)

			if result.AllConditionsMet != tt.expectMet {
				t.Errorf("AllConditionsMet = %v, want %v. RSI=%.1f, Target=%.1f",
					result.AllConditionsMet, tt.expectMet, tt.rsi, tt.targetValue)
			}
		})
	}
}

// TestCheckMACDCross verifies MACD cross detection
func TestCheckMACDCross(t *testing.T) {
	tc := NewTriggerChecker()

	tests := []struct {
		name      string
		direction string
		macdHist  float64
		expectMet bool
	}{
		{
			name:      "LONG with bullish MACD - should trigger",
			direction: "LONG",
			macdHist:  0.5,
			expectMet: true,
		},
		{
			name:      "LONG with bearish MACD - should NOT trigger",
			direction: "LONG",
			macdHist:  -0.5,
			expectMet: false,
		},
		{
			name:      "SHORT with bearish MACD - should trigger",
			direction: "SHORT",
			macdHist:  -0.5,
			expectMet: true,
		},
		{
			name:      "SHORT with bullish MACD - should NOT trigger",
			direction: "SHORT",
			macdHist:  0.5,
			expectMet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal := &Signal{
				Symbol:    "BTCUSDT",
				Direction: tt.direction,
				TriggerConditions: []TriggerCondition{
					{Type: TriggerMACDCross},
				},
			}

			data := &market.MarketData{
				MACDHist: tt.macdHist,
			}

			result := tc.CheckTriggers(signal, data)

			if result.AllConditionsMet != tt.expectMet {
				t.Errorf("AllConditionsMet = %v, want %v. Direction=%s, MACDHist=%.2f",
					result.AllConditionsMet, tt.expectMet, tt.direction, tt.macdHist)
			}
		})
	}
}

// TestCheckVolumeSpike verifies volume spike detection
func TestCheckVolumeSpike(t *testing.T) {
	tc := NewTriggerChecker()

	// Create klines with known volumes
	// Average of first 10: 100 each = 100 avg
	// Last candle needs to be >= 150 (1.5x) to trigger
	klines := make([]exchange.Kline, 12)
	for i := 0; i < 11; i++ {
		klines[i] = exchange.Kline{Volume: 100}
	}

	tests := []struct {
		name       string
		lastVolume float64
		expectMet  bool
	}{
		{
			name:       "Volume spike 2x average - should trigger",
			lastVolume: 200,
			expectMet:  true,
		},
		{
			name:       "Volume at 1.5x threshold - should trigger",
			lastVolume: 150,
			expectMet:  true,
		},
		{
			name:       "Normal volume - should NOT trigger",
			lastVolume: 100,
			expectMet:  false,
		},
		{
			name:       "Low volume - should NOT trigger",
			lastVolume: 50,
			expectMet:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testKlines := make([]exchange.Kline, len(klines))
			copy(testKlines, klines)
			testKlines[11] = exchange.Kline{Volume: tt.lastVolume}

			signal := &Signal{
				Symbol:    "BTCUSDT",
				Direction: "LONG",
				TriggerConditions: []TriggerCondition{
					{Type: TriggerVolumeSpike},
				},
			}

			data := &market.MarketData{
				Klines: testKlines,
			}

			result := tc.CheckTriggers(signal, data)

			if result.AllConditionsMet != tt.expectMet {
				t.Errorf("AllConditionsMet = %v, want %v. Volume=%.0f",
					result.AllConditionsMet, tt.expectMet, tt.lastVolume)
			}
		})
	}
}

// TestDetectBullishPatterns verifies bullish candlestick pattern detection
func TestDetectBullishPatterns(t *testing.T) {
	tc := NewTriggerChecker()

	tests := []struct {
		name            string
		current         exchange.Kline
		previous        exchange.Kline
		expectedPattern string
	}{
		{
			name: "Hammer pattern",
			current: exchange.Kline{
				Open:  100,
				Close: 102,
				High:  103,
				Low:   90, // Long lower wick
			},
			previous:        exchange.Kline{Open: 102, Close: 100, High: 103, Low: 99},
			expectedPattern: "hammer",
		},
		{
			name: "Bullish engulfing",
			current: exchange.Kline{
				Open:  98, // Opens below previous close
				Close: 105, // Closes above previous open
				High:  106,
				Low:   97,
			},
			previous: exchange.Kline{
				Open:  103,
				Close: 99, // Bearish candle
				High:  104,
				Low:   98,
			},
			expectedPattern: "bullish_engulfing",
		},
		{
			name: "Strong bullish candle",
			current: exchange.Kline{
				Open:  100,
				Close: 110, // Big body
				High:  110, // Closes at high
				Low:   99,
			},
			previous:        exchange.Kline{Open: 99, Close: 100, High: 101, Low: 98},
			expectedPattern: "strong_bullish",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns := tc.detectBullishPatterns(tt.current, tt.previous)

			found := false
			for _, p := range patterns {
				if p == tt.expectedPattern {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("Expected pattern '%s' not found. Got: %v", tt.expectedPattern, patterns)
			}
		})
	}
}

// TestDetectBearishPatterns verifies bearish candlestick pattern detection
func TestDetectBearishPatterns(t *testing.T) {
	tc := NewTriggerChecker()

	tests := []struct {
		name            string
		current         exchange.Kline
		previous        exchange.Kline
		expectedPattern string
	}{
		{
			name: "Shooting star pattern",
			current: exchange.Kline{
				Open:  102,
				Close: 100,
				High:  112, // Long upper wick
				Low:   99,
			},
			previous:        exchange.Kline{Open: 100, Close: 102, High: 103, Low: 99},
			expectedPattern: "shooting_star",
		},
		{
			name: "Bearish engulfing",
			current: exchange.Kline{
				Open:  105, // Opens above previous close
				Close: 98, // Closes below previous open
				High:  106,
				Low:   97,
			},
			previous: exchange.Kline{
				Open:  99,
				Close: 103, // Bullish candle
				High:  104,
				Low:   98,
			},
			expectedPattern: "bearish_engulfing",
		},
		{
			name: "Strong bearish candle",
			current: exchange.Kline{
				Open:  110,
				Close: 100, // Big body
				High:  111,
				Low:   100, // Closes at low
			},
			previous:        exchange.Kline{Open: 109, Close: 110, High: 111, Low: 108},
			expectedPattern: "strong_bearish",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns := tc.detectBearishPatterns(tt.current, tt.previous)

			found := false
			for _, p := range patterns {
				if p == tt.expectedPattern {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("Expected pattern '%s' not found. Got: %v", tt.expectedPattern, patterns)
			}
		})
	}
}

// TestMultipleConditions verifies that ALL conditions must be met
func TestMultipleConditions(t *testing.T) {
	tc := NewTriggerChecker()

	// Create market data where EMA pullback is met but RSI is not
	signal := &Signal{
		Symbol:    "BTCUSDT",
		Direction: "LONG",
		TriggerConditions: []TriggerCondition{
			{Type: TriggerPullbackEMA9},
			{Type: TriggerRSIBelow, TargetValue: 50},
		},
	}

	// EMA9 pullback met (price at EMA), but RSI still high
	data := &market.MarketData{
		CurrentPrice: 50000,
		EMA9:         50000,
		RSI:          65, // Above 50, not in pullback zone
	}

	result := tc.CheckTriggers(signal, data)

	if result.AllConditionsMet {
		t.Error("AllConditionsMet should be false when only 1/2 conditions met")
	}
	if result.ConditionsMet != 1 {
		t.Errorf("ConditionsMet = %d, want 1", result.ConditionsMet)
	}
	if result.ConditionsChecked != 2 {
		t.Errorf("ConditionsChecked = %d, want 2", result.ConditionsChecked)
	}
}

// TestAllConditionsMetTriggersExecution verifies signal state transition
func TestAllConditionsMetTriggersExecution(t *testing.T) {
	tc := NewTriggerChecker()

	signal := &Signal{
		Symbol:    "BTCUSDT",
		Direction: "LONG",
		TriggerConditions: []TriggerCondition{
			{Type: TriggerPullbackEMA9},
			{Type: TriggerRSIBelow, TargetValue: 50},
		},
	}

	// Both conditions met
	data := &market.MarketData{
		CurrentPrice: 49900, // At EMA9
		EMA9:         50000,
		RSI:          45, // Below 50
	}

	result := tc.CheckTriggers(signal, data)

	if !result.AllConditionsMet {
		t.Error("AllConditionsMet should be true when all conditions met")
	}
	if !result.ReadyToExecute {
		t.Error("ReadyToExecute should be true when all conditions met")
	}
	if result.ConditionsMet != 2 {
		t.Errorf("ConditionsMet = %d, want 2", result.ConditionsMet)
	}

	// Verify individual conditions were marked as met
	for i, cond := range signal.TriggerConditions {
		if !cond.Met {
			t.Errorf("Condition %d (%s) should be marked as Met", i, cond.Type)
		}
	}
}

// TestBullishCandleDetection verifies candlestick pattern trigger
func TestBullishCandleDetection(t *testing.T) {
	tc := NewTriggerChecker()

	// Create klines with a hammer pattern at the end
	klines := []exchange.Kline{
		{Open: 100, Close: 101, High: 102, Low: 99, Volume: 100},  // -3
		{Open: 101, Close: 99, High: 102, Low: 98, Volume: 100},   // -2 (previous)
		{Open: 99, Close: 101, High: 102, Low: 89, Volume: 150},   // -1 (hammer - last completed)
		{Open: 101, Close: 102, High: 103, Low: 100, Volume: 120}, // current (forming)
	}

	signal := &Signal{
		Symbol:    "BTCUSDT",
		Direction: "LONG",
		TriggerConditions: []TriggerCondition{
			{Type: TriggerBullishCandle},
		},
	}

	data := &market.MarketData{
		Klines: klines,
	}

	result := tc.CheckTriggers(signal, data)

	if !result.AllConditionsMet {
		t.Error("Should detect hammer pattern as bullish confirmation")
	}
}

// TestConditionMarkedMetOnlyOnce verifies idempotent condition updates
func TestConditionMarkedMetOnlyOnce(t *testing.T) {
	tc := NewTriggerChecker()

	signal := &Signal{
		Symbol:    "BTCUSDT",
		Direction: "LONG",
		TriggerConditions: []TriggerCondition{
			{Type: TriggerRSIBelow, TargetValue: 50},
		},
	}

	data := &market.MarketData{
		RSI: 45,
	}

	// First check
	tc.CheckTriggers(signal, data)
	firstMetAt := signal.TriggerConditions[0].MetAt

	// Second check
	tc.CheckTriggers(signal, data)
	secondMetAt := signal.TriggerConditions[0].MetAt

	// MetAt should not change on subsequent checks
	if !firstMetAt.Equal(secondMetAt) {
		t.Error("MetAt should not change once condition is met")
	}
}
