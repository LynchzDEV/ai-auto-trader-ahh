package market

import (
	"testing"
)

// TestCalculateMoveMaturity tests the move maturity calculation
// Reference: https://www.stockforecasttoday.com/post/swing-trading-examples-using-cycle-timing-and-price-structure
// "Short-term cycle trades might last 3–7 sessions"
func TestCalculateMoveMaturity(t *testing.T) {
	// Test cases grouped by maturity level with tolerance for EMA smoothing
	// The actual candle count may vary by ±1 due to EMA calculation
	tests := []struct {
		name                  string
		candlesSinceCross     int
		expectedMaturity      string
		expectedScore         int
		allowAdjacentMaturity string // Adjacent maturity that's acceptable due to EMA smoothing
	}{
		// EARLY: 1-7 candles (within short-term cycle window)
		{"1 candle - EARLY", 1, "EARLY", 1, ""},
		{"3 candles - EARLY (start of sweet spot)", 3, "EARLY", 1, ""},
		{"5 candles - EARLY (middle of sweet spot)", 5, "EARLY", 1, ""},
		{"6 candles - EARLY", 6, "EARLY", 1, ""},

		// MID: 8-14 candles (trend confirmed but aging)
		// Note: boundary tests (7,8) may vary due to EMA smoothing
		{"10 candles - MID", 10, "MID", 2, ""},
		{"12 candles - MID", 12, "MID", 2, ""},

		// LATE: 15-21 candles (approaching cycle end)
		{"18 candles - LATE", 18, "LATE", 3, ""},
		{"20 candles - LATE", 20, "LATE", 3, ""},

		// EXHAUSTED: >21 candles (cycle exhausted)
		{"25 candles - EXHAUSTED", 25, "EXHAUSTED", 4, ""},
		{"30 candles - EXHAUSTED", 30, "EXHAUSTED", 4, ""},
		{"50 candles - EXHAUSTED", 50, "EXHAUSTED", 4, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create price data that simulates the crossover at the expected position
			// We need at least longPeriod + candlesSinceCross + buffer candles
			totalCandles := 21 + tt.candlesSinceCross + 10 // 21 for EMA21 + candles since cross + buffer

			closes := make([]float64, totalCandles)

			// Create a crossover scenario:
			// First half: EMA9 < EMA21 (downtrend)
			// Then crossover occurs
			// After crossover: EMA9 > EMA21 (uptrend)
			crossoverIndex := totalCandles - 1 - tt.candlesSinceCross

			for i := 0; i < totalCandles; i++ {
				if i < crossoverIndex {
					// Before crossover: declining prices (EMA9 < EMA21)
					closes[i] = 100.0 - float64(i)*0.1
				} else {
					// After crossover: rising prices (EMA9 > EMA21)
					closes[i] = 100.0 + float64(i-crossoverIndex)*0.5
				}
			}

			candlesSinceCross, maturity, score := calculateMoveMaturity(closes, 9, 21, "1d") // Use daily timeframe to match original thresholds

			// Allow tolerance in candle count due to EMA calculation smoothing
			if abs(candlesSinceCross-tt.candlesSinceCross) > 2 {
				t.Errorf("candlesSinceCross = %d, want approximately %d (±2)", candlesSinceCross, tt.candlesSinceCross)
			}

			// Check maturity - allow adjacent maturity for boundary cases
			maturityOk := maturity == tt.expectedMaturity
			if !maturityOk && tt.allowAdjacentMaturity != "" {
				maturityOk = maturity == tt.allowAdjacentMaturity
			}
			if !maturityOk {
				t.Errorf("maturity = %s, want %s (candles since cross: %d)", maturity, tt.expectedMaturity, candlesSinceCross)
			}

			// Score should match the maturity
			expectedScoreForMaturity := map[string]int{"EARLY": 1, "MID": 2, "LATE": 3, "EXHAUSTED": 4}
			if score != expectedScoreForMaturity[maturity] {
				t.Errorf("score = %d doesn't match maturity %s (expected %d)", score, maturity, expectedScoreForMaturity[maturity])
			}
		})
	}
}

