package exchange

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	BinanceMainnetURL   = "https://fapi.binance.com"
	BinanceTestnetURL   = "https://testnet.binancefuture.com"
	BinanceAPIBaseURL   = "https://api.binance.com"
	BinanceWSMainnetURL = "wss://fstream.binance.com/ws"
	BinanceWSTestnetURL = "wss://stream.binancefuture.com/ws"
)

type BinanceClient struct {
	apiKey           string
	secretKey        string
	baseURL          string
	httpClient       *http.Client
	serverTimeOffset int64 // Offset between local time and Binance server time (in ms)

	// Symbol precision cache (fetched from exchange)
	symbolInfo map[string]*SymbolInfo

	// SECURITY: Goroutine lifecycle management
	stopCh chan struct{}
	wg     sync.WaitGroup

	// WebSocket fields for User Data Stream
	wsConn         *websocket.Conn
	wsMu           sync.Mutex
	listenKey      string
	WsUpdateCh     chan *PositionUpdate // Exported for Engine to read
	wsErrorCh      chan error
	wsStopCh       chan struct{}
	wsConnected    atomic.Bool
}

// SymbolInfo holds precision info for a trading symbol
type SymbolInfo struct {
	Symbol            string
	QuantityPrecision int
	PricePrecision    int
	MinQty            float64
	StepSize          float64
	Status            string
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

// PositionUpdate represents a real-time position update from WebSocket
type PositionUpdate struct {
	Symbol           string
	PositionSide     string
	PositionAmt      float64
	EntryPrice       float64
	MarkPrice        float64
	UnrealizedPnL    float64
	Leverage         int
}

type Order struct {
	OrderID      int64   `json:"orderId"`
	Symbol       string  `json:"symbol"`
	Status       string  `json:"status"`
	Side         string  `json:"side"`
	PositionSide string  `json:"positionSide"`
	Type         string  `json:"type"`
	Price        float64 `json:"price,string"`
	AvgPrice     float64 `json:"avgPrice,string"`
	OrigQty      float64 `json:"origQty,string"`
	ExecutedQty  float64 `json:"executedQty,string"`
	Time         int64   `json:"time"`
	UpdateTime   int64   `json:"updateTime"`
}

type Ticker struct {
	Symbol string  `json:"symbol"`
	Price  float64 `json:"price,string"`
	Time   int64   `json:"time"`
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

	client := &BinanceClient{
		apiKey:    apiKey,
		secretKey: secretKey,
		baseURL:   baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		serverTimeOffset: 0,
		symbolInfo:       make(map[string]*SymbolInfo),
		stopCh:           make(chan struct{}),
		WsUpdateCh:       make(chan *PositionUpdate, 100), // Buffered channel
		wsErrorCh:        make(chan error, 10),
		wsStopCh:         make(chan struct{}),
	}

	// Sync time with Binance server
	client.syncServerTime()

	// Start periodic time sync (every 15 minutes) to prevent drift
	client.startPeriodicTimeSync()

	// Fetch exchange info for precision data
	client.fetchExchangeInfo()

	return client
}

// startPeriodicTimeSync starts a goroutine that syncs server time every 15 minutes
// This prevents timestamp drift issues during long-running sessions
func (c *BinanceClient) startPeriodicTimeSync() {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				c.syncServerTime()
			case <-c.stopCh:
				log.Printf("[Binance] Periodic time sync stopped")
				return
			}
		}
	}()
	log.Printf("[Binance] Periodic time sync started (every 15 minutes)")
}

// Close stops all background goroutines and cleans up resources
// SECURITY: Prevents goroutine leaks
func (c *BinanceClient) Close() error {
	// Stop WebSocket connection if running
	if c.wsConnected.Load() {
		close(c.wsStopCh)
		// Delete listenKey
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.DeleteListenKey(ctx)
	}

	// Signal all goroutines to stop
	close(c.stopCh)

	// Wait for all goroutines to finish
	c.wg.Wait()

	log.Printf("[Binance] Client closed, all goroutines stopped")
	return nil
}

