package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

const OpenRouterBaseURL = "https://openrouter.ai/api/v1"

type Client struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
}

type ChatResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			Reasoning string `json:"reasoning"` // Chain-of-thought from reasoning models
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string      `json:"message"`
		Type    string      `json:"type"`
		Code    interface{} `json:"code"` // Can be string or number depending on API response
	} `json:"error,omitempty"`
}

// ChatResult holds the response content and optional reasoning
type ChatResult struct {
	Content   string
	Reasoning string
}

type TradingDecision struct {
	Action        string  `json:"action"`          // BUY, SELL, HOLD, CLOSE
	Symbol        string  `json:"symbol"`          // Trading pair
	Confidence    float64 `json:"confidence"`      // 0-100
	Reasoning     string  `json:"reasoning"`       // AI's reasoning
	StopLossPct   float64 `json:"stop_loss_pct"`   // Stop loss as percentage (e.g., 2.0 = 2%)
	TakeProfitPct float64 `json:"take_profit_pct"` // Take profit as percentage (e.g., 6.0 = 6%)
	// Legacy fields for backward compatibility
	StopLoss   float64 `json:"stop_loss,omitempty"`   // Deprecated: use StopLossPct
	TakeProfit float64 `json:"take_profit,omitempty"` // Deprecated: use TakeProfitPct
}

func NewClient(apiKey, model string) *Client {
	// Custom Dialer to force IPv4 and log connections
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Force IPv4 by using "tcp4"
			conn, err := dialer.DialContext(ctx, "tcp4", addr)
			if err != nil {
				log.Printf("[OpenRouter] Dial failed to %s: %v", addr, err)
				return nil, err
			}
			// Log the resolved IP address to help diagnose DNS/routing issues
			log.Printf("[OpenRouter] Successfully established connection to %s (%s)", addr, conn.RemoteAddr().String())
			return conn, nil
		},
		ForceAttemptHTTP2:     false, // Disable HTTP/2 to avoid potential framing/tunneling issues
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &Client{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout:   180 * time.Second, // 3 minutes for slower models
			Transport: transport,
		},
	}
}

// SetModel changes the model used for requests
func (c *Client) SetModel(model string) {
	c.model = model
}

// GetModel returns the current model
func (c *Client) GetModel() string {
	return c.model
}

func (c *Client) Chat(messages []Message) (string, error) {
	result, err := c.ChatWithReasoning(messages)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// ChatWithReasoning returns both content and reasoning (for reasoning models)
func (c *Client) ChatWithReasoning(messages []Message) (*ChatResult, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		result, err := c.doChat(messages, attempt)
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Check if error is retryable (timeout, connection errors, rate limits)
		if !isRetryableError(err) {
			return nil, err
		}

		if attempt < maxRetries {
			// Exponential backoff: 2s, 4s, 8s
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			log.Printf("[OpenRouter] Retry %d/%d after %v (error: %v)", attempt, maxRetries, backoff, err)
			time.Sleep(backoff)
		}
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// isRetryableError checks if the error is transient and worth retrying
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	retryablePatterns := []string{
		"timeout",
		"deadline exceeded",
		"connection reset",
		"connection refused",
		"temporary failure",
		"no such host",
		"EOF",
		"stream error",
		"429", // rate limit
		"502", // bad gateway
		"503", // service unavailable
		"504", // gateway timeout
	}
	for _, pattern := range retryablePatterns {
		if contains(errStr, pattern) {
			return true
		}
	}
	return false
}

// contains performs a case-insensitive substring check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsLower(toLower(s), toLower(substr)))
}