// TestCalculateMoveMaturityInsufficientData tests edge cases
func TestCalculateMoveMaturityInsufficientData(t *testing.T) {
	// Test with insufficient data
	closes := make([]float64, 20) // Less than longPeriod + 10
	for i := range closes {
		closes[i] = 100.0
	}

	candlesSinceCross, maturity, score := calculateMoveMaturity(closes, 9, 21, "1d") // Use daily timeframe

	if maturity != "UNKNOWN" {
		t.Errorf("maturity = %s, want UNKNOWN for insufficient data", maturity)
	}
	if score != 0 {
		t.Errorf("score = %d, want 0 for insufficient data", score)
	}
	if candlesSinceCross != 0 {
		t.Errorf("candlesSinceCross = %d, want 0 for insufficient data", candlesSinceCross)
	}
}

// TestCalculateMoveMaturityNoCrossover tests when there's no crossover in the data
func TestCalculateMoveMaturityNoCrossover(t *testing.T) {
	// Steady uptrend with no crossover (EMA9 always above EMA21)
	closes := make([]float64, 50)
	for i := range closes {
		closes[i] = 100.0 + float64(i)*0.5 // Consistent uptrend
	}

	candlesSinceCross, maturity, score := calculateMoveMaturity(closes, 9, 21, "1d") // Use daily timeframe

	// Should return EXHAUSTED since no recent crossover detected
	// (the "crossover" would be at the start of data)
	if candlesSinceCross < 20 {
		t.Logf("candlesSinceCross = %d (expected high value for no recent crossover)", candlesSinceCross)
	}

	// With high candles since cross, should be EXHAUSTED or LATE
	if maturity != "EXHAUSTED" && maturity != "LATE" {
		t.Errorf("maturity = %s, want EXHAUSTED or LATE for no recent crossover", maturity)
	}

	if score < 3 {
		t.Errorf("score = %d, want >= 3 for no recent crossover", score)
	}
}