// fetchExchangeInfo fetches symbol precision info from Binance
func (c *BinanceClient) fetchExchangeInfo() {
	resp, err := c.httpClient.Get(c.baseURL + "/fapi/v1/exchangeInfo")
	if err != nil {
		log.Printf("[Binance] Failed to fetch exchange info: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[Binance] Failed to read exchange info: %v", err)
		return
	}

	var result struct {
		Symbols []struct {
			Symbol            string `json:"symbol"`
			Status            string `json:"status"`
			QuantityPrecision int    `json:"quantityPrecision"`
			PricePrecision    int    `json:"pricePrecision"`
			Filters           []struct {
				FilterType string `json:"filterType"`
				MinQty     string `json:"minQty"`
				StepSize   string `json:"stepSize"`
			} `json:"filters"`
		} `json:"symbols"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[Binance] Failed to parse exchange info: %v", err)
		return
	}

	for _, s := range result.Symbols {
		info := &SymbolInfo{
			Symbol:            s.Symbol,
			Status:            s.Status,
			QuantityPrecision: s.QuantityPrecision,
			PricePrecision:    s.PricePrecision,
		}

		// Extract LOT_SIZE filter for min qty and step size
		for _, f := range s.Filters {
			if f.FilterType == "LOT_SIZE" {
				info.MinQty = parseFloat(f.MinQty)
				info.StepSize = parseFloat(f.StepSize)
				break
			}
		}

		c.symbolInfo[s.Symbol] = info
	}

	log.Printf("[Binance] Fetched exchange info for %d symbols", len(c.symbolInfo))
}

// syncServerTime fetches server time and calculates offset
func (c *BinanceClient) syncServerTime() {
	localTime := time.Now().UnixMilli()

	resp, err := c.httpClient.Get(c.baseURL + "/fapi/v1/time")
	if err != nil {
		log.Printf("[Binance] Failed to sync server time: %v", err)
		return
	}
	defer resp.Body.Close()

	var result struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[Binance] Failed to parse server time: %v", err)
		return
	}

	c.serverTimeOffset = result.ServerTime - localTime
	log.Printf("[Binance] Server time synced, offset: %dms", c.serverTimeOffset)
}

func (c *BinanceClient) sign(params url.Values) string {
	// Use server time with offset for accurate timestamp
	timestamp := time.Now().UnixMilli() + c.serverTimeOffset
	params.Set("timestamp", strconv.FormatInt(timestamp, 10))
	params.Set("recvWindow", "10000") // Increased from 5000 for more tolerance

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

// doPublicRequest performs a GET request to the Mainnet API (always)
// This is used for fetching market data (OI, Sentiment) that is not available or reliable on Testnet
func (c *BinanceClient) doPublicRequest(ctx context.Context, endpoint string, params url.Values) ([]byte, error) {
	reqURL := BinanceMainnetURL + endpoint
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create public request: %w", err)
	}

	// We don't strictly need API headers for public data
	// Sending Testnet keys to Mainnet is messy, so we omit Auth headers

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("public request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("public API error (status %d): %s", resp.StatusCode, string(respBody))
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

	// Filter only positions with non-zero amount AND meaningful notional value
	var activePositions []Position
	for _, p := range positions {
		if p.PositionAmt == 0 {
			continue
		}

		// Calculate notional value (position size in USD)
		amt := p.PositionAmt
		if amt < 0 {
			amt = -amt // Absolute value for shorts
		}
		notionalValue := amt * p.MarkPrice

		// Filter out dust positions (< $1 notional value)
		// These are typically leftover from partial fills or closed positions
		if notionalValue < 1.0 {
			continue
		}

		activePositions = append(activePositions, p)
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

// FundingInfo contains funding rate information for a symbol
type FundingInfo struct {
	Symbol               string  `json:"symbol"`
	MarkPrice            float64 `json:"markPrice,string"`
	IndexPrice           float64 `json:"indexPrice,string"`
	LastFundingRate      float64 `json:"lastFundingRate,string"` // Current/last funding rate (e.g., 0.0001 = 0.01%)
	NextFundingTime      int64   `json:"nextFundingTime"`        // Timestamp of next funding
	InterestRate         float64 `json:"interestRate,string"`
	EstimatedSettlePrice float64 `json:"estimatedSettlePrice,string"` // Estimated settle price for next funding
}

// GetFundingInfo gets current funding rate info for a symbol
// Uses /fapi/v1/premiumIndex endpoint which returns current funding rate and next funding time
func (c *BinanceClient) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	params := url.Values{}
	params.Set("symbol", symbol)

	body, err := c.doRequest(ctx, "GET", "/fapi/v1/premiumIndex", params, false)
	if err != nil {
		return nil, err
	}

	var funding FundingInfo
	if err := json.Unmarshal(body, &funding); err != nil {
		return nil, fmt.Errorf("failed to parse funding info: %w", err)
	}

	return &funding, nil
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

// GetHistoricalKlines retrieves candlestick data for a time range
func (c *BinanceClient) GetHistoricalKlines(ctx context.Context, symbol, interval string, startTime, endTime int64) ([]Kline, error) {
	var allKlines []Kline
	limit := 1500 // Max limit for Binance API

	for startTime < endTime {
		params := url.Values{}
		params.Set("symbol", symbol)
		params.Set("interval", interval)
		params.Set("startTime", strconv.FormatInt(startTime, 10))
		params.Set("endTime", strconv.FormatInt(endTime, 10))
		params.Set("limit", strconv.Itoa(limit))

		body, err := c.doRequest(ctx, "GET", "/fapi/v1/klines", params, false)
		if err != nil {
			return nil, err
		}

		var raw [][]interface{}
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("failed to parse klines: %w", err)
		}

		if len(raw) == 0 {
			break
		}

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
			allKlines = append(allKlines, kline)
		}

		// Move start time to after last kline
		lastKline := raw[len(raw)-1]
		startTime = int64(lastKline[6].(float64)) + 1

		// If we got less than limit, we're done
		if len(raw) < limit {
			break
		}
	}

	return allKlines, nil
}

// SetLeverage sets the leverage for a symbol
func (c *BinanceClient) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("leverage", strconv.Itoa(leverage))

	_, err := c.doRequest(ctx, "POST", "/fapi/v1/leverage", params, true)
	return err
}

// getQuantityPrecision returns the quantity precision for a symbol
func (c *BinanceClient) getQuantityPrecision(symbol string) int {
	// Check cached exchange info first
	if info, ok := c.symbolInfo[symbol]; ok {
		return info.QuantityPrecision
	}

	// Fallback to hardcoded values
	precisions := map[string]int{
		"BTCUSDT":   3,
		"ETHUSDT":   3,
		"BNBUSDT":   2,
		"SOLUSDT":   0,
		"XRPUSDT":   1,
		"DOGEUSDT":  0,
		"ADAUSDT":   0,
		"AVAXUSDT":  1,
		"DOTUSDT":   1,
		"LINKUSDT":  2,
		"MATICUSDT": 0,
		"LTCUSDT":   3,
		"ATOMUSDT":  2,
		"UNIUSDT":   1,
		"XLMUSDT":   0,
		"THUSDT":    0,
		"ARUSDT":    1,
		"FETUSDT":   0,
		"APTUSDT":   1,
		"ARBUSDT":   0,
		"OPUSDT":    1,
		"SUIUSDT":   0,
		"SEIUSDT":   0,
		"TIAUSDT":   1,
		"INJUSDT":   1,
	}
	if p, ok := precisions[symbol]; ok {
		return p
	}
	return 2 // default to 2 decimal places
}

// getPricePrecision returns the price precision for a symbol
func (c *BinanceClient) getPricePrecision(symbol string) int {
	// Check cached exchange info first
	if info, ok := c.symbolInfo[symbol]; ok {
		return info.PricePrecision
	}

	// Fallback to hardcoded values
	precisions := map[string]int{
		"BTCUSDT":   1,
		"ETHUSDT":   2,
		"BNBUSDT":   2,
		"SOLUSDT":   2,
		"XRPUSDT":   4,
		"DOGEUSDT":  5,
		"ADAUSDT":   4,
		"AVAXUSDT":  2,
		"DOTUSDT":   3,
		"LINKUSDT":  3,
		"MATICUSDT": 4,
		"LTCUSDT":   2,
		"ATOMUSDT":  3,
		"UNIUSDT":   3,
		"XLMUSDT":   5,
		"THUSDT":    5,
		"ARUSDT":    3,
		"FETUSDT":   4,
		"APTUSDT":   3,
		"ARBUSDT":   4,
		"OPUSDT":    4,
		"SUIUSDT":   4,
		"SEIUSDT":   4,
		"TIAUSDT":   4,
		"INJUSDT":   3,
	}
	if p, ok := precisions[symbol]; ok {
		return p
	}
	return 4 // default to 4 decimal places
}

// roundToStepSize rounds a quantity to the symbol's step size
func (c *BinanceClient) roundToStepSize(symbol string, quantity float64) float64 {
	if info, ok := c.symbolInfo[symbol]; ok && info.StepSize > 0 {
		// Round down to nearest step size
		steps := int(quantity / info.StepSize)
		return float64(steps) * info.StepSize
	}

	// Fallback to precision-based rounding
	precision := c.getQuantityPrecision(symbol)
	multiplier := 1.0
	for i := 0; i < precision; i++ {
		multiplier *= 10
	}
	return float64(int(quantity*multiplier)) / multiplier
}

// PlaceOrder places a new order
func (c *BinanceClient) PlaceOrder(ctx context.Context, symbol, side, orderType string, quantity float64, price float64, reduceOnly bool) (*Order, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", side)      // BUY or SELL
	params.Set("type", orderType) // MARKET or LIMIT

	// Round quantity to step size for proper precision
	quantity = c.roundToStepSize(symbol, quantity)

	// Use proper precision for the symbol
	qtyPrecision := c.getQuantityPrecision(symbol)
	qtyStr := strconv.FormatFloat(quantity, 'f', qtyPrecision, 64)
	params.Set("quantity", qtyStr)

	if orderType == "LIMIT" {
		pricePrecision := c.getPricePrecision(symbol)
		params.Set("price", strconv.FormatFloat(price, 'f', pricePrecision, 64))
		params.Set("timeInForce", "GTC")
	}

	if reduceOnly {
		params.Set("reduceOnly", "true")
	}

	log.Printf("[Binance] Placing %s %s order: %s %s @ %s (reduceOnly=%v)", orderType, side, symbol, qtyStr, "MARKET", reduceOnly)

	body, err := c.doRequest(ctx, "POST", "/fapi/v1/order", params, true)
	if err != nil {
		log.Printf("[Binance] Order failed: %v", err)
		return nil, err
	}

	var order Order
	if err := json.Unmarshal(body, &order); err != nil {
		return nil, fmt.Errorf("failed to parse order: %w", err)
	}

	log.Printf("[Binance] Order placed successfully: ID=%d, Status=%s, AvgPrice=%.2f", order.OrderID, order.Status, order.AvgPrice)
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

	// Round quantity to step size
	quantity = c.roundToStepSize(symbol, quantity)

	return c.PlaceOrder(ctx, symbol, side, "MARKET", quantity, 0, true)
}

// CancelAllOrders cancels all open orders for a symbol (both regular and algo orders)
func (c *BinanceClient) CancelAllOrders(ctx context.Context, symbol string) error {
	// 1. Cancel regular orders
	params := url.Values{}
	params.Set("symbol", symbol)
	_, err := c.doRequest(ctx, "DELETE", "/fapi/v1/allOpenOrders", params, true)
	if err != nil {
		log.Printf("[Binance] Warning: failed to cancel regular orders for %s: %v", symbol, err)
	}

	// 2. Cancel algo orders (SL/TP placed via /fapi/v1/algoOrder)
	if err := c.CancelAllAlgoOrders(ctx, symbol); err != nil {
		log.Printf("[Binance] Warning: failed to cancel algo orders for %s: %v", symbol, err)
	}

	return nil
}

// GetOpenAlgoOrders returns all open algo orders (SL/TP) for a symbol
func (c *BinanceClient) GetOpenAlgoOrders(ctx context.Context, symbol string) ([]map[string]interface{}, error) {
	params := url.Values{}
	params.Set("symbol", symbol)

	body, err := c.doRequest(ctx, "GET", "/fapi/v1/openAlgoOrders", params, true)
	if err != nil {
		return nil, err
	}

	var result struct {
		AlgoOrders []map[string]interface{} `json:"algoOrders"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		// Fallback: try parsing as direct array
		var orders []map[string]interface{}
		if err := json.Unmarshal(body, &orders); err != nil {
			return nil, fmt.Errorf("failed to parse algo orders: %w", err)
		}
		return orders, nil
	}

	return result.AlgoOrders, nil
}

