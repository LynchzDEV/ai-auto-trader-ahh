package coinank

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	BaseURL        = "https://open-api-v3.coinglass.com"
	DefaultTimeout = 10 * time.Second
)

// Client is the CoinAnk/Coinglass API client
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

// NewClient creates a new CoinAnk client
func NewClient(apiKey string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		baseURL: BaseURL,
		apiKey:  apiKey,
	}
}

// Response is the generic API response wrapper
type Response[T any] struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Data    T      `json:"data"`
	Success bool   `json:"success"`
}

// Get makes a GET request to the API
func (c *Client) Get(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Add API key header
	if c.apiKey != "" {
		req.Header.Set("CG-API-KEY", c.apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: status=%d body=%s", resp.StatusCode, string(body))
	}

	return body, nil
}

// OIData represents Open Interest data for a symbol
type OIData struct {
	Symbol           string  `json:"symbol"`
	OpenInterest     float64 `json:"openInterest"`     // Total OI value in USD
	OpenInterestCoin float64 `json:"openInterestCoin"` // OI in coin amount
	Change1H         float64 `json:"h1OiChangePercent"`
	Change4H         float64 `json:"h4OiChangePercent"`
	Change24H        float64 `json:"h24OiChangePercent"`
	Price            float64 `json:"price"`
	PriceChange24H   float64 `json:"priceChangePercent"`
}

// GetOpenInterest fetches Open Interest data for a symbol
func (c *Client) GetOpenInterest(ctx context.Context, symbol string) (*OIData, error) {
	// Extract base coin from symbol (e.g., BTCUSDT -> BTC)
	baseCoin := symbol
	if len(symbol) > 4 && symbol[len(symbol)-4:] == "USDT" {
		baseCoin = symbol[:len(symbol)-4]
	}

	params := map[string]string{
		"symbol": baseCoin,
	}

	resp, err := c.Get(ctx, "/api/futures/openInterest/ohlc-aggregated-history", params)
	if err != nil {
		return nil, err
	}

	var result Response[[]OIHistoryItem]
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if result.Code != 0 || len(result.Data) == 0 {
		// Fallback to simpler endpoint
		return c.getOISimple(ctx, baseCoin)
	}

	// Get latest data point
	latest := result.Data[len(result.Data)-1]

	// Calculate changes from historical data
	change1H := 0.0
	change4H := 0.0
	change24H := 0.0

	if len(result.Data) >= 2 {
		prev := result.Data[len(result.Data)-2]
		if prev.OpenInterest > 0 {
			change1H = ((latest.OpenInterest - prev.OpenInterest) / prev.OpenInterest) * 100
		}
	}
	if len(result.Data) >= 5 {
		prev := result.Data[len(result.Data)-5]
		if prev.OpenInterest > 0 {
			change4H = ((latest.OpenInterest - prev.OpenInterest) / prev.OpenInterest) * 100
		}
	}
	if len(result.Data) >= 25 {
		prev := result.Data[len(result.Data)-25]
		if prev.OpenInterest > 0 {
			change24H = ((latest.OpenInterest - prev.OpenInterest) / prev.OpenInterest) * 100
		}
	}

	return &OIData{
		Symbol:       symbol,
		OpenInterest: latest.OpenInterest,
		Change1H:     change1H,
		Change4H:     change4H,
		Change24H:    change24H,
	}, nil
}

// OIHistoryItem represents a single data point in OI history
type OIHistoryItem struct {
	Timestamp    int64   `json:"t"`
	OpenInterest float64 `json:"o"` // Open
	High         float64 `json:"h"`
	Low          float64 `json:"l"`
	Close        float64 `json:"c"`
}

// getOISimple is a fallback method using a simpler API
func (c *Client) getOISimple(ctx context.Context, baseCoin string) (*OIData, error) {
	params := map[string]string{
		"symbol": baseCoin,
	}

	resp, err := c.Get(ctx, "/api/futures/openInterest/aggregated-ohlc", params)
	if err != nil {
		return nil, err
	}

	var result Response[OIAggregated]
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &OIData{
		Symbol:       baseCoin + "USDT",
		OpenInterest: result.Data.OpenInterest,
		Change1H:     result.Data.Change1H,
		Change4H:     result.Data.Change4H,
		Change24H:    result.Data.Change24H,
	}, nil
}

// OIAggregated represents aggregated OI data
type OIAggregated struct {
	OpenInterest float64 `json:"openInterest"`
	Change1H     float64 `json:"h1OiChangePercent"`
	Change4H     float64 `json:"h4OiChangePercent"`
	Change24H    float64 `json:"h24OiChangePercent"`
}

// OIRankItem represents a coin in the OI ranking
type OIRankItem struct {
	Symbol         string  `json:"symbol"`
	OpenInterest   float64 `json:"openInterest"`
	OIChange1H     float64 `json:"h1OiChangePercent"`
	OIChange4H     float64 `json:"h4OiChangePercent"`
	OIChange24H    float64 `json:"h24OiChangePercent"`
	Price          float64 `json:"price"`
	PriceChange24H float64 `json:"priceChangePercent"`
	Volume24H      float64 `json:"turnover24h"`
	LongShortRatio float64 `json:"longShortRatio"`
}

// GetOIRanking fetches the top coins by OI change
func (c *Client) GetOIRanking(ctx context.Context, limit int) ([]OIRankItem, error) {
	if limit <= 0 {
		limit = 20
	}

	params := map[string]string{
		"size": fmt.Sprintf("%d", limit),
	}

	resp, err := c.Get(ctx, "/api/futures/openInterest/tops", params)
	if err != nil {
		return nil, err
	}

	var result Response[[]OIRankItem]
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d msg=%s", result.Code, result.Msg)
	}

	return result.Data, nil
}

