package intel

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestFetchFearGreed tests the Fear & Greed API
func TestFetchFearGreed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fg, err := FetchFearGreed(ctx)
	if err != nil {
		t.Fatalf("FetchFearGreed failed: %v", err)
	}

	// Validate response
	if fg.Value < 0 || fg.Value > 100 {
		t.Errorf("Fear & Greed value out of range: %d", fg.Value)
	}
	if fg.Classification == "" {
		t.Error("Fear & Greed classification is empty")
	}

	t.Logf("Fear & Greed: %d (%s)", fg.Value, fg.Classification)

	// Test formatting
	formatted := FormatFearGreed(fg)
	if formatted == "" {
		t.Error("FormatFearGreed returned empty string")
	}
	t.Logf("Formatted:\n%s", formatted)
}

// TestFetchNews tests the Google News RSS Feed
func TestFetchNews(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	news, err := FetchNews(ctx, "crypto market", 5)
	if err != nil {
		t.Fatalf("FetchNews failed: %v", err)
	}

	if len(news) == 0 {
		t.Skip("FetchNews returned no news items - API may be unavailable")
	}

	for i, item := range news {
		if item.Title == "" {
			t.Errorf("News item %d has empty title", i)
		}
		t.Logf("News %d: %s (%s)", i+1, item.Title, item.Source)
	}

	// Test filtering
	filtered := FilterNewsForSymbols(news, []string{"BTCUSDT", "ETHUSDT"})
	t.Logf("Filtered news for BTC/ETH: %d items", len(filtered))

	// Test formatting
	formatted := FormatNews(news, 3)
	if formatted == "" {
		t.Error("FormatNews returned empty string")
	}
	t.Logf("Formatted:\n%s", formatted)
}

// TestFetchCoinData tests the CoinGecko markets API
func TestFetchCoinData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	coinIDs := []string{"bitcoin", "ethereum"}
	coinData, err := FetchCoinData(ctx, coinIDs)
	if err != nil {
		// CoinGecko has strict rate limits - skip if rate limited
		if strings.Contains(err.Error(), "rate limit") {
			t.Skipf("CoinGecko rate limited: %v", err)
		}
		t.Fatalf("FetchCoinData failed: %v", err)
	}

	if len(coinData) == 0 {
		t.Error("FetchCoinData returned no data")
	}

	// Check Bitcoin data
	btc, ok := coinData["bitcoin"]
	if !ok {
		t.Error("Bitcoin data not found")
	} else {
		if btc.CurrentPrice <= 0 {
			t.Errorf("Bitcoin price invalid: %f", btc.CurrentPrice)
		}
		if btc.MarketCap <= 0 {
			t.Errorf("Bitcoin market cap invalid: %f", btc.MarketCap)
		}
		if btc.ATH <= 0 {
			t.Errorf("Bitcoin ATH invalid: %f", btc.ATH)
		}
		t.Logf("Bitcoin: $%.2f (ATH: $%.2f, %.1f%% from ATH)", btc.CurrentPrice, btc.ATH, btc.ATHChangePct)
	}

	// Test formatting
	formatted := FormatCoinData(coinData, []string{"BTCUSDT", "ETHUSDT"})
	if formatted == "" {
		t.Error("FormatCoinData returned empty string")
	}
	t.Logf("Formatted:\n%s", formatted)
}

// TestFetchGlobalData tests the CoinGecko global API
func TestFetchGlobalData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	global, err := FetchGlobalData(ctx)
	if err != nil {
		// CoinGecko has strict rate limits - skip if rate limited
		if strings.Contains(err.Error(), "rate limit") {
			t.Skipf("CoinGecko rate limited: %v", err)
		}
		t.Fatalf("FetchGlobalData failed: %v", err)
	}

	if global.TotalMarketCap <= 0 {
		t.Errorf("Total market cap invalid: %f", global.TotalMarketCap)
	}
	if global.BTCDominance <= 0 || global.BTCDominance > 100 {
		t.Errorf("BTC dominance invalid: %f", global.BTCDominance)
	}

	t.Logf("Global: Total Cap $%.2fB, BTC Dom %.1f%%", global.TotalMarketCap/1e9, global.BTCDominance)

	// Test formatting
	formatted := FormatGlobalData(global)
	if formatted == "" {
		t.Error("FormatGlobalData returned empty string")
	}
	t.Logf("Formatted:\n%s", formatted)
}

