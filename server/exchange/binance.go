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
	"time"
)

const (
	BinanceMainnetURL = "https://fapi.binance.com"
	BinanceTestnetURL = "https://testnet.binancefuture.com"
	BinanceAPIBaseURL = "https://api.binance.com"
)

type BinanceClient struct {
	apiKey           string
	secretKey        string
	baseURL          string
	httpClient       *http.Client
	serverTimeOffset int64 // Offset between local time and Binance server time (in ms)

	// Symbol precision cache (fetched from exchange)
	symbolInfo map[string]*SymbolInfo
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
	}

	// Sync time with Binance server
	client.syncServerTime()

	// Fetch exchange info for precision data
	client.fetchExchangeInfo()

	return client
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

// CancelAllOrders cancels all open orders for a symbol
func (c *BinanceClient) CancelAllOrders(ctx context.Context, symbol string) error {
	params := url.Values{}
	params.Set("symbol", symbol)

	_, err := c.doRequest(ctx, "DELETE", "/fapi/v1/allOpenOrders", params, true)
	return err
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
		// If TP fails, cancel the SL order to avoid orphaned orders
		log.Printf("[Binance] Take-profit failed, cancelling stop-loss order %d", slOrder.OrderID)
		_ = c.CancelOrder(ctx, symbol, slOrder.OrderID)
		return nil, nil, fmt.Errorf("failed to place take-profit: %w", err)
	}

	return slOrder, tpOrder, nil
}

// CancelOrder cancels a specific order by ID
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