// LiquidationData represents liquidation statistics
type LiquidationData struct {
	Symbol           string  `json:"symbol"`
	LongLiquidation  float64 `json:"longLiquidation"`  // USD value of long liquidations
	ShortLiquidation float64 `json:"shortLiquidation"` // USD value of short liquidations
	TotalLiquidation float64 `json:"totalLiquidation"`
	LongRatio        float64 `json:"longRatio"`  // % of liquidations that were longs
	ShortRatio       float64 `json:"shortRatio"` // % of liquidations that were shorts
}

// GetLiquidation fetches recent liquidation data for a symbol
func (c *Client) GetLiquidation(ctx context.Context, symbol string) (*LiquidationData, error) {
	baseCoin := symbol
	if len(symbol) > 4 && symbol[len(symbol)-4:] == "USDT" {
		baseCoin = symbol[:len(symbol)-4]
	}

	params := map[string]string{
		"symbol": baseCoin,
	}

	resp, err := c.Get(ctx, "/api/futures/liquidation/aggregated", params)
	if err != nil {
		return nil, err
	}

	var result Response[LiquidationAggregated]
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d msg=%s", result.Code, result.Msg)
	}

	total := result.Data.H24LongLiquidation + result.Data.H24ShortLiquidation
	longRatio := 0.0
	shortRatio := 0.0
	if total > 0 {
		longRatio = (result.Data.H24LongLiquidation / total) * 100
		shortRatio = (result.Data.H24ShortLiquidation / total) * 100
	}

	return &LiquidationData{
		Symbol:           symbol,
		LongLiquidation:  result.Data.H24LongLiquidation,
		ShortLiquidation: result.Data.H24ShortLiquidation,
		TotalLiquidation: total,
		LongRatio:        longRatio,
		ShortRatio:       shortRatio,
	}, nil
}

// LiquidationAggregated represents the API response for liquidation data
type LiquidationAggregated struct {
	H1LongLiquidation   float64 `json:"h1LongLiquidation"`
	H1ShortLiquidation  float64 `json:"h1ShortLiquidation"`
	H4LongLiquidation   float64 `json:"h4LongLiquidation"`
	H4ShortLiquidation  float64 `json:"h4ShortLiquidation"`
	H24LongLiquidation  float64 `json:"h24LongLiquidation"`
	H24ShortLiquidation float64 `json:"h24ShortLiquidation"`
}

// LongShortRatioData represents long/short ratio data
type LongShortRatioData struct {
	Symbol     string  `json:"symbol"`
	LongRatio  float64 `json:"longRatio"`  // % of traders long
	ShortRatio float64 `json:"shortRatio"` // % of traders short
	LongShort  float64 `json:"longShort"`  // Long/Short ratio (>1 = more longs)
}

// GetLongShortRatio fetches long/short ratio for a symbol
func (c *Client) GetLongShortRatio(ctx context.Context, symbol string) (*LongShortRatioData, error) {
	baseCoin := symbol
	if len(symbol) > 4 && symbol[len(symbol)-4:] == "USDT" {
		baseCoin = symbol[:len(symbol)-4]
	}

	params := map[string]string{
		"symbol": baseCoin,
	}

	resp, err := c.Get(ctx, "/api/futures/globalLongShortAccountRatio/chart", params)
	if err != nil {
		return nil, err
	}

	var result Response[[]LongShortHistoryItem]
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if result.Code != 0 || len(result.Data) == 0 {
		return &LongShortRatioData{
			Symbol:     symbol,
			LongRatio:  50,
			ShortRatio: 50,
			LongShort:  1.0,
		}, nil
	}

	// Get latest data point
	latest := result.Data[len(result.Data)-1]

	return &LongShortRatioData{
		Symbol:     symbol,
		LongRatio:  latest.LongRate * 100,
		ShortRatio: latest.ShortRate * 100,
		LongShort:  latest.LongShortRatio,
	}, nil
}

// LongShortHistoryItem represents a data point in long/short history
type LongShortHistoryItem struct {
	Timestamp      int64   `json:"t"`
	LongRate       float64 `json:"longRate"`
	ShortRate      float64 `json:"shortRate"`
	LongShortRatio float64 `json:"longShortRatio"`
}