// CancelAllAlgoOrders cancels all open algo orders (SL/TP) for a symbol
func (c *BinanceClient) CancelAllAlgoOrders(ctx context.Context, symbol string) error {
	// Get all open algo orders
	algoOrders, err := c.GetOpenAlgoOrders(ctx, symbol)
	if err != nil {
		return err
	}

	if len(algoOrders) == 0 {
		return nil
	}

	log.Printf("[Binance] Found %d open algo orders for %s, cancelling...", len(algoOrders), symbol)

	// Cancel each algo order
	for _, order := range algoOrders {
		algoID, ok := order["algoId"].(float64)
		if !ok {
			continue
		}
		if err := c.CancelAlgoOrder(ctx, symbol, int64(algoID)); err != nil {
			log.Printf("[Binance] Warning: failed to cancel algo order %d: %v", int64(algoID), err)
		}
	}

	return nil
}

// PlaceStopLoss places a stop-loss order (STOP_MARKET) using Algo Order API
// For LONG positions: side should be "SELL", stopPrice below entry
// For SHORT positions: side should be "BUY", stopPrice above entry
// Note: As of 2025-12-09, Binance requires STOP_MARKET orders to use /fapi/v1/algoOrder endpoint
func (c *BinanceClient) PlaceStopLoss(ctx context.Context, symbol, side string, quantity, stopPrice float64) (*Order, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", side)
	params.Set("type", "STOP_MARKET")
	params.Set("algoType", "CONDITIONAL")
	params.Set("closePosition", "true") // Close entire position when triggered

	// Set trigger price with proper precision (renamed from stopPrice for algo orders)
	pricePrecision := c.getPricePrecision(symbol)
	params.Set("triggerPrice", strconv.FormatFloat(stopPrice, 'f', pricePrecision, 64))

	log.Printf("[Binance] Placing STOP_LOSS (Algo): %s %s @ %.2f", symbol, side, stopPrice)

	body, err := c.doRequest(ctx, "POST", "/fapi/v1/algoOrder", params, true)
	if err != nil {
		log.Printf("[Binance] Stop-loss order failed: %v", err)
		return nil, err
	}

	// Parse algo order response
	var algoResp struct {
		AlgoID       int64  `json:"algoId"`
		AlgoStatus   string `json:"algoStatus"`
		AlgoType     string `json:"algoType"`
		Symbol       string `json:"symbol"`
		Side         string `json:"side"`
		TriggerPrice string `json:"triggerPrice"`
	}
	if err := json.Unmarshal(body, &algoResp); err != nil {
		return nil, fmt.Errorf("failed to parse algo order: %w", err)
	}

	// Convert to Order struct for compatibility
	order := &Order{
		OrderID: algoResp.AlgoID,
		Symbol:  algoResp.Symbol,
		Status:  algoResp.AlgoStatus,
		Side:    algoResp.Side,
		Type:    "STOP_MARKET",
	}

	log.Printf("[Binance] Stop-loss placed: AlgoID=%d, Status=%s", order.OrderID, order.Status)
	return order, nil
}