func containsLower(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// doChat performs a single chat request
func (c *Client) doChat(messages []Message, attempt int) (*ChatResult, error) {
	start := time.Now()

	req := ChatRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   4096,
		Temperature: 0.7,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Log prompt size for debugging
	promptSize := 0
	for _, m := range messages {
		promptSize += len(m.Content)
	}
	log.Printf("[OpenRouter] Sending request to %s (prompt size: %d chars, model: %s, attempt: %d)", c.model, promptSize, c.model, attempt)

	httpReq, err := http.NewRequest("POST", OpenRouterBaseURL+"/chat/completions", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("HTTP-Referer", "https://passive-income-ahh.local")
	httpReq.Header.Set("X-Title", "Passive Income Ahh")

	resp, err := c.httpClient.Do(httpReq)
	elapsed := time.Since(start)
	if err != nil {
		log.Printf("[OpenRouter] Request failed after %v: %v", elapsed, err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	log.Printf("[OpenRouter] Response received in %v (status: %d)", elapsed, resp.StatusCode)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for HTTP errors first
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		// Log the raw response for debugging
		log.Printf("[OpenRouter] Failed to parse response: %v\nRaw response: %s", err, string(respBody))
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if chatResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("no response choices returned")
	}

	result := &ChatResult{
		Content:   chatResp.Choices[0].Message.Content,
		Reasoning: chatResp.Choices[0].Message.Reasoning,
	}

	// Log if reasoning was returned
	if result.Reasoning != "" {
		log.Printf("[OpenRouter] Reasoning received (%d chars)", len(result.Reasoning))
	}

	return result, nil
}

func (c *Client) GetTradingDecision(marketData string) (*TradingDecision, string, error) {
	systemPrompt := `You are a cryptocurrency futures trader AI. Balance profitability with risk management.

## TRADING PHILOSOPHY: BALANCED MODE

Look for QUALITY setups but also trade on MODERATE opportunities when available.

## RESPONSE FORMAT

You MUST respond with ONLY a valid JSON object:

{
  "action": "BUY" | "SELL" | "HOLD" | "CLOSE",
  "symbol": "<EXACT_SYMBOL_FROM_DATA>",
  "confidence": 0-100,
  "reasoning": "Brief explanation",
  "stop_loss_pct": 2.0,
  "take_profit_pct": 6.0
}

## CONFIDENCE DEFINITION (IMPORTANT!)

Confidence = "How sure are you about YOUR recommended action?"

**For BUY/SELL:**
- 75-100: Strong trade setup, take this trade
- 65-74: Moderate setup, trade with caution and tighter stops
- <65: Don't use BUY/SELL, use HOLD instead

**For HOLD:**
- 80-100: Confidently waiting, no good opportunity right now
- 60-79: Uncertain market, better to wait
- <60: Market is confusing, definitely wait

**For CLOSE:**
- 95-100: Only use for significant losses (> -1.5%) or significant profits (> +1.5%)
- <95: DO NOT CLOSE - trust your Stop Loss order
- NEVER close a position that is -1% to +1% (this is normal market noise!)

## ACTION DEFINITIONS

- **BUY** = Open a LONG position (confident price will go UP)
- **SELL** = Open a SHORT position (confident price will go DOWN)  
- **HOLD** = No action. Waiting is the best choice right now.
- **CLOSE** = Close the current position (RARELY USED)

## WHEN TO TRADE (BUY/SELL)

✅ Trade when:
- Entry score is 2/4 or higher (MODERATE or STRONG)
- Trend direction is clear (EMA9 not crossing EMA21)
- RSI is in a safe range (not extreme overbought/oversold)
- You would confidently put money on this direction

❌ Don't trade when:
- Entry score is 0-1/4 (WEAK signals)
- RSI > 75 for LONG or RSI < 25 for SHORT
- EMA9 and EMA21 are crossing or very close
- Multiple conflicting signals

## STOP LOSS & TAKE PROFIT

- For STRONG setups: stop_loss_pct: 2-3%, take_profit_pct: 6-9%
- For MODERATE setups: stop_loss_pct: 1-2%, take_profit_pct: 3-6%
- Always maintain 3:1 reward-to-risk ratio

## CRITICAL: POSITION MANAGEMENT RULES

If you have an existing position:

⚠️ **ALMOST ALWAYS RECOMMEND HOLD** for existing positions!

The SL/TP orders on the exchange will handle exits. You should only recommend CLOSE in rare cases:

✅ CLOSE is OK when:
- Loss > 1.5% AND momentum has completely reversed
- Profit > 1.5% AND you see strong reversal signals

❌ NEVER CLOSE when:
- Position is between -1.5% and +1.5% (NOISE ZONE)
- Position was opened less than 10 minutes ago
- You're just "uncertain" - that's not a reason to close

**Default behavior for existing positions: HOLD**
Trust your stop loss. It's there for a reason.`

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: "Analyze this market data and provide your trading decision:\n\n" + marketData},
	}

	result, err := c.ChatWithReasoning(messages)
	if err != nil {
		return nil, "", fmt.Errorf("AI chat failed: %w", err)
	}

	// Log reasoning if present (from reasoning models like deepseek-r1)
	if result.Reasoning != "" {
		log.Printf("[OpenRouter] AI Reasoning:\n%s", result.Reasoning)
	}

	response := result.Content

	// Parse JSON from response
	var decision TradingDecision
	if err := json.Unmarshal([]byte(response), &decision); err != nil {
		// Try to extract JSON from response if wrapped in markdown
		start := bytes.Index([]byte(response), []byte("{"))
		end := bytes.LastIndex([]byte(response), []byte("}"))
		if start >= 0 && end > start {
			jsonStr := response[start : end+1]
			if err := json.Unmarshal([]byte(jsonStr), &decision); err != nil {
				return nil, response, fmt.Errorf("failed to parse AI decision: %w", err)
			}
		} else {
			return nil, response, fmt.Errorf("no JSON found in response")
		}
	}

	return &decision, response, nil
}

// GetTradingDecisionSimple uses a minimal prompt like v1.4.7
// This is used when Simple Mode is enabled - less overthinking, trust SL/TP
func (c *Client) GetTradingDecisionSimple(marketData string) (*TradingDecision, string, error) {
	systemPrompt := `You are a cryptocurrency futures trader. Make clear BUY, SELL, or HOLD decisions.

RESPONSE FORMAT:
{
  "action": "BUY" | "SELL" | "HOLD",
  "symbol": "<SYMBOL>",
  "confidence": 0-100,
  "reasoning": "Brief explanation",
  "stop_loss_pct": 2.0,
  "take_profit_pct": 6.0
}

ACTIONS:
- BUY = Open LONG, expect price UP
- SELL = Open SHORT, expect price DOWN
- HOLD = No trade, wait

SIMPLE RULES:
1. Trade WITH the trend (EMA direction)
2. Use momentum confirmation (MACD)
3. Avoid extreme RSI (>70 or <30)
4. If unsure, HOLD
5. For existing positions: almost always HOLD, trust your SL/TP`

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: "Analyze and decide:\n\n" + marketData},
	}

	result, err := c.ChatWithReasoning(messages)
	if err != nil {
		return nil, "", fmt.Errorf("AI chat failed: %w", err)
	}

	if result.Reasoning != "" {
		log.Printf("[OpenRouter] AI Reasoning:\n%s", result.Reasoning)
	}

	response := result.Content

	var decision TradingDecision
	if err := json.Unmarshal([]byte(response), &decision); err != nil {
		start := bytes.Index([]byte(response), []byte("{"))
		end := bytes.LastIndex([]byte(response), []byte("}"))
		if start >= 0 && end > start {
			jsonStr := response[start : end+1]
			if err := json.Unmarshal([]byte(jsonStr), &decision); err != nil {
				return nil, response, fmt.Errorf("failed to parse AI decision: %w", err)
			}
		} else {
			return nil, response, fmt.Errorf("no JSON found in response")
		}
	}

	return &decision, response, nil
}
