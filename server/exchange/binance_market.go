package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Ticker24h represents 24h ticker statistics
type Ticker24h struct {
	Symbol      string  `json:"symbol"`
	PriceChange float64 `json:"priceChangePercent,string"`
	LastPrice   float64 `json:"lastPrice,string"`
	Volume      float64 `json:"volume,string"`      // Base Asset Volume
	QuoteVolume float64 `json:"quoteVolume,string"` // Quote Asset Volume (USDT)
	Count       int64   `json:"count"`              // Trade Count
}

// Get24hTicker returns 24h ticker data for all symbols
func (c *BinanceClient) Get24hTicker(ctx context.Context) ([]Ticker24h, error) {
	// 24h ticker endpoint is public, no signature needed
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/fapi/v1/ticker/24hr", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d)", resp.StatusCode)
	}

	var tickers []Ticker24h
	if err := json.NewDecoder(resp.Body).Decode(&tickers); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return tickers, nil
}

// GetTickerStats returns 24h stats for a single symbol
func (c *BinanceClient) GetTickerStats(ctx context.Context, symbol string) (*Ticker24h, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/fapi/v1/ticker/24hr?symbol="+symbol, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d)", resp.StatusCode)
	}

	var ticker Ticker24h
	if err := json.NewDecoder(resp.Body).Decode(&ticker); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &ticker, nil
}

// GetTopVolumeCoins returns top N coins by 24h Quote Volume (USDT)
// Filters out stablecoins and non-USDT pairs
func (c *BinanceClient) GetTopVolumeCoins(ctx context.Context, limit int) ([]string, error) {
	tickers, err := c.Get24hTicker(ctx)
	if err != nil {
		return nil, err
	}

	// Filter and sort
	var candidates []Ticker24h
	exclude := map[string]bool{
		"USDCUSDT": true, "FDUSDUSDT": true, "TUSDUSDT": true, "USDPUSDT": true, // Stablecoiins
		"BTCUSDT": false, "ETHUSDT": false, // Keep majors? Usually strategies might want to filter them or keep them. Keeping for now.
	}

	for _, t := range tickers {
		// Must be USDT pair
		if !strings.HasSuffix(t.Symbol, "USDT") {
			continue
		}

		// Skip ignored symbols
		if exclude[t.Symbol] {
			continue
		}

		candidates = append(candidates, t)
	}

	// Sort by Quote Volume (Desc)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].QuoteVolume > candidates[j].QuoteVolume
	})

	// Take top N
	if limit > len(candidates) {
		limit = len(candidates)
	}

	result := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		result = append(result, candidates[i].Symbol)
	}

	return result, nil
}

// IsActiveSymbol checks if a symbol is currently trading
func (c *BinanceClient) IsActiveSymbol(symbol string) bool {
	if info, ok := c.symbolInfo[symbol]; ok {
		return info.Status == "TRADING"
	}
	return false
}

// IsRecentlyListed returns true if the symbol was listed within the last maxAgeDays days.
// Always returns true when maxAgeDays is 0 (filter disabled) or the symbol is not found.
func (c *BinanceClient) IsRecentlyListed(symbol string, maxAgeDays int) bool {
	if maxAgeDays <= 0 {
		return true
	}
	info, ok := c.symbolInfo[symbol]
	if !ok || info.OnboardDate == 0 {
		return true // unknown listing date: allow it
	}
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays).UnixMilli()
	return info.OnboardDate >= cutoff
}