// PlaceTakeProfit places a take-profit order (TAKE_PROFIT_MARKET) using Algo Order API
// For LONG positions: side should be "SELL", stopPrice above entry
// For SHORT positions: side should be "BUY", stopPrice below entry
// Note: As of 2025-12-09, Binance requires TAKE_PROFIT_MARKET orders to use /fapi/v1/algoOrder endpoint
func (c *BinanceClient) PlaceTakeProfit(ctx context.Context, symbol, side string, quantity, stopPrice float64) (*Order, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", side)
	params.Set("type", "TAKE_PROFIT_MARKET")
	params.Set("algoType", "CONDITIONAL")
	params.Set("closePosition", "true") // Close entire position when triggered

	// Set trigger price with proper precision (renamed from stopPrice for algo orders)
	pricePrecision := c.getPricePrecision(symbol)
	params.Set("triggerPrice", strconv.FormatFloat(stopPrice, 'f', pricePrecision, 64))

	log.Printf("[Binance] Placing TAKE_PROFIT (Algo): %s %s @ %.2f", symbol, side, stopPrice)

	body, err := c.doRequest(ctx, "POST", "/fapi/v1/algoOrder", params, true)
	if err != nil {
		log.Printf("[Binance] Take-profit order failed: %v", err)
		return nil, err
	}

	// Parse algo order response
	var algoResp struct {
		AlgoID       int64  `json:"algoId"`
		AlgoStatus   string `json:"algoStatus"`
		AlgoType     string `json:"algoType"`
		Symbol       string `json:"symbol"`
		Side         string `json:"side"`
		TriggerPrice string `json:"triggerPrice"`
	}
	if err := json.Unmarshal(body, &algoResp); err != nil {
		return nil, fmt.Errorf("failed to parse algo order: %w", err)
	}

	// Convert to Order struct for compatibility
	order := &Order{
		OrderID: algoResp.AlgoID,
		Symbol:  algoResp.Symbol,
		Status:  algoResp.AlgoStatus,
		Side:    algoResp.Side,
		Type:    "TAKE_PROFIT_MARKET",
	}

	log.Printf("[Binance] Take-profit placed: AlgoID=%d, Status=%s", order.OrderID, order.Status)
	return order, nil
}

// PlaceBracketOrders places both stop-loss and take-profit orders for a position
// Returns (slOrder, tpOrder, error)
func (c *BinanceClient) PlaceBracketOrders(ctx context.Context, symbol string, isLong bool, entryPrice, slPct, tpPct float64) (*Order, *Order, error) {
	var slPrice, tpPrice float64
	var closeSide string

	if isLong {
		closeSide = "SELL"
		slPrice = entryPrice * (1 - slPct/100) // SL below entry for long
		tpPrice = entryPrice * (1 + tpPct/100) // TP above entry for long
	} else {
		closeSide = "BUY"
		slPrice = entryPrice * (1 + slPct/100) // SL above entry for short
		tpPrice = entryPrice * (1 - tpPct/100) // TP below entry for short
	}

	log.Printf("[Binance] Placing bracket orders for %s: entry=%.2f, SL=%.2f (%.1f%%), TP=%.2f (%.1f%%)",
		symbol, entryPrice, slPrice, slPct, tpPrice, tpPct)

	// Place stop-loss
	slOrder, err := c.PlaceStopLoss(ctx, symbol, closeSide, 0, slPrice)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to place stop-loss: %w", err)
	}

	// Place take-profit
	tpOrder, err := c.PlaceTakeProfit(ctx, symbol, closeSide, 0, tpPrice)
	if err != nil {
		// If TP fails, cancel the SL algo order to avoid orphaned orders
		log.Printf("[Binance] Take-profit failed, cancelling stop-loss algo order %d", slOrder.OrderID)
		_ = c.CancelAlgoOrder(ctx, symbol, slOrder.OrderID)
		return nil, nil, fmt.Errorf("failed to place take-profit: %w", err)
	}

	return slOrder, tpOrder, nil
}

// CancelOrder cancels a specific regular order by ID
func (c *BinanceClient) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", strconv.FormatInt(orderID, 10))

	_, err := c.doRequest(ctx, "DELETE", "/fapi/v1/order", params, true)
	if err != nil {
		log.Printf("[Binance] Failed to cancel order %d: %v", orderID, err)
		return err
	}

	log.Printf("[Binance] Cancelled order %d for %s", orderID, symbol)
	return nil
}

