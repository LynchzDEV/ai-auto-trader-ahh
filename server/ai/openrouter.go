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
	Action     string  `json:"action"`      // BUY, SELL, HOLD, CLOSE
	Symbol     string  `json:"symbol"`      // Trading pair
	Confidence float64 `json:"confidence"`  // 0-100
	Reasoning  string  `json:"reasoning"`   // AI's reasoning
	StopLoss   float64 `json:"stop_loss"`   // Stop loss PRICE (absolute, not %)
	TakeProfit float64 `json:"take_profit"` // Take profit PRICE (absolute, not %)
}

func NewClient(apiKey, model string) *Client {
	return &Client{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *Client) Chat(messages []Message) (string, error) {
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

	httpReq, err := http.NewRequest("POST", OpenRouterBaseURL+"/chat/completions", bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("HTTP-Referer", "https://passive-income-ahh.local")
	httpReq.Header.Set("X-Title", "Passive Income Ahh")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

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
  "stop_loss": 93500.00,
  "take_profit": 96000.00
}

CRITICAL RULES FOR stop_loss AND take_profit:
- These MUST be ABSOLUTE PRICE values, NOT percentages
- For BUY (long): stop_loss < current_price < take_profit
- For SELL (short): take_profit < current_price < stop_loss
- Use the current price from market data to calculate reasonable levels
- Aim for at least 3:1 reward-to-risk ratio
- Example for BUY at $94000: stop_loss=93000, take_profit=97000

Trading Rules:
- BUY: Open a long position (bullish)
- SELL: Open a short position (bearish)
- HOLD: Do nothing, wait for better opportunity
- CLOSE: Close existing position
- Only trade when confidence >= 70
- Consider trend, volume, RSI, MACD, EMA crossovers, and support/resistance levels`

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
