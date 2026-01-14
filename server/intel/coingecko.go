package intel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	coingeckoBaseURL    = "https://api.coingecko.com/api/v3"
	coingeckoMarketsURL = coingeckoBaseURL + "/coins/markets"
	coingeckoGlobalURL  = coingeckoBaseURL + "/global"
	coingeckoSearchURL  = coingeckoBaseURL + "/search"
)

// coingeckoMarketResponse represents coin market data from CoinGecko
type coingeckoMarketResponse struct {
	ID                                 string  `json:"id"`
	Symbol                             string  `json:"symbol"`
	Name                               string  `json:"name"`
	CurrentPrice                       float64 `json:"current_price"`
	MarketCap                          float64 `json:"market_cap"`
	MarketCapRank                      int     `json:"market_cap_rank"`
	TotalVolume                        float64 `json:"total_volume"`
	High24h                            float64 `json:"high_24h"`
	Low24h                             float64 `json:"low_24h"`
	PriceChange24h                     float64 `json:"price_change_24h"`
	PriceChangePercentage24h           float64 `json:"price_change_percentage_24h"`
	PriceChangePercentage7dInCurrency  float64 `json:"price_change_percentage_7d_in_currency"`
	PriceChangePercentage30dInCurrency float64 `json:"price_change_percentage_30d_in_currency"`
	ATH                                float64 `json:"ath"`
	ATHChangePercentage                float64 `json:"ath_change_percentage"`
	ATHDate                            string  `json:"ath_date"`
	ATL                                float64 `json:"atl"`
	ATLChangePercentage                float64 `json:"atl_change_percentage"`
	CirculatingSupply                  float64 `json:"circulating_supply"`
	TotalSupply                        float64 `json:"total_supply"`
	LastUpdated                        string  `json:"last_updated"`
}

// coingeckoGlobalResponse represents global market data
type coingeckoGlobalResponse struct {
	Data struct {
		TotalMarketCap               map[string]float64 `json:"total_market_cap"`
		TotalVolume                  map[string]float64 `json:"total_volume"`
		MarketCapPercentage          map[string]float64 `json:"market_cap_percentage"`
		MarketCapChangePercentage24h float64            `json:"market_cap_change_percentage_24h_usd"`
		ActiveCryptocurrencies       int                `json:"active_cryptocurrencies"`
	} `json:"data"`
}

// coingeckoSearchResponse represents the search API response
type coingeckoSearchResponse struct {
	Coins []struct {
		ID            string `json:"id"`
		Symbol        string `json:"symbol"`
		Name          string `json:"name"`
		MarketCapRank int    `json:"market_cap_rank"`
	} `json:"coins"`
}

// FetchCoinData fetches market data for specific coins from CoinGecko
func FetchCoinData(ctx context.Context, coinIDs []string) (map[string]*CoinInfo, error) {
	if len(coinIDs) == 0 {
		return nil, nil
	}

	// Build URL with coin IDs
	url := fmt.Sprintf("%s?vs_currency=usd&ids=%s&order=market_cap_desc&sparkline=false&price_change_percentage=7d,30d",
		coingeckoMarketsURL, strings.Join(coinIDs, ","))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch coin data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("CoinGecko rate limit exceeded")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CoinGecko API returned status %d", resp.StatusCode)
	}

	var results []coingeckoMarketResponse
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	coinData := make(map[string]*CoinInfo)
	for _, r := range results {
		athDate, _ := time.Parse(time.RFC3339, r.ATHDate)
		lastUpdated, _ := time.Parse(time.RFC3339, r.LastUpdated)

		coinData[r.ID] = &CoinInfo{
			ID:                r.ID,
			Symbol:            r.Symbol,
			Name:              r.Name,
			CurrentPrice:      r.CurrentPrice,
			MarketCap:         r.MarketCap,
			MarketCapRank:     r.MarketCapRank,
			TotalVolume:       r.TotalVolume,
			High24h:           r.High24h,
			Low24h:            r.Low24h,
			PriceChange24h:    r.PriceChange24h,
			PriceChangePct24h: r.PriceChangePercentage24h,
			PriceChangePct7d:  r.PriceChangePercentage7dInCurrency,
			PriceChangePct30d: r.PriceChangePercentage30dInCurrency,
			ATH:               r.ATH,
			ATHChangePct:      r.ATHChangePercentage,
			ATHDate:           athDate,
			ATL:               r.ATL,
			ATLChangePct:      r.ATLChangePercentage,
			CirculatingSupply: r.CirculatingSupply,
			TotalSupply:       r.TotalSupply,
			LastUpdated:       lastUpdated,
		}
	}

	return coinData, nil
}