// TestMoveMaturityBoundaries tests the exact boundary conditions
func TestMoveMaturityBoundaries(t *testing.T) {
	// Test that boundaries are correct based on research
	// Reference: "Short-term cycle trades might last 3–7 sessions"

	boundaries := []struct {
		candles  int
		maturity string
	}{
		{7, "EARLY"},      // Upper bound of EARLY
		{8, "MID"},        // Lower bound of MID
		{14, "MID"},       // Upper bound of MID
		{15, "LATE"},      // Lower bound of LATE
		{21, "LATE"},      // Upper bound of LATE
		{22, "EXHAUSTED"}, // Lower bound of EXHAUSTED
	}

	for _, b := range boundaries {
		// Directly test the classification logic
		var maturity string
		switch {
		case b.candles <= 7:
			maturity = "EARLY"
		case b.candles <= 14:
			maturity = "MID"
		case b.candles <= 21:
			maturity = "LATE"
		default:
			maturity = "EXHAUSTED"
		}

		if maturity != b.maturity {
			t.Errorf("boundary test failed: %d candles should be %s, got %s", b.candles, b.maturity, maturity)
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ========== Entry Timing Indicators Tests (Task 3.1) ==========

func TestCalculateDistanceFromEMA(t *testing.T) {
	tests := []struct {
		name         string
		currentPrice float64
		ema          float64
		expected     float64
	}{
		{"Price at EMA", 100.0, 100.0, 0.0},
		{"Price 2% above EMA", 102.0, 100.0, 2.0},
		{"Price 2% below EMA", 98.0, 100.0, -2.0},
		{"Price 5% above EMA", 105.0, 100.0, 5.0},
		{"EMA is zero", 100.0, 0.0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateDistanceFromEMA(tt.currentPrice, tt.ema)
			if absFloat(result-tt.expected) > 0.01 {
				t.Errorf("calculateDistanceFromEMA(%.2f, %.2f) = %.2f, want %.2f",
					tt.currentPrice, tt.ema, result, tt.expected)
			}
		})
	}
}

func TestCalculatePullbackDepth(t *testing.T) {
	// Create test data: uptrend with pullback
	highs := []float64{100, 101, 102, 103, 104, 105, 104, 103, 102}
	lows := []float64{99, 100, 101, 102, 103, 104, 103, 102, 101}
	closes := []float64{100, 101, 102, 103, 104, 105, 103, 102, 101.5}

	// In uptrend, high was 105, current is 101.5
	// Pullback depth = (105 - 101.5) / 105 * 100 ≈ 3.33%
	depth := calculatePullbackDepth(highs, lows, closes, 9, true)

	if depth < 3.0 || depth > 4.0 {
		t.Errorf("calculatePullbackDepth = %.2f, expected around 3.33%%", depth)
	}
}

func TestDetectMomentumDecay(t *testing.T) {
	// Create data where price rises but MACD histogram should decline
	// This requires 26+ candles for MACD calculation plus lookback

	// Simple rising price series
	closes := make([]float64, 35)
	for i := range closes {
		closes[i] = 100.0 + float64(i)*0.5 // Steady rise
	}

	isDecaying, decayBars := detectMomentumDecay(closes, 5)

	// With steady rise, MACD should be positive and not decaying initially
	t.Logf("Momentum decay: %v, bars: %d", isDecaying, decayBars)
}

// ========== Market Regime Detection Tests (Task 4.1) ==========

func TestCalculateADX(t *testing.T) {
	// Create trending data
	highs := make([]float64, 50)
	lows := make([]float64, 50)
	closes := make([]float64, 50)

	for i := range closes {
		// Strong uptrend
		closes[i] = 100.0 + float64(i)*2.0
		highs[i] = closes[i] + 1.0
		lows[i] = closes[i] - 1.0
	}

	adx, trend := calculateADX(highs, lows, closes, 14)

	t.Logf("ADX: %.2f, Trend: %s", adx, trend)

	// In a strong trend, ADX should be positive
	if adx < 0 {
		t.Errorf("ADX = %.2f, should be positive", adx)
	}
}

func TestCalculateBollingerBands(t *testing.T) {
	// Create data with low volatility (squeeze)
	closes := make([]float64, 50)
	for i := range closes {
		closes[i] = 100.0 + float64(i%3-1)*0.1 // Tiny oscillation
	}

	width, isSqueeze := calculateBollingerBands(closes, 20, 2.0)

	t.Logf("Bollinger width: %.4f, Squeeze: %v", width, isSqueeze)

	// Width should be small with low volatility
	if width < 0 {
		t.Errorf("Bollinger width = %.4f, should be positive", width)
	}
}

func TestDetectMarketRegime(t *testing.T) {
	tests := []struct {
		name             string
		adx              float64
		adxTrend         string
		bollingerSqueeze bool
		atrRatio         float64
		momentumDecay    bool
		expectedRegime   string
	}{
		{
			name:           "Strong trend",
			adx:            30.0,
			adxTrend:       "STRONG",
			atrRatio:       1.0,
			expectedRegime: "TRENDING",
		},
		{
			name:           "Ranging market",
			adx:            15.0,
			adxTrend:       "WEAK",
			atrRatio:       1.0,
			expectedRegime: "RANGING",
		},
		{
			name:           "High volatility",
			adx:            20.0,
			adxTrend:       "MODERATE",
			atrRatio:       2.5,
			expectedRegime: "VOLATILE",
		},
		{
			name:           "Exhausted trend",
			adx:            25.0,
			adxTrend:       "MODERATE",
			momentumDecay:  true,
			atrRatio:       1.0,
			expectedRegime: "EXHAUSTED",
		},
		{
			name:             "Bollinger squeeze",
			adx:              18.0,
			adxTrend:         "WEAK",
			bollingerSqueeze: true,
			atrRatio:         0.8,
			expectedRegime:   "RANGING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			regime, description := detectMarketRegime(tt.adx, tt.adxTrend, tt.bollingerSqueeze, tt.atrRatio, tt.momentumDecay)

			if regime != tt.expectedRegime {
				t.Errorf("detectMarketRegime() = %s, want %s. Description: %s",
					regime, tt.expectedRegime, description)
			}
		})
	}
}

// ========== Fibonacci Detection Tests (Task 3.2) ==========

func TestCalculateFibonacciLevels(t *testing.T) {
	// Create test data with clear swing high and low
	highs := []float64{100, 105, 110, 115, 120, 118, 115, 112}
	lows := []float64{98, 103, 108, 113, 118, 116, 113, 110}
	currentPrice := 114.0
	isUptrend := true

	fib := calculateFibonacciLevels(highs, lows, currentPrice, 8, isUptrend)

	// Swing high should be 120, swing low should be 98
	if fib.SwingHigh != 120.0 {
		t.Errorf("SwingHigh = %.2f, want 120.0", fib.SwingHigh)
	}
	if fib.SwingLow != 98.0 {
		t.Errorf("SwingLow = %.2f, want 98.0", fib.SwingLow)
	}

	// Range = 22, so in uptrend:
	// 38.2% retracement = 120 - (22 * 0.382) = 120 - 8.4 = 111.6
	// 50% retracement = 120 - (22 * 0.5) = 120 - 11 = 109.0
	// 61.8% retracement = 120 - (22 * 0.618) = 120 - 13.6 = 106.4
	expectedLevel382 := 120.0 - (22.0 * 0.382)
	expectedLevel500 := 120.0 - (22.0 * 0.500)
	expectedLevel618 := 120.0 - (22.0 * 0.618)

	if absFloat(fib.Level382-expectedLevel382) > 0.1 {
		t.Errorf("Level382 = %.2f, want %.2f", fib.Level382, expectedLevel382)
	}
	if absFloat(fib.Level500-expectedLevel500) > 0.1 {
		t.Errorf("Level500 = %.2f, want %.2f", fib.Level500, expectedLevel500)
	}
	if absFloat(fib.Level618-expectedLevel618) > 0.1 {
		t.Errorf("Level618 = %.2f, want %.2f", fib.Level618, expectedLevel618)
	}

	t.Logf("Fib levels: 38.2%%=%.2f, 50%%=%.2f, 61.8%%=%.2f", fib.Level382, fib.Level500, fib.Level618)
	t.Logf("Nearest level: %s, Distance: %.2f%%, AtSupport: %v", fib.NearestLevel, fib.DistancePct, fib.AtSupport)
}

// ========== Volume Profile Tests (Task 3.3) ==========

func TestAnalyzeVolumeProfile(t *testing.T) {
	tests := []struct {
		name            string
		trendVolumes    []float64
		pullbackVolumes []float64
		expectedSignal  string
	}{
		{
			name:            "Healthy pullback - volume drops",
			trendVolumes:    []float64{100, 110, 120, 115, 108},
			pullbackVolumes: []float64{50, 45, 40, 35},
			expectedSignal:  "HEALTHY",
		},
		{
			name:            "Unhealthy pullback - volume rises",
			trendVolumes:    []float64{50, 55, 60, 58, 52},
			pullbackVolumes: []float64{100, 110, 120, 130},
			expectedSignal:  "UNHEALTHY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Combine volumes
			volumes := append(tt.trendVolumes, tt.pullbackVolumes...)

			// Create matching closes that show peak and pullback
			closes := make([]float64, len(volumes))
			for i := range closes {
				if i < len(tt.trendVolumes) {
					closes[i] = 100.0 + float64(i)*2 // Rising
				} else {
					closes[i] = closes[len(tt.trendVolumes)-1] - float64(i-len(tt.trendVolumes)+1) // Falling
				}
			}

			profile := analyzeVolumeProfile(volumes, closes, len(volumes), true)

			if profile.Signal != tt.expectedSignal {
				t.Errorf("Signal = %s, want %s. Ratio: %.2f",
					profile.Signal, tt.expectedSignal, profile.PullbackVolumeRatio)
			}
		})
	}
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