// CancelAlgoOrder cancels a specific algo order (SL/TP) by AlgoID
// IMPORTANT: Algo orders (placed via /fapi/v1/algoOrder) must be cancelled
// using this method, not CancelOrder which uses regular order API
func (c *BinanceClient) CancelAlgoOrder(ctx context.Context, symbol string, algoID int64) error {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("algoId", strconv.FormatInt(algoID, 10))

	_, err := c.doRequest(ctx, "DELETE", "/fapi/v1/algoOrder", params, true)
	if err != nil {
		log.Printf("[Binance] Failed to cancel algo order %d: %v", algoID, err)
		return err
	}

	log.Printf("[Binance] Cancelled algo order %d for %s", algoID, symbol)
	return nil
}

// GetOpenOrders returns all open orders for a symbol
func (c *BinanceClient) GetOpenOrders(ctx context.Context, symbol string) ([]Order, error) {
	params := url.Values{}
	params.Set("symbol", symbol)

	body, err := c.doRequest(ctx, "GET", "/fapi/v1/openOrders", params, true)
	if err != nil {
		return nil, err
	}

	var orders []Order
	if err := json.Unmarshal(body, &orders); err != nil {
		return nil, fmt.Errorf("failed to parse orders: %w", err)
	}

	return orders, nil
}

// Trade represents a single trade (fill) from Binance
type Trade struct {
	ID              int64   `json:"id"`
	Symbol          string  `json:"symbol"`
	OrderID         int64   `json:"orderId"`
	Side            string  `json:"side"`         // BUY or SELL
	Price           float64 `json:"price,string"` // Execution price
	Qty             float64 `json:"qty,string"`   // Executed quantity
	RealizedPnL     float64 `json:"realizedPnl,string"`
	QuoteQty        float64 `json:"quoteQty,string"` // Executed value in USDT
	Commission      float64 `json:"commission,string"`
	CommissionAsset string  `json:"commissionAsset"`
	Time            int64   `json:"time"`
	PositionSide    string  `json:"positionSide"`
	Buyer           bool    `json:"buyer"`
	Maker           bool    `json:"maker"`
}

// GetTradeHistory retrieves trade history for a symbol
// If symbol is empty, returns trades for all symbols
// startTime is in milliseconds, 0 means from the beginning
func (c *BinanceClient) GetTradeHistory(ctx context.Context, symbol string, startTime int64, limit int) ([]Trade, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	if startTime > 0 {
		params.Set("startTime", strconv.FormatInt(startTime, 10))
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	} else {
		params.Set("limit", "500") // Default limit
	}

	body, err := c.doRequest(ctx, "GET", "/fapi/v1/userTrades", params, true)
	if err != nil {
		return nil, err
	}

	var trades []Trade
	if err := json.Unmarshal(body, &trades); err != nil {
		return nil, fmt.Errorf("failed to parse trades: %w", err)
	}

	return trades, nil
}

// GetIncomeHistory retrieves income history (PnL, funding, commission, etc.)
// incomeType can be: REALIZED_PNL, FUNDING_FEE, COMMISSION, etc.
func (c *BinanceClient) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime int64, limit int) ([]map[string]interface{}, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	if incomeType != "" {
		params.Set("incomeType", incomeType)
	}
	if startTime > 0 {
		params.Set("startTime", strconv.FormatInt(startTime, 10))
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	} else {
		params.Set("limit", "100")
	}

	body, err := c.doRequest(ctx, "GET", "/fapi/v1/income", params, true)
	if err != nil {
		return nil, err
	}

	var income []map[string]interface{}
	if err := json.Unmarshal(body, &income); err != nil {
		return nil, fmt.Errorf("failed to parse income: %w", err)
	}

	return income, nil
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

// OpenInterestData represents current open interest for a symbol
type OpenInterestData struct {
	Symbol       string  `json:"symbol"`
	OpenInterest float64 `json:"openInterest,string"` // OI in contracts
	Time         int64   `json:"time"`
}

// GetOpenInterest fetches current open interest for a symbol (FREE - no API key needed)
// Endpoint: GET /fapi/v1/openInterest (weight: 1)
func (c *BinanceClient) GetOpenInterest(ctx context.Context, symbol string) (*OpenInterestData, error) {
	params := url.Values{}
	params.Set("symbol", symbol)

	body, err := c.doPublicRequest(ctx, "/fapi/v1/openInterest", params)
	if err != nil {
		return nil, err
	}

	var oi OpenInterestData
	if err := json.Unmarshal(body, &oi); err != nil {
		return nil, fmt.Errorf("failed to parse open interest: %w", err)
	}

	return &oi, nil
}

// OpenInterestHistData represents historical open interest statistics
type OpenInterestHistData struct {
	Symbol               string  `json:"symbol"`
	SumOpenInterest      float64 `json:"sumOpenInterest,string"`      // Total OI in contracts
	SumOpenInterestValue float64 `json:"sumOpenInterestValue,string"` // Total OI value in USD
	Timestamp            int64   `json:"timestamp"`
}

// OIAnalysis represents analyzed OI data with change percentages
type OIAnalysis struct {
	Symbol       string
	CurrentOI    float64 // Current OI value in USD
	OIChange1H   float64 // OI change in last 1 hour (%)
	OIChange4H   float64 // OI change in last 4 hours (%)
	OIChange24H  float64 // OI change in last 24 hours (%)
	OISignal     string  // BULLISH, BEARISH, REVERSAL_UP, REVERSAL_DOWN, NEUTRAL
	OIConfidence string  // HIGH, MEDIUM, LOW

	// Inferred Liquidation Detection (FREE alternative to liquidation API)
	// Detected by: Large OI drop + Rapid price move in opposite direction
	LiquidationPressure string // LONG_LIQUIDATION, SHORT_LIQUIDATION, NONE
	LiquidationSeverity string // HIGH, MEDIUM, LOW, NONE
	LiquidationMessage  string // Human readable explanation
}

// GetOpenInterestHist fetches historical OI data and calculates changes (FREE)
// Endpoint: GET /futures/data/openInterestHist (weight: 0, rate limit: 1000 req/5min)
// period can be: "5m", "15m", "30m", "1h", "2h", "4h", "6h", "12h", "1d"
func (c *BinanceClient) GetOpenInterestHist(ctx context.Context, symbol string, period string, limit int) ([]OpenInterestHistData, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("period", period)
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	} else {
		params.Set("limit", "30") // Default to 30 data points
	}

	body, err := c.doPublicRequest(ctx, "/futures/data/openInterestHist", params)
	if err != nil {
		if strings.Contains(err.Error(), "status 202") {
			return nil, nil // Treat as no data available yet
		}
		return nil, err
	}

	var oiHist []OpenInterestHistData
	if err := json.Unmarshal(body, &oiHist); err != nil {
		return nil, fmt.Errorf("failed to parse OI history: %w", err)
	}

	return oiHist, nil
}

