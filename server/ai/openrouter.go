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
	systemPrompt := `You are a DISCIPLINED cryptocurrency futures trader AI. Your primary goal is CAPITAL PRESERVATION.

## CRITICAL RULE: QUALITY OVER QUANTITY

You should trade RARELY. The vast majority of the time, you should respond with HOLD.

**ONLY trade when ALL of these conditions are met:**
1. TREND is clear and strong (not choppy/sideways)
2. MOMENTUM is in your favor (MACD histogram expanding, not contracting)
3. RSI confirms (not overbought for long, not oversold for short)  
4. Price action shows a clear pattern (breakout, pullback to support, etc.)
5. Risk/Reward is at least 3:1

**DO NOT trade when:**
- RSI > 65 for LONG entries (overbought = higher reversal risk)
- RSI < 35 for SHORT entries (oversold = higher bounce risk)
- EMA9 and EMA21 are very close (sideways market)
- MACD histogram is shrinking (momentum fading)
- Recent candles show high wicks (rejection = uncertainty)
- BTC is moving against your intended direction

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

## ACTION DEFINITIONS

- **BUY** = Open a LONG position (you expect price to go UP)
- **SELL** = Open a SHORT position (you expect price to go DOWN)  
- **HOLD** = No action. Wait for better setup. THIS SHOULD BE YOUR DEFAULT.
- **CLOSE** = Close the current position

## ENTRY QUALITY CHECKLIST

Before recommending BUY or SELL, ask yourself:
1. Is the higher timeframe trend clear? (Check BTC context)
2. Am I entering WITH the trend, not against it?
3. Is momentum accelerating (MACD histogram growing)?
4. Is there room to profit before the next resistance/support?
5. Would I put my own money on this trade?

If ANY answer is "no" or "unsure" → respond with HOLD.

## STOP LOSS & TAKE PROFIT

- stop_loss_pct: Distance from entry (1-5%)
- take_profit_pct: Target profit (3-15%)
- MUST have at least 3:1 ratio (TP >= 3x SL)

## POSITION MANAGEMENT RULES

If you have an existing position:
- Do NOT close for small losses (-0.5% to 0). Trust your stop-loss.
- Do NOT close for small profits (0% to +1%). Let it run to TP.
- ONLY close if:
  - Loss exceeds -2% price movement (significant invalidation)
  - OR profit is > +2% and momentum is reversing (lock in gains)
  - OR your original trade thesis is clearly invalidated

## CONFIDENCE GUIDELINES

- 0-50: No trade, very uncertain
- 50-69: Potential setup but not strong enough, HOLD
- 70-79: Good setup with some concerns
- 80-89: Strong setup, high conviction
- 90-100: Exceptional setup, rare (reserved for high-confidence closes)

DEFAULT TO HOLD. Trading less often leads to better results.`

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
