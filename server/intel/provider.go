package intel

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// ProviderConfig configures the intel provider
type ProviderConfig struct {
	// Cache durations
	FearGreedCacheDuration  time.Duration // Default: 30 min (updates daily anyway)
	NewsCacheDuration       time.Duration // Default: 15 min
	CoinDataCacheDuration   time.Duration // Default: 15 min
	GlobalDataCacheDuration time.Duration // Default: 15 min

	// Limits
	MaxNewsItems int // Default: 10

	// Feature toggles
	EnableFearGreed  bool
	EnableNews       bool
	EnableCoinData   bool
	EnableGlobalData bool
}

// DefaultConfig returns default provider configuration
func DefaultConfig() ProviderConfig {
	return ProviderConfig{
		FearGreedCacheDuration:  30 * time.Minute,
		NewsCacheDuration:       15 * time.Minute,
		CoinDataCacheDuration:   15 * time.Minute,
		GlobalDataCacheDuration: 15 * time.Minute,
		MaxNewsItems:            10,
		EnableFearGreed:         true,
		EnableNews:              true,
		EnableCoinData:          true,
		EnableGlobalData:        true,
	}
}

// coinCacheEntry holds cached coin data with its own timestamp
type coinCacheEntry struct {
	data      *CoinInfo
	fetchedAt time.Time
}

// newsCacheEntry holds cached news for a symbol with its own timestamp
type newsCacheEntry struct {
	items     []NewsItem
	fetchedAt time.Time
}

// Provider fetches and caches market intelligence data
type Provider struct {
	config ProviderConfig

	// Cached data with timestamps
	mu             sync.RWMutex
	fearGreedCache *FearGreedData
	fearGreedTime  time.Time
	// Per-symbol caches
	newsCache     map[string]*newsCacheEntry // key: symbol (e.g., "BTC")
	coinDataCache map[string]*coinCacheEntry // key: coingecko ID (e.g., "bitcoin")
	// Dynamic symbol-to-CoinGecko ID mapping (permanent cache)
	symbolIDCache map[string]string // key: symbol (e.g., "IPUSDT"), value: coingecko ID or "" if not found
	// Global cache (applies to all pairs)
	globalCache *GlobalMarketData
	globalTime  time.Time
}

// NewProvider creates a new intel provider
func NewProvider(config ProviderConfig) *Provider {
	return &Provider{
		config:        config,
		newsCache:     make(map[string]*newsCacheEntry),
		coinDataCache: make(map[string]*coinCacheEntry),
		symbolIDCache: make(map[string]string),
	}
}

// GetMarketIntel fetches all market intelligence for the given symbols
// Uses caching to avoid hitting rate limits
func (p *Provider) GetMarketIntel(ctx context.Context, symbols []string) (*MarketIntel, error) {
	intel := &MarketIntel{
		FetchedAt: time.Now(),
		CoinData:  make(map[string]*CoinInfo),
	}

	var wg sync.WaitGroup
	var errs []error
	var errMu sync.Mutex

	// Fetch Fear & Greed
	if p.config.EnableFearGreed {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fg, err := p.getFearGreed(ctx)
			if err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("fear & greed: %w", err))
				errMu.Unlock()
				log.Printf("[Intel] Fear & Greed fetch error: %v", err)
			} else {
				p.mu.Lock()
				intel.FearGreed = fg
				p.mu.Unlock()
			}
		}()
	}

	// Fetch News
	if p.config.EnableNews {
		wg.Add(1)
		go func() {
			defer wg.Done()
			news, err := p.getNews(ctx, symbols)
			if err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("news: %w", err))
				errMu.Unlock()
				log.Printf("[Intel] News fetch error: %v", err)
			} else {
				p.mu.Lock()
				intel.News = news
				p.mu.Unlock()
			}
		}()
	}

	// Fetch Coin Data
	if p.config.EnableCoinData {
		wg.Add(1)
		go func() {
			defer wg.Done()
			coinData, err := p.getCoinData(ctx, symbols)
			if err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("coin data: %w", err))
				errMu.Unlock()
				log.Printf("[Intel] CoinGecko fetch error: %v", err)
			} else {
				p.mu.Lock()
				intel.CoinData = coinData
				p.mu.Unlock()
			}
		}()
	}

	// Fetch Global Data
	if p.config.EnableGlobalData {
		wg.Add(1)
		go func() {
			defer wg.Done()
			global, err := p.getGlobalData(ctx)
			if err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("global data: %w", err))
				errMu.Unlock()
				log.Printf("[Intel] Global data fetch error: %v", err)
			} else {
				p.mu.Lock()
				intel.GlobalData = global
				p.mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Return intel even with partial errors (cached data may be used)
	return intel, nil
}