// GetOIAnalysis fetches OI data and calculates changes with interpretation (FREE)
func (c *BinanceClient) GetOIAnalysis(ctx context.Context, symbol string, priceChange float64) (*OIAnalysis, error) {
	// Fetch 1-hour OI history (each data point is 5min, so 12 points = 1 hour)
	oiHist, err := c.GetOpenInterestHist(ctx, symbol, "5m", 30)
	if err != nil {
		return nil, err
	}

	if len(oiHist) < 2 {
		return nil, fmt.Errorf("insufficient OI history data")
	}

	// Get latest and calculate changes
	latest := oiHist[len(oiHist)-1]
	currentOI := latest.SumOpenInterestValue

	// Calculate 1H change (compare with 12 periods ago if available)
	change1H := 0.0
	if len(oiHist) >= 12 {
		oldOI := oiHist[len(oiHist)-12].SumOpenInterestValue
		if oldOI > 0 {
			change1H = ((currentOI - oldOI) / oldOI) * 100
		}
	}

	// Calculate 4H change (would need more data, approximate with available)
	change4H := 0.0
	if len(oiHist) >= 30 {
		oldOI := oiHist[0].SumOpenInterestValue
		if oldOI > 0 {
			change4H = ((currentOI - oldOI) / oldOI) * 100
		}
	}

	// Interpret OI signal
	signal := "NEUTRAL"
	confidence := "LOW"

	if change1H > 0 && priceChange > 0 {
		signal = "BULLISH"
		if change1H > 2 && priceChange > 1 {
			confidence = "HIGH"
		} else {
			confidence = "MEDIUM"
		}
	} else if change1H > 0 && priceChange < 0 {
		signal = "BEARISH"
		if change1H > 2 && priceChange < -1 {
			confidence = "HIGH"
		} else {
			confidence = "MEDIUM"
		}
	} else if change1H < 0 && priceChange > 0 {
		signal = "REVERSAL_UP" // Shorts covering
		confidence = "LOW"
	} else if change1H < 0 && priceChange < 0 {
		signal = "REVERSAL_DOWN" // Longs capitulating
		confidence = "LOW"
	}

	// Infer liquidation pressure (FREE alternative to liquidation API)
	// Logic: Large OI drop + Rapid price move = Cascade liquidation
	liqPressure := "NONE"
	liqSeverity := "NONE"
	liqMessage := ""

	// Calculate OI velocity (rate of change)
	oiDropThreshold := -2.0 // OI dropped more than 2%
	priceThreshold := 1.5   // Price moved more than 1.5%

	if change1H < oiDropThreshold {
		// Significant OI drop detected - likely liquidations
		if priceChange < -priceThreshold {
			// OI down + Price down = Long liquidation cascade
			liqPressure = "LONG_LIQUIDATION"
			if change1H < -5 && priceChange < -3 {
				liqSeverity = "HIGH"
				liqMessage = "🚨 HEAVY LONG LIQUIDATIONS - Large OI drop + price crash. Potential capitulation bottom forming."
			} else if change1H < -3 {
				liqSeverity = "MEDIUM"
				liqMessage = "⚠️ Long liquidations detected - Longs getting stopped/liquidated. Watch for reversal."
			} else {
				liqSeverity = "LOW"
				liqMessage = "Long positions being closed - Minor liquidation pressure."
			}
		} else if priceChange > priceThreshold {
			// OI down + Price up = Short liquidation/squeeze
			liqPressure = "SHORT_LIQUIDATION"
			if change1H < -5 && priceChange > 3 {
				liqSeverity = "HIGH"
				liqMessage = "🚨 SHORT SQUEEZE - Heavy short liquidations driving price up. Caution on new longs."
			} else if change1H < -3 {
				liqSeverity = "MEDIUM"
				liqMessage = "⚠️ Short liquidations detected - Shorts getting squeezed. Rally may exhaust."
			} else {
				liqSeverity = "LOW"
				liqMessage = "Short positions being closed - Minor squeeze pressure."
			}
		}
	}

	return &OIAnalysis{
		Symbol:              symbol,
		CurrentOI:           currentOI,
		OIChange1H:          change1H,
		OIChange4H:          change4H,
		OIChange24H:         0, // Would need 24h of data
		OISignal:            signal,
		OIConfidence:        confidence,
		LiquidationPressure: liqPressure,
		LiquidationSeverity: liqSeverity,
		LiquidationMessage:  liqMessage,
	}, nil
}

// LongShortRatioData represents top trader long/short ratio
type LongShortRatioData struct {
	Symbol         string  `json:"symbol"`
	LongShortRatio float64 `json:"longShortRatio,string"` // Long accounts / Short accounts
	LongAccount    float64 `json:"longAccount,string"`    // % of accounts that are long
	ShortAccount   float64 `json:"shortAccount,string"`   // % of accounts that are short
	Timestamp      int64   `json:"timestamp"`
}

// GetTopTraderLongShortRatio fetches top trader positioning (FREE)
// Endpoint: GET /futures/data/topLongShortPositionRatio (weight: 0)
// period can be: "5m", "15m", "30m", "1h", "2h", "4h", "6h", "12h", "1d"
func (c *BinanceClient) GetTopTraderLongShortRatio(ctx context.Context, symbol string, period string, limit int) ([]LongShortRatioData, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("period", period)
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	} else {
		params.Set("limit", "1") // Just get latest
	}

	body, err := c.doPublicRequest(ctx, "/futures/data/topLongShortPositionRatio", params)
	if err != nil {
		if strings.Contains(err.Error(), "status 202") {
			return nil, nil // Treat as no data available yet
		}
		return nil, err
	}

	var ratios []LongShortRatioData
	if err := json.Unmarshal(body, &ratios); err != nil {
		return nil, fmt.Errorf("failed to parse long/short ratio: %w", err)
	}

	return ratios, nil
}

