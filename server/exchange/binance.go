package exchange

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	BinanceMainnetURL = "https://fapi.binance.com"
	BinanceTestnetURL = "https://testnet.binancefuture.com"
)

type BinanceClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
}

type AccountInfo struct {
	TotalWalletBalance    float64 `json:"totalWalletBalance,string"`
	AvailableBalance      float64 `json:"availableBalance,string"`
	TotalUnrealizedProfit float64 `json:"totalUnrealizedProfit,string"`
	TotalMarginBalance    float64 `json:"totalMarginBalance,string"`
}

type Position struct {
	Symbol           string  `json:"symbol"`
	PositionAmt      float64 `json:"positionAmt,string"`
	EntryPrice       float64 `json:"entryPrice,string"`
	UnrealizedProfit float64 `json:"unrealizedProfit,string"`
	Leverage         int     `json:"leverage,string"`
	PositionSide     string  `json:"positionSide"`
	MarkPrice        float64 `json:"markPrice,string"`
}

type Order struct {
	OrderID       int64   `json:"orderId"`
	Symbol        string  `json:"symbol"`
	Status        string  `json:"status"`
	Side          string  `json:"side"`
	PositionSide  string  `json:"positionSide"`
	Type          string  `json:"type"`
	Price         float64 `json:"price,string"`
	AvgPrice      float64 `json:"avgPrice,string"`
	OrigQty       float64 `json:"origQty,string"`
	ExecutedQty   float64 `json:"executedQty,string"`
	Time          int64   `json:"time"`
	UpdateTime    int64   `json:"updateTime"`
}

type Ticker struct {
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price,string"`
	Time      int64   `json:"time"`
}

type Kline struct {
	OpenTime  int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	CloseTime int64
}

func NewBinanceClient(apiKey, secretKey string, testnet bool) *BinanceClient {
	baseURL := BinanceMainnetURL
	if testnet {
		baseURL = BinanceTestnetURL
	}

	return &BinanceClient{
		apiKey:    apiKey,
		secretKey: secretKey,
		baseURL:   baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *BinanceClient) sign(params url.Values) string {
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	params.Set("recvWindow", "5000")

	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(params.Encode()))
	signature := hex.EncodeToString(h.Sum(nil))

	return signature
}

func (c *BinanceClient) doRequest(ctx context.Context, method, endpoint string, params url.Values, signed bool) ([]byte, error) {
	var reqURL string
	var body io.Reader

	if signed {
		signature := c.sign(params)
		params.Set("signature", signature)
	}

	if method == "GET" || method == "DELETE" {
		reqURL = c.baseURL + endpoint
		if len(params) > 0 {
			reqURL += "?" + params.Encode()
		}
	} else {
		reqURL = c.baseURL + endpoint
		body = strings.NewReader(params.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-MBX-APIKEY", c.apiKey)
	if method == "POST" || method == "PUT" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// GetAccountInfo retrieves account balance and margin info
func (c *BinanceClient) GetAccountInfo(ctx context.Context) (*AccountInfo, error) {
	params := url.Values{}

	body, err := c.doRequest(ctx, "GET", "/fapi/v2/account", params, true)
	if err != nil {
		return nil, err
	}

	var account AccountInfo
	if err := json.Unmarshal(body, &account); err != nil {
		return nil, fmt.Errorf("failed to parse account info: %w", err)
	}

	return &account, nil
}

// GetPositions retrieves all open positions
func (c *BinanceClient) GetPositions(ctx context.Context) ([]Position, error) {
	params := url.Values{}

	body, err := c.doRequest(ctx, "GET", "/fapi/v2/positionRisk", params, true)
	if err != nil {
		return nil, err
	}

	var positions []Position
	if err := json.Unmarshal(body, &positions); err != nil {
		return nil, fmt.Errorf("failed to parse positions: %w", err)
	}

	// Filter only positions with non-zero amount
	var activePositions []Position
	for _, p := range positions {
		if p.PositionAmt != 0 {
			activePositions = append(activePositions, p)
		}
	}

	return activePositions, nil
}

// GetTicker gets current price for a symbol
func (c *BinanceClient) GetTicker(ctx context.Context, symbol string) (*Ticker, error) {
	params := url.Values{}
	params.Set("symbol", symbol)

	body, err := c.doRequest(ctx, "GET", "/fapi/v1/ticker/price", params, false)
	if err != nil {
		return nil, err
	}

	var ticker Ticker
	if err := json.Unmarshal(body, &ticker); err != nil {
		return nil, fmt.Errorf("failed to parse ticker: %w", err)
	}

	return &ticker, nil
}

// GetKlines retrieves candlestick data
func (c *BinanceClient) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", interval)
	params.Set("limit", strconv.Itoa(limit))

	body, err := c.doRequest(ctx, "GET", "/fapi/v1/klines", params, false)
	if err != nil {
		return nil, err
	}

	var raw [][]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse klines: %w", err)
	}

	var klines []Kline
	for _, k := range raw {
		if len(k) < 7 {
			continue
		}
		kline := Kline{
			OpenTime:  int64(k[0].(float64)),
			Open:      parseFloat(k[1]),
			High:      parseFloat(k[2]),
			Low:       parseFloat(k[3]),
			Close:     parseFloat(k[4]),
			Volume:    parseFloat(k[5]),
			CloseTime: int64(k[6].(float64)),
		}
		klines = append(klines, kline)
	}

	return klines, nil
}

// SetLeverage sets the leverage for a symbol
func (c *BinanceClient) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("leverage", strconv.Itoa(leverage))

	_, err := c.doRequest(ctx, "POST", "/fapi/v1/leverage", params, true)
	return err
}

// PlaceOrder places a new order
func (c *BinanceClient) PlaceOrder(ctx context.Context, symbol, side, orderType string, quantity float64, price float64) (*Order, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", side) // BUY or SELL
	params.Set("type", orderType) // MARKET or LIMIT
	params.Set("quantity", strconv.FormatFloat(quantity, 'f', 6, 64))

	if orderType == "LIMIT" {
		params.Set("price", strconv.FormatFloat(price, 'f', 2, 64))
		params.Set("timeInForce", "GTC")
	}

	body, err := c.doRequest(ctx, "POST", "/fapi/v1/order", params, true)
	if err != nil {
		return nil, err
	}

	var order Order
	if err := json.Unmarshal(body, &order); err != nil {
		return nil, fmt.Errorf("failed to parse order: %w", err)
	}

	return &order, nil
}

// ClosePosition closes an existing position
func (c *BinanceClient) ClosePosition(ctx context.Context, symbol string, positionAmt float64) (*Order, error) {
	side := "SELL"
	quantity := positionAmt
	if positionAmt < 0 {
		side = "BUY"
		quantity = -positionAmt
	}

	return c.PlaceOrder(ctx, symbol, side, "MARKET", quantity, 0)
}

// CancelAllOrders cancels all open orders for a symbol
func (c *BinanceClient) CancelAllOrders(ctx context.Context, symbol string) error {
	params := url.Values{}
	params.Set("symbol", symbol)

	_, err := c.doRequest(ctx, "DELETE", "/fapi/v1/allOpenOrders", params, true)
	return err
}

func parseFloat(v interface{}) float64 {
	switch val := v.(type) {
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	case float64:
		return val
	default:
		return 0
	}
}