// getFearGreed returns cached or fresh Fear & Greed data
func (p *Provider) getFearGreed(ctx context.Context) (*FearGreedData, error) {
	p.mu.RLock()
	if p.fearGreedCache != nil && time.Since(p.fearGreedTime) < p.config.FearGreedCacheDuration {
		cached := p.fearGreedCache
		p.mu.RUnlock()
		return cached, nil
	}
	p.mu.RUnlock()

	// Fetch fresh data
	fg, err := FetchFearGreed(ctx)
	if err != nil {
		// Return cached if available
		p.mu.RLock()
		if p.fearGreedCache != nil {
			cached := p.fearGreedCache
			p.mu.RUnlock()
			return cached, nil
		}
		p.mu.RUnlock()
		return nil, err
	}

	// Update cache
	p.mu.Lock()
	p.fearGreedCache = fg
	p.fearGreedTime = time.Now()
	p.mu.Unlock()

	return fg, nil
}

// getNews returns cached or fresh news for the given symbols
// Uses per-symbol caching to avoid returning unrelated news
func (p *Provider) getNews(ctx context.Context, symbols []string) ([]NewsItem, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	// Extract base currencies from symbols
	baseCurrencies := make([]string, 0, len(symbols))
	for _, s := range symbols {
		base := strings.TrimSuffix(s, "USDT")
		if base != "" {
			baseCurrencies = append(baseCurrencies, base)
		}
	}

	if len(baseCurrencies) == 0 {
		return nil, nil
	}

	// Collect news from per-symbol cache or fetch fresh
	var allNews []NewsItem
	var symbolsToFetch []string

	p.mu.RLock()
	for _, base := range baseCurrencies {
		if entry, ok := p.newsCache[base]; ok && time.Since(entry.fetchedAt) < p.config.NewsCacheDuration {
			allNews = append(allNews, entry.items...)
		} else {
			symbolsToFetch = append(symbolsToFetch, base)
		}
	}
	p.mu.RUnlock()

	// Fetch news for symbols not in cache
	for _, base := range symbolsToFetch {
		// Query specifically for this symbol
		query := base + " cryptocurrency crypto news"
		news, err := FetchNews(ctx, query, 10)
		if err != nil {
			// Try to use stale cache if available
			p.mu.RLock()
			if entry, ok := p.newsCache[base]; ok {
				allNews = append(allNews, entry.items...)
			}
			p.mu.RUnlock()
			continue
		}

		// Filter to only include news actually mentioning this symbol
		filtered := FilterNewsForSymbols(news, []string{base + "USDT"})
		if len(filtered) == 0 {
			// If no filtered results, take top 3 from the query results
			if len(news) > 3 {
				filtered = news[:3]
			} else {
				filtered = news
			}
		}

		// Update per-symbol cache
		p.mu.Lock()
		p.newsCache[base] = &newsCacheEntry{
			items:     filtered,
			fetchedAt: time.Now(),
		}
		p.mu.Unlock()

		allNews = append(allNews, filtered...)
	}

	// Deduplicate news by title
	seen := make(map[string]bool)
	dedupedNews := make([]NewsItem, 0, len(allNews))
	for _, item := range allNews {
		if !seen[item.Title] {
			seen[item.Title] = true
			dedupedNews = append(dedupedNews, item)
		}
	}

	return dedupedNews, nil
}

// getCoinGeckoID returns the CoinGecko ID for a symbol, using dynamic lookup if needed
func (p *Provider) getCoinGeckoID(ctx context.Context, symbol string) string {
	// First check hardcoded mapping
	if id := GetCoinGeckoID(symbol); id != "" {
		return id
	}

	// Check dynamic cache
	p.mu.RLock()
	if id, ok := p.symbolIDCache[symbol]; ok {
		p.mu.RUnlock()
		return id // Returns "" if we already tried and failed
	}
	p.mu.RUnlock()

	// Dynamic lookup via CoinGecko search API
	id, err := SearchCoinID(ctx, symbol)
	if err != nil {
		log.Printf("[Intel] Failed to search CoinGecko for %s: %v", symbol, err)
		// Don't cache failures from network errors - we'll retry next time
		return ""
	}

	// Cache result (even empty string to avoid repeated lookups)
	p.mu.Lock()
	p.symbolIDCache[symbol] = id
	p.mu.Unlock()

	if id != "" {
		log.Printf("[Intel] Discovered CoinGecko ID for %s: %s", symbol, id)
	} else {
		log.Printf("[Intel] No CoinGecko ID found for %s", symbol)
	}

	return id
}

