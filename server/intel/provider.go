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

// Provider fetches and caches market intelligence data
type Provider struct {
	config ProviderConfig

	// Cached data with timestamps
	mu              sync.RWMutex
	fearGreedCache  *FearGreedData
	fearGreedTime   time.Time
	newsCache       []NewsItem
	newsTime        time.Time
	coinDataCache   map[string]*CoinInfo
	coinDataTime    time.Time
	coinDataSymbols []string // symbols used for last fetch
	globalCache     *GlobalMarketData
	globalTime      time.Time
}

// NewProvider creates a new intel provider
func NewProvider(config ProviderConfig) *Provider {
	return &Provider{
		config:        config,
		coinDataCache: make(map[string]*CoinInfo),
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

// getNews returns cached or fresh news
func (p *Provider) getNews(ctx context.Context, symbols []string) ([]NewsItem, error) {
	p.mu.RLock()
	if p.newsCache != nil && time.Since(p.newsTime) < p.config.NewsCacheDuration {
		cached := p.newsCache
		p.mu.RUnlock()
		// Filter for current symbols
		filtered := FilterNewsForSymbols(cached, symbols)
		if len(filtered) > 0 {
			return filtered, nil
		}
		return cached, nil
	}
	p.mu.RUnlock()

	// Construct search query from symbols for better relevance
	query := "cryptocurrency trading market"
	if len(symbols) > 0 {
		cleanSymbols := make([]string, 0)
		for _, s := range symbols {
			// Strip USDT
			clean := strings.Replace(s, "USDT", "", 1)
			cleanSymbols = append(cleanSymbols, clean)
		}
		// "BTC ETH SOL crypto news"
		query = strings.Join(cleanSymbols, " ") + " crypto news"
	}

	// Fetch fresh data
	news, err := FetchNews(ctx, query, 20)
	if err != nil {
		p.mu.RLock()
		if p.newsCache != nil {
			cached := p.newsCache
			p.mu.RUnlock()
			return FilterNewsForSymbols(cached, symbols), nil
		}
		p.mu.RUnlock()
		return nil, err
	}

	// Update cache
	p.mu.Lock()
	p.newsCache = news
	p.newsTime = time.Now()
	p.mu.Unlock()

	// Filter for requested symbols
	filtered := FilterNewsForSymbols(news, symbols)
	if len(filtered) > 0 {
		return filtered, nil
	}
	return news, nil
}

// getCoinData returns cached or fresh coin data
func (p *Provider) getCoinData(ctx context.Context, symbols []string) (map[string]*CoinInfo, error) {
	// Check if we need to refresh (new symbols or cache expired)
	p.mu.RLock()
	needRefresh := time.Since(p.coinDataTime) >= p.config.CoinDataCacheDuration
	if !needRefresh {
		// Check if all requested symbols are in cache
		for _, symbol := range symbols {
			cgID := GetCoinGeckoID(symbol)
			if cgID != "" {
				if _, ok := p.coinDataCache[cgID]; !ok {
					needRefresh = true
					break
				}
			}
		}
	}

	if !needRefresh && len(p.coinDataCache) > 0 {
		cached := make(map[string]*CoinInfo)
		for k, v := range p.coinDataCache {
			cached[k] = v
		}
		p.mu.RUnlock()
		return cached, nil
	}
	p.mu.RUnlock()

	// Get CoinGecko IDs for symbols
	var coinIDs []string
	for _, symbol := range symbols {
		if id := GetCoinGeckoID(symbol); id != "" {
			coinIDs = append(coinIDs, id)
		}
	}

	if len(coinIDs) == 0 {
		return nil, nil
	}

	// Fetch fresh data
	coinData, err := FetchCoinData(ctx, coinIDs)
	if err != nil {
		// Return cached if available
		p.mu.RLock()
		if len(p.coinDataCache) > 0 {
			cached := make(map[string]*CoinInfo)
			for k, v := range p.coinDataCache {
				cached[k] = v
			}
			p.mu.RUnlock()
			return cached, nil
		}
		p.mu.RUnlock()
		return nil, err
	}

	// Update cache
	p.mu.Lock()
	p.coinDataCache = coinData
	p.coinDataTime = time.Now()
	p.coinDataSymbols = symbols
	p.mu.Unlock()

	return coinData, nil
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
	p.newsCache = nil
	p.coinDataCache = make(map[string]*CoinInfo)
	p.globalCache = nil
}