// FetchGlobalData fetches global crypto market data
func FetchGlobalData(ctx context.Context) (*GlobalMarketData, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", coingeckoGlobalURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch global data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("CoinGecko rate limit exceeded")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CoinGecko API returned status %d", resp.StatusCode)
	}

	var result coingeckoGlobalResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &GlobalMarketData{
		TotalMarketCap:         result.Data.TotalMarketCap["usd"],
		TotalVolume:            result.Data.TotalVolume["usd"],
		BTCDominance:           result.Data.MarketCapPercentage["btc"],
		ETHDominance:           result.Data.MarketCapPercentage["eth"],
		MarketCapChangePct24h:  result.Data.MarketCapChangePercentage24h,
		ActiveCryptocurrencies: result.Data.ActiveCryptocurrencies,
	}, nil
}

// FormatCoinData formats coin data for AI consumption
func FormatCoinData(coinData map[string]*CoinInfo, symbols []string) string {
	if len(coinData) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Live Coin Data (Source: CoinGecko)\n\n")

	for _, symbol := range symbols {
		cgID := GetCoinGeckoID(symbol)
		if cgID == "" {
			continue
		}

		coin, ok := coinData[cgID]
		if !ok {
			continue
		}

		// ATH analysis
		athWarning := ""
		if coin.ATHChangePct > -10 {
			athWarning = " [NEAR ATH - Caution!]"
		} else if coin.ATHChangePct < -80 {
			athWarning = " [>80% from ATH - Potential value]"
		} else if coin.ATHChangePct < -50 {
			athWarning = " [50%+ from ATH]"
		}

		// 7d/30d momentum
		momentum := "NEUTRAL"
		if coin.PriceChangePct7d > 10 && coin.PriceChangePct30d > 20 {
			momentum = "STRONG BULLISH"
		} else if coin.PriceChangePct7d > 5 {
			momentum = "BULLISH"
		} else if coin.PriceChangePct7d < -10 && coin.PriceChangePct30d < -20 {
			momentum = "STRONG BEARISH"
		} else if coin.PriceChangePct7d < -5 {
			momentum = "BEARISH"
		}

		sb.WriteString(fmt.Sprintf("### %s (%s)\n", strings.ToUpper(coin.Symbol), coin.Name))
		sb.WriteString(fmt.Sprintf("- Market Cap Rank: #%d\n", coin.MarketCapRank))
		sb.WriteString(fmt.Sprintf("- Market Cap: $%.0fM\n", coin.MarketCap/1_000_000))
		sb.WriteString(fmt.Sprintf("- 24h Volume: $%.0fM\n", coin.TotalVolume/1_000_000))
		sb.WriteString(fmt.Sprintf("- 7d Change: %.2f%% | 30d Change: %.2f%% [%s]\n",
			coin.PriceChangePct7d, coin.PriceChangePct30d, momentum))
		sb.WriteString(fmt.Sprintf("- ATH: $%.2f (%.1f%% from ATH)%s\n",
			coin.ATH, coin.ATHChangePct, athWarning))
		sb.WriteString("\n")
	}

	return sb.String()
}

// FormatGlobalData formats global market data for AI consumption
func FormatGlobalData(global *GlobalMarketData) string {
	if global == nil {
		return ""
	}

	marketStatus := "NEUTRAL"
	if global.MarketCapChangePct24h > 2 {
		marketStatus = "BULLISH - Market expanding"
	} else if global.MarketCapChangePct24h < -2 {
		marketStatus = "BEARISH - Market contracting"
	}

	btcDomWarning := ""
	if global.BTCDominance > 55 {
		btcDomWarning = " [High BTC dominance - Altcoins may lag]"
	} else if global.BTCDominance < 40 {
		btcDomWarning = " [Low BTC dominance - Alt season possible]"
	}

	return fmt.Sprintf(`## Global Crypto Market (Source: CoinGecko Global)
- Total Market Cap: $%.0fB (%.2f%% 24h)
- 24h Volume: $%.0fB
- BTC Dominance: %.1f%%%s
- ETH Dominance: %.1f%%
- Market Status: %s

`, global.TotalMarketCap/1_000_000_000, global.MarketCapChangePct24h,
		global.TotalVolume/1_000_000_000, global.BTCDominance, btcDomWarning,
		global.ETHDominance, marketStatus)
}

// SearchCoinID searches CoinGecko for a coin by symbol and returns its ID
// Returns empty string if not found
func SearchCoinID(ctx context.Context, symbol string) (string, error) {
	// Clean symbol (remove USDT suffix if present)
	cleanSymbol := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")
	if cleanSymbol == "" {
		return "", nil
	}

	url := fmt.Sprintf("%s?query=%s", coingeckoSearchURL, strings.ToLower(cleanSymbol))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to search coin: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("CoinGecko rate limit exceeded")
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("CoinGecko search returned status %d", resp.StatusCode)
	}

	var result coingeckoSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	// Find best match - prefer exact symbol match with highest market cap rank
	cleanSymbolLower := strings.ToLower(cleanSymbol)
	for _, coin := range result.Coins {
		if strings.ToLower(coin.Symbol) == cleanSymbolLower {
			return coin.ID, nil
		}
	}

	// No exact match found
	return "", nil
}