// getCoinData returns cached or fresh coin data for the given symbols
// Uses per-symbol caching to only return data for requested symbols
func (p *Provider) getCoinData(ctx context.Context, symbols []string) (map[string]*CoinInfo, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	result := make(map[string]*CoinInfo)
	var idsToFetch []string

	// Check per-symbol cache, using dynamic lookup for unknown symbols
	for _, symbol := range symbols {
		cgID := p.getCoinGeckoID(ctx, symbol)
		if cgID == "" {
			continue
		}

		p.mu.RLock()
		if entry, ok := p.coinDataCache[cgID]; ok && time.Since(entry.fetchedAt) < p.config.CoinDataCacheDuration {
			result[cgID] = entry.data
			p.mu.RUnlock()
		} else {
			p.mu.RUnlock()
			idsToFetch = append(idsToFetch, cgID)
		}
	}

	// If all symbols are cached, return early
	if len(idsToFetch) == 0 {
		return result, nil
	}

	// Fetch missing symbols from API
	coinData, err := FetchCoinData(ctx, idsToFetch)
	if err != nil {
		// Return stale cache if available
		p.mu.RLock()
		for _, id := range idsToFetch {
			if entry, ok := p.coinDataCache[id]; ok {
				result[id] = entry.data
			}
		}
		p.mu.RUnlock()

		// If we got some cached data, return it without error
		if len(result) > 0 {
			return result, nil
		}
		return nil, err
	}

	// Update per-symbol cache and result
	p.mu.Lock()
	for id, data := range coinData {
		p.coinDataCache[id] = &coinCacheEntry{
			data:      data,
			fetchedAt: time.Now(),
		}
		result[id] = data
	}
	p.mu.Unlock()

	return result, nil
}

// getGlobalData returns cached or fresh global market data
func (p *Provider) getGlobalData(ctx context.Context) (*GlobalMarketData, error) {
	p.mu.RLock()
	if p.globalCache != nil && time.Since(p.globalTime) < p.config.GlobalDataCacheDuration {
		cached := p.globalCache
		p.mu.RUnlock()
		return cached, nil
	}
	p.mu.RUnlock()

	// Fetch fresh data
	global, err := FetchGlobalData(ctx)
	if err != nil {
		p.mu.RLock()
		if p.globalCache != nil {
			cached := p.globalCache
			p.mu.RUnlock()
			return cached, nil
		}
		p.mu.RUnlock()
		return nil, err
	}

	// Update cache
	p.mu.Lock()
	p.globalCache = global
	p.globalTime = time.Now()
	p.mu.Unlock()

	return global, nil
}

// FormatForAI formats all market intelligence for AI consumption
func FormatForAI(intel *MarketIntel, symbols []string, maxNewsItems int) string {
	if intel == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n>>> SEARCHING WEB FOR MARKET INTELLIGENCE...\n")
	sb.WriteString(">>> FOUND RELEVANT LIVE DATA:\n\n")

	// Global market data first
	if intel.GlobalData != nil {
		sb.WriteString(FormatGlobalData(intel.GlobalData))
	}

	// Fear & Greed
	if intel.FearGreed != nil {
		sb.WriteString(FormatFearGreed(intel.FearGreed))
	}

	// Coin fundamental data
	if len(intel.CoinData) > 0 {
		sb.WriteString(FormatCoinData(intel.CoinData, symbols))
	}

	// News
	if len(intel.News) > 0 {
		sb.WriteString(FormatNews(intel.News, maxNewsItems))
	}

	sb.WriteString("---\n\n")

	return sb.String()
}

// ClearCache clears all cached data
func (p *Provider) ClearCache() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.fearGreedCache = nil
	p.newsCache = make(map[string]*newsCacheEntry)
	p.coinDataCache = make(map[string]*coinCacheEntry)
	p.globalCache = nil
}