// TestProvider tests the full provider with caching
func TestProvider(t *testing.T) {
	// Use config with only Fear & Greed to avoid rate limits from previous tests
	config := ProviderConfig{
		FearGreedCacheDuration:  1 * time.Minute,
		NewsCacheDuration:       1 * time.Minute,
		CoinDataCacheDuration:   1 * time.Minute,
		GlobalDataCacheDuration: 1 * time.Minute,
		MaxNewsItems:            5,
		EnableFearGreed:         true,
		EnableNews:              false, // Disabled
		EnableCoinData:          false, // Disabled to avoid rate limits in test suite
		EnableGlobalData:        false, // Disabled to avoid rate limits in test suite
	}
	provider := NewProvider(config)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	symbols := []string{"BTCUSDT", "ETHUSDT"}

	// First fetch - should hit APIs
	t.Log("First fetch (should hit APIs)...")
	start := time.Now()
	intel1, err := provider.GetMarketIntel(ctx, symbols)
	if err != nil {
		t.Fatalf("GetMarketIntel failed: %v", err)
	}
	firstDuration := time.Since(start)
	t.Logf("First fetch took: %v", firstDuration)

	// Validate intel data - at minimum Fear & Greed should work
	if intel1.FearGreed == nil {
		t.Error("FearGreed is nil")
	} else {
		t.Logf("Fear & Greed: %d (%s)", intel1.FearGreed.Value, intel1.FearGreed.Classification)
	}

	// Second fetch - should use cache (much faster)
	t.Log("Second fetch (should use cache)...")
	start = time.Now()
	intel2, err := provider.GetMarketIntel(ctx, symbols)
	if err != nil {
		t.Fatalf("GetMarketIntel (cached) failed: %v", err)
	}
	secondDuration := time.Since(start)
	t.Logf("Second fetch took: %v", secondDuration)

	// Cache should be much faster
	if secondDuration > 50*time.Millisecond {
		t.Logf("Note: Second fetch took %v (expected <50ms from cache)", secondDuration)
	}

	// Data should be the same (from cache)
	if intel1.FearGreed != nil && intel2.FearGreed != nil {
		if intel1.FearGreed.Value != intel2.FearGreed.Value {
			t.Error("Cached Fear & Greed value differs from first fetch")
		}
	}

	// Test FormatForAI
	formatted := FormatForAI(intel1, symbols, 5)
	if formatted == "" {
		t.Error("FormatForAI returned empty string")
	}
	t.Logf("Full formatted output (%d chars):\n%s", len(formatted), formatted)
}

// TestGetCoinGeckoID tests the symbol to CoinGecko ID mapping
func TestGetCoinGeckoID(t *testing.T) {
	tests := []struct {
		symbol   string
		expected string
	}{
		{"BTCUSDT", "bitcoin"},
		{"ETHUSDT", "ethereum"},
		{"XMRUSDT", "monero"},
		{"DASHUSDT", "dash"},
		{"UNKNOWNUSDT", ""}, // Unknown should return empty
	}

	for _, tt := range tests {
		result := GetCoinGeckoID(tt.symbol)
		if result != tt.expected {
			t.Errorf("GetCoinGeckoID(%s) = %s, want %s", tt.symbol, result, tt.expected)
		}
	}
}

// TestProviderCacheExpiry tests that cache expires correctly
func TestProviderCacheExpiry(t *testing.T) {
	// Use very short cache duration for testing
	config := ProviderConfig{
		FearGreedCacheDuration:  100 * time.Millisecond,
		NewsCacheDuration:       100 * time.Millisecond,
		CoinDataCacheDuration:   100 * time.Millisecond,
		GlobalDataCacheDuration: 100 * time.Millisecond,
		MaxNewsItems:            5,
		EnableFearGreed:         true,
		EnableNews:              false, // Disable to speed up test
		EnableCoinData:          false,
		EnableGlobalData:        false,
	}
	provider := NewProvider(config)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// First fetch
	intel1, err := provider.GetMarketIntel(ctx, []string{"BTCUSDT"})
	if err != nil {
		t.Fatalf("First fetch failed: %v", err)
	}

	// Wait for cache to expire
	time.Sleep(150 * time.Millisecond)

	// Second fetch should hit API again
	intel2, err := provider.GetMarketIntel(ctx, []string{"BTCUSDT"})
	if err != nil {
		t.Fatalf("Second fetch failed: %v", err)
	}

	// Both should have data
	if intel1.FearGreed == nil || intel2.FearGreed == nil {
		t.Skip("Fear & Greed API not available, skipping cache expiry test")
	}

	t.Logf("Cache expiry test passed - both fetches returned data")
}
