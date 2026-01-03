package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
			Role    string `json:"role"`
			Content string `json:"content"`
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
	return &Client{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 180 * time.Second, // 3 minutes for slower models
		},
	}
}

func (c *Client) Chat(messages []Message) (string, error) {
	start := time.Now()

	req := ChatRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   4096,
		Temperature: 0.7,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Log prompt size for debugging
	promptSize := 0
	for _, m := range messages {
		promptSize += len(m.Content)
	}
	log.Printf("[OpenRouter] Sending request to %s (prompt size: %d chars, model: %s)", c.model, promptSize, c.model)

	httpReq, err := http.NewRequest("POST", OpenRouterBaseURL+"/chat/completions", bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("HTTP-Referer", "https://passive-income-ahh.local")
	httpReq.Header.Set("X-Title", "Passive Income Ahh")

	resp, err := c.httpClient.Do(httpReq)
	elapsed := time.Since(start)
	if err != nil {
		log.Printf("[OpenRouter] Request failed after %v: %v", elapsed, err)
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	log.Printf("[OpenRouter] Response received in %v (status: %d)", elapsed, resp.StatusCode)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check for HTTP errors first
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		// Log the raw response for debugging
		log.Printf("[OpenRouter] Failed to parse response: %v\nRaw response: %s", err, string(respBody))
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no response choices returned")
	}

	return chatResp.Choices[0].Message.Content, nil
}

func (c *Client) GetTradingDecision(marketData string) (*TradingDecision, string, error) {
	systemPrompt := `You are an expert cryptocurrency futures trader AI. Analyze the market data and make trading decisions.

IMPORTANT: You must respond with ONLY a valid JSON object, no other text.

Response format:
{
  "action": "BUY" | "SELL" | "HOLD" | "CLOSE",
  "symbol": "BTCUSDT",
  "confidence": 0-100,
  "reasoning": "Brief explanation of your decision",
  "stop_loss_pct": 2.0,
  "take_profit_pct": 6.0
}

CRITICAL RULES FOR stop_loss_pct AND take_profit_pct:
- These are PERCENTAGES from entry price (e.g., 2.0 means 2%)
- stop_loss_pct: How far price can move against you before stopping out (1-5%)
- take_profit_pct: Target profit percentage (3-15%)
- MUST maintain at least 3:1 reward-to-risk ratio (take_profit_pct >= 3 * stop_loss_pct)
- Example: stop_loss_pct=2.0, take_profit_pct=6.0 gives 3:1 ratio

Trading Rules:
- BUY: Open a long position (bullish)
- SELL: Open a short position (bearish)
- HOLD: Do nothing, wait for better opportunity
- CLOSE: Close existing position
- Only trade when confidence >= 70
- Consider trend, volume, RSI, MACD, EMA crossovers, and support/resistance levels
- Higher volatility (ATR) = wider stops needed

CRITICAL POSITION MANAGEMENT RULES:
- NEVER recommend CLOSE if the position is at a loss (negative PnL) - let the stop-loss handle it
- NEVER recommend CLOSE if profit is less than 3% - let the take-profit order do its job
- Only recommend CLOSE for early profit-taking when profit is ABOVE 3% AND there's a clear reversal signal
- The stop-loss and take-profit orders are already placed on the exchange at 2% and 6% respectively
- Trust the exchange orders to manage exits - your job is to find ENTRY points, not micromanage exits
- HOLD positions and let them develop - don't close after 5 minutes just because of minor price fluctuations
- A position should typically be held for at least 30-60 minutes unless there's a major market reversal
- Stop over-trading: if you just opened or closed a position, HOLD for the next few cycles`

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: "Analyze this market data and provide your trading decision:\n\n" + marketData},
	}

	response, err := c.Chat(messages)
	if err != nil {
		return nil, "", fmt.Errorf("AI chat failed: %w", err)
	}

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