// LongShortAnalysis calculates sentiment shifts
type LongShortAnalysis struct {
	LongShortRatioData
	LSRatioChange1H  float64 // Change in Long Ratio over 1 hour
	SentimentTrend   string  // "BECOMING_BULLISH", "BECOMING_BEARISH", "STABLE"
	SentimentMessage string  // Guidance message
}

// GetLatestLongShortRatio fetches the latest long/short ratio for a symbol (FREE)
func (c *BinanceClient) GetLatestLongShortRatio(ctx context.Context, symbol string) (*LongShortRatioData, error) {
	ratios, err := c.GetTopTraderLongShortRatio(ctx, symbol, "5m", 1)
	if err != nil {
		return nil, err
	}
	if len(ratios) == 0 {
		return nil, fmt.Errorf("no long/short ratio data available")
	}
	return &ratios[0], nil
}

// GetLongShortAnalysis fetches L/S ratio history and detects sentiment shifts
func (c *BinanceClient) GetLongShortAnalysis(ctx context.Context, symbol string) (*LongShortAnalysis, error) {
	// standard is 5m periods, so 12 periods = 1 hour
	ratios, err := c.GetTopTraderLongShortRatio(ctx, symbol, "5m", 13)
	if err != nil {
		return nil, err
	}
	if len(ratios) == 0 {
		return nil, fmt.Errorf("no long/short ratio data")
	}

	current := ratios[len(ratios)-1]

	analysis := &LongShortAnalysis{
		LongShortRatioData: current,
		SentimentTrend:     "STABLE",
	}

	if len(ratios) >= 12 {
		old := ratios[len(ratios)-12]
		// Calculate absolute change in Long Percentage
		longChange := (current.LongAccount - old.LongAccount) * 100
		analysis.LSRatioChange1H = longChange

		if longChange > 5 {
			analysis.SentimentTrend = "BECOMING_BULLISH"
			analysis.SentimentMessage = fmt.Sprintf("Retail sentiment shifting LONG (+%.1f%% in 1h) - watch for contrarian reversal if crowded > 65%%", longChange)
		} else if longChange < -5 {
			analysis.SentimentTrend = "BECOMING_BEARISH"
			analysis.SentimentMessage = fmt.Sprintf("Retail sentiment shifting SHORT (%.1f%% drop in long ratio) - potential squeeze up if price holds", longChange)
		} else {
			analysis.SentimentMessage = "Sentiment is stable"
		}
	} else {
		analysis.SentimentMessage = "Insufficient history for trend"
	}

	return analysis, nil
}

// ===== User Data Stream (WebSocket) API =====

// CreateListenKey creates a new listenKey for User Data Stream WebSocket
// listenKey is valid for 60 minutes and must be renewed every 30 minutes
// Endpoint: POST /fapi/v1/listenKey
// Note: This endpoint does NOT require signature, only API key header
func (c *BinanceClient) CreateListenKey(ctx context.Context) (string, error) {
	reqURL := c.baseURL + "/fapi/v1/listenKey"

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create listen key request: %w", err)
	}

	req.Header.Set("X-MBX-APIKEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("listen key request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read listen key response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("listen key API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ListenKey string `json:"listenKey"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse listen key: %w", err)
	}

	log.Printf("[Binance] Created listenKey: %s", result.ListenKey)
	return result.ListenKey, nil
}

