package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// doSAPIRequest performs a request to the Binance Spot/SAPI API (https://api.binance.com)
// used for Copy Trading endpoints
func (c *BinanceClient) doSAPIRequest(ctx context.Context, method, endpoint string, params url.Values, signed bool) ([]byte, error) {
	var reqURL string
	var body io.Reader

	if signed {
		signature := c.sign(params)
		params.Set("signature", signature)
	}

	// Always use the main API base URL for SAPI calls
	baseURL := BinanceAPIBaseURL
	// Note: Testnet support for SAPI might differ, but Copy Trading is usually mainnet only feature for production bots
	// If needed, we can add a testnet URL for SAPI, but usually it is https://testnet.binance.vision
	// However, Copy Trading might not be available on standard testnet. Assuming mainnet for now.

	if method == "GET" || method == "DELETE" {
		reqURL = baseURL + endpoint
		if len(params) > 0 {
			reqURL += "?" + params.Encode()
		}
	} else {
		reqURL = baseURL + endpoint
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

// CopyTradingStatus represents the user's status in Copy Trading
type CopyTradingStatus struct {
	IsLeadTrader bool `json:"isLeadTrader"`
	IsCopyTrader bool `json:"isCopyTrader"`
}

// GetCopyTradingStatus checks if the user is a Lead Trader or Copy Trader
func (c *BinanceClient) GetCopyTradingStatus(ctx context.Context) (*CopyTradingStatus, error) {
	// Endpoint: GET /sapi/v1/copyTrading/futures/userStatus
	params := url.Values{}

	// SAPI endpoints require signature
	body, err := c.doSAPIRequest(ctx, "GET", "/sapi/v1/copyTrading/futures/userStatus", params, true)
	if err != nil {
		return nil, err
	}

	var status struct {
		Data struct {
			IsLeadTrader bool `json:"isLeadTrader"`
			IsCopyTrader bool `json:"isCopyTrader"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &status); err != nil {
		return nil, fmt.Errorf("failed to parse copy trading status: %w", err)
	}

	return &CopyTradingStatus{
		IsLeadTrader: status.Data.IsLeadTrader,
		IsCopyTrader: status.Data.IsCopyTrader,
	}, nil
}