// RenewListenKey renews an existing listenKey to extend its validity
// Should be called every 30 minutes to keep the WebSocket connection alive
// Endpoint: PUT /fapi/v1/listenKey
// Note: This endpoint does NOT require signature, only API key header
func (c *BinanceClient) RenewListenKey(ctx context.Context) error {
	reqURL := c.baseURL + "/fapi/v1/listenKey"

	req, err := http.NewRequestWithContext(ctx, "PUT", reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create renew request: %w", err)
	}

	req.Header.Set("X-MBX-APIKEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("renew request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read renew response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("renew API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	log.Printf("[Binance] Renewed listenKey")
	return nil
}

// DeleteListenKey deletes an existing listenKey (cleanup on shutdown)
// Endpoint: DELETE /fapi/v1/listenKey
// Note: This endpoint does NOT require signature, only API key header
func (c *BinanceClient) DeleteListenKey(ctx context.Context) error {
	reqURL := c.baseURL + "/fapi/v1/listenKey"

	req, err := http.NewRequestWithContext(ctx, "DELETE", reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}

	req.Header.Set("X-MBX-APIKEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read delete response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	log.Printf("[Binance] Deleted listenKey")
	return nil
}

// StartUserDataStream starts WebSocket User Data Stream for real-time position updates
// This enables <1s latency for position updates vs 30-60s with REST polling
func (c *BinanceClient) StartUserDataStream(ctx context.Context) error {
	// Create listenKey
	listenKey, err := c.CreateListenKey(ctx)
	if err != nil {
		return fmt.Errorf("failed to create listenKey: %w", err)
	}
	c.listenKey = listenKey

	// Determine WebSocket URL based on baseURL
	wsURL := BinanceWSMainnetURL
	if strings.Contains(c.baseURL, "testnet") {
		wsURL = BinanceWSTestnetURL
	}
	wsURL = fmt.Sprintf("%s/%s", wsURL, listenKey)

	// Connect to WebSocket
	if err := c.connectWebSocket(wsURL); err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	// Start goroutines for message reading and listenKey renewal
	c.wg.Add(2)
	go c.readWebSocketMessages()
	go c.renewListenKeyPeriodically()

	log.Printf("[Binance] WebSocket User Data Stream started")
	return nil
}

// connectWebSocket establishes WebSocket connection
func (c *BinanceClient) connectWebSocket(wsURL string) error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	c.wsConn = conn
	c.wsConnected.Store(true)
	log.Printf("[Binance] WebSocket connected to %s", wsURL)
	return nil
}

// readWebSocketMessages reads and processes WebSocket messages
func (c *BinanceClient) readWebSocketMessages() {
	defer c.wg.Done()
	defer func() {
		c.wsConnected.Store(false)
		if c.wsConn != nil {
			c.wsConn.Close()
		}
	}()

	for {
		select {
		case <-c.wsStopCh:
			log.Printf("[Binance] WebSocket reader stopped")
			return
		default:
			// Set read deadline to detect stale connections
			c.wsConn.SetReadDeadline(time.Now().Add(60 * time.Second))

			_, message, err := c.wsConn.ReadMessage()
			if err != nil {
				log.Printf("[Binance] WebSocket read error: %v", err)
				// Attempt reconnection
				if c.reconnectWebSocket() != nil {
					log.Printf("[Binance] WebSocket reconnection failed, stopping reader")
					return
				}
				continue
			}

			// Parse message
			if err := c.parseWebSocketMessage(message); err != nil {
				log.Printf("[Binance] Failed to parse WebSocket message: %v", err)
			}
		}
	}
}

// parseWebSocketMessage parses incoming WebSocket messages
func (c *BinanceClient) parseWebSocketMessage(message []byte) error {
	// First, let's try to parse as a generic map to see the structure
	var rawMsg map[string]interface{}
	if err := json.Unmarshal(message, &rawMsg); err != nil {
		return fmt.Errorf("failed to parse as map: %w", err)
	}

	eventType, ok := rawMsg["e"].(string)
	if !ok {
		return fmt.Errorf("event type 'e' is not a string, got: %T %v", rawMsg["e"], rawMsg["e"])
	}

	switch eventType {
	case "ACCOUNT_UPDATE":
		return c.handleAccountUpdate(message)
	case "ORDER_TRADE_UPDATE":
		// Can be extended later if needed
		log.Printf("[Binance] Received ORDER_TRADE_UPDATE event")
	case "listenKeyExpired":
		log.Printf("[Binance] listenKey expired, triggering reconnection")
		go c.reconnectWebSocket()
	default:
		log.Printf("[Binance] Unknown WebSocket event type: %s", eventType)
	}

	return nil
}

// handleAccountUpdate processes ACCOUNT_UPDATE events
func (c *BinanceClient) handleAccountUpdate(message []byte) error {
	var update struct {
		EventType string `json:"e"`
		EventTime int64  `json:"E"`
		Data      struct {
			Positions []struct {
				Symbol       string `json:"s"`
				PositionSide string `json:"ps"`
				PositionAmt  string `json:"pa"`
				EntryPrice   string `json:"ep"`
				MarkPrice    string `json:"mp"`
				UnrealizedPnL string `json:"up"`
			} `json:"P"`
		} `json:"a"`
	}

	if err := json.Unmarshal(message, &update); err != nil {
		return fmt.Errorf("failed to parse ACCOUNT_UPDATE: %w", err)
	}

	// Process each position update
	for _, pos := range update.Data.Positions {
		posUpdate := &PositionUpdate{
			Symbol:        pos.Symbol,
			PositionSide:  pos.PositionSide,
			PositionAmt:   parseFloat(pos.PositionAmt),
			EntryPrice:    parseFloat(pos.EntryPrice),
			MarkPrice:     parseFloat(pos.MarkPrice),
			UnrealizedPnL: parseFloat(pos.UnrealizedPnL),
		}

		// Send to update channel (non-blocking)
		select {
		case c.WsUpdateCh <- posUpdate:
			log.Printf("[Binance] WebSocket position update: %s %.4f @ %.2f (PnL: %.2f)",
				posUpdate.Symbol, posUpdate.PositionAmt, posUpdate.MarkPrice, posUpdate.UnrealizedPnL)
		default:
			log.Printf("[Binance] Warning: WsUpdateCh buffer full, dropping update for %s", pos.Symbol)
		}
	}

	return nil
}

// reconnectWebSocket attempts to reconnect the WebSocket with exponential backoff
func (c *BinanceClient) reconnectWebSocket() error {
	maxRetries := 5
	baseDelay := 5 * time.Second

	for i := 0; i < maxRetries; i++ {
		log.Printf("[Binance] Attempting WebSocket reconnection (attempt %d/%d)", i+1, maxRetries)

		// Close old connection
		c.wsMu.Lock()
		if c.wsConn != nil {
			c.wsConn.Close()
			c.wsConn = nil
		}
		c.wsMu.Unlock()

		// Wait before retry (exponential backoff)
		if i > 0 {
			delay := baseDelay * time.Duration(1<<uint(i-1))
			if delay > 60*time.Second {
				delay = 60 * time.Second
			}
			time.Sleep(delay)
		}

		// Create new listenKey
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		listenKey, err := c.CreateListenKey(ctx)
		cancel()
		if err != nil {
			log.Printf("[Binance] Failed to create new listenKey: %v", err)
			continue
		}
		c.listenKey = listenKey

		// Determine WebSocket URL
		wsURL := BinanceWSMainnetURL
		if strings.Contains(c.baseURL, "testnet") {
			wsURL = BinanceWSTestnetURL
		}
		wsURL = fmt.Sprintf("%s/%s", wsURL, listenKey)

		// Reconnect
		if err := c.connectWebSocket(wsURL); err != nil {
			log.Printf("[Binance] Reconnection failed: %v", err)
			continue
		}

		log.Printf("[Binance] WebSocket reconnected successfully")
		return nil
	}

	return fmt.Errorf("failed to reconnect after %d attempts", maxRetries)
}

// renewListenKeyPeriodically renews listenKey every 30 minutes
func (c *BinanceClient) renewListenKeyPeriodically() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := c.RenewListenKey(ctx); err != nil {
				log.Printf("[Binance] Failed to renew listenKey: %v", err)
				// If renewal fails, trigger reconnection
				go c.reconnectWebSocket()
			}
			cancel()
		case <-c.wsStopCh:
			log.Printf("[Binance] listenKey renewal stopped")
			return
		}
	}
}
