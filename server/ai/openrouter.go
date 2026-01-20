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

## LEARNING FROM HISTORY (CRITICAL)

You will receive historical data for the specific symbol and your account's worst performers.
1. **CHECK 'Account Worst Performers'**: If the symbol is on this list, be EXTREMELY skeptical. Do NOT force a trade unless the setup is perfect (Confidence > 85).
2. **CHECK 'Recent 24h Trading History'**:
   - If previous trades were losses, understand WHY (e.g., "SL_HIT"). Do not repeat the same mistake.
   - If the strategy failed recently on this symbol, require higher confidence to try again.
   - You MUST reference this history in your "reasoning" (e.g., "Avoiding due to recent losses...", "Trying again as conditions changed...").

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
- When you think a position should be closed, provide clear reasoning
- Explain WHY you believe the thesis has failed or profits should be locked
- The system will evaluate your reasoning and confidence level

## ACTION DEFINITIONS

- **BUY** = Open a LONG position (confident price will go UP)
- **SELL** = Open a SHORT position (confident price will go DOWN)  
- **HOLD** = No action. Waiting is the best choice right now.
- **CLOSE** = Close the current position (RARELY USED)

## TIMING AWARENESS (Factor into your analysis)

Consider the 24h price change when entering:
- Large move already happened (>4-5% up) → You may be late. Momentum can continue, but reversal risk is HIGHER.
- Large move already happened (>4-5% down) → Bounce risk is higher if shorting.
- This is NOT a prohibition. Strong momentum can push further. But factor timing into your confidence and SL sizing.
- If entering after extended moves, consider tighter stop-loss (1-1.5%) to protect against reversal.

## PRICE ACTION CONTEXT (Read the candles)

Use the candle data to understand market sentiment:
- Rejection wicks (large upper wick after a pump) = sellers pushing back, momentum may be slowing
- Rejection wicks (large lower wick after a dump) = buyers stepping in, selling may be exhausted
- Price near recent high (within 1%) = potential resistance, could reject
- Price near recent low (within 1%) = potential support, could bounce
- [HIGH WICK ⚠️] labels in candle data = significant rejection, weigh this in your analysis

These are context clues to improve your judgment, not commands. Combine with indicators.

## WHEN TO TRADE (BUY/SELL)

✅ Trade when:
- Entry score is 2/4 or higher (MODERATE or STRONG)
- Trend direction is clear (EMA9 not crossing EMA21)
- RSI is in a safe range (not extreme overbought/oversold)
- You would confidently put money on this direction
- Timing looks reasonable (not chasing an exhausted move)

❌ Don't trade when:
- Entry score is 0-1/4 (WEAK signals)
- RSI > 75 for LONG or RSI < 25 for SHORT
- EMA9 and EMA21 are crossing or very close
- Multiple conflicting signals
- Price shows rejection patterns against your intended direction

## STOP LOSS & TAKE PROFIT

- For STRONG setups: stop_loss_pct: 2-3%, take_profit_pct: 6-9%
- For MODERATE setups: stop_loss_pct: 1-2%, take_profit_pct: 3-6%
- Always maintain 3:1 reward-to-risk ratio

## CRITICAL: POSITION MANAGEMENT RULES

If you have an existing position:

**Provide your analysis FIRST, then your recommendation**

When considering CLOSE, always explain:
1. What has changed since entry?
2. Has the trade thesis been invalidated?
3. What specific signals support closing?

✅ CLOSE is appropriate when:
- Trade thesis is clearly invalidated (e.g., support/resistance broken)
- Momentum has completely reversed against position
- Significant profits at risk of reversal

For close decisions: Provide clear reasoning about why the position should be closed.
The system will evaluate your insight and confidence level.`

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
// This is used when Simple Mode is enabled - less overthinking
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
5. For existing positions: provide your analysis first if you think action is needed

TIMING & CONTEXT:
- If 24h change is large (>4-5%), consider if you're chasing. Not prohibited, but factor into confidence.
- Watch for rejection wicks (large wicks after a move) = momentum may be fading.
- Use tighter SL (1-1.5%) if entering after extended moves.`

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

// EntryAssessment represents AI's assessment of entry quality (for Phase 2 signal/trigger system)
type EntryAssessment struct {
	Symbol        string   `json:"symbol"`
	Direction     string   `json:"direction"`     // "LONG", "SHORT", or "NONE"
	EntryQuality  string   `json:"entry_quality"` // "EXCELLENT", "GOOD", "WAIT", "AVOID"
	Confidence    int      `json:"confidence"`
	WaitFor       []string `json:"wait_for"`       // Trigger conditions to wait for (e.g., "pullback_to_ema", "rsi_below_45")
	Reasoning     string   `json:"reasoning"`
	StopLossPct   float64  `json:"stop_loss_pct"`
	TakeProfitPct float64  `json:"take_profit_pct"`
}

// GetEntryAssessment uses a simplified prompt focused ONLY on entry quality
// This reduces the "distraction effect" from conflicting instructions
// Reference: https://pmc.ncbi.nlm.nih.gov/articles/PMC12421730/
func (c *Client) GetEntryAssessment(marketData string) (*EntryAssessment, string, error) {
	// Simplified prompt - under 50 lines, focused ONLY on entry quality
	systemPrompt := `You assess ENTRY QUALITY for crypto futures trades. One job: "Is NOW a good time to enter?"

## YOUR ONLY JOB
Evaluate if current price is a GOOD ENTRY POINT. You do NOT manage positions.

## ENTRY QUALITY RATINGS

**EXCELLENT** (confidence 85+): Enter immediately
- Price at/near EMA support (for LONG) or resistance (for SHORT)
- Move Maturity is EARLY or MID
- Momentum confirming direction
- RSI not extreme

**GOOD** (confidence 70-84): Enter with caution
- Decent setup but not perfect
- Some confirmation missing
- Acceptable risk/reward

**WAIT** (confidence 50-69): Signal exists, but wait for better entry
- Trend identified but price extended from EMA
- Move Maturity is LATE - wait for pullback
- Use "wait_for" to specify what to wait for

**AVOID** (confidence <50): No trade
- Conflicting signals
- Move EXHAUSTED
- Against trend

## KEY INDICATORS TO CHECK
1. Move Maturity (EARLY=best, EXHAUSTED=avoid)
2. Distance from EMA9 (closer=better, >1.5% extended=wait for pullback)
3. RSI (40-60 ideal for entry, extremes = caution)
4. MACD direction (should confirm your direction)

## RESPONSE FORMAT (JSON only)
{
  "symbol": "BTCUSDT",
  "direction": "LONG|SHORT|NONE",
  "entry_quality": "EXCELLENT|GOOD|WAIT|AVOID",
  "confidence": 75,
  "wait_for": ["pullback_to_ema9", "rsi_below_45"],
  "reasoning": "Brief 1-2 sentence explanation",
  "stop_loss_pct": 3.0,
  "take_profit_pct": 9.0
}

## WAIT_FOR OPTIONS (use when entry_quality is WAIT)
- "pullback_to_ema9" - wait for price to retrace to EMA9
- "pullback_to_ema21" - wait for deeper pullback to EMA21
- "rsi_below_X" - wait for RSI to drop below X (e.g., "rsi_below_45")
- "rsi_above_X" - wait for RSI to rise above X
- "bullish_candle" - wait for bullish confirmation candle
- "bearish_candle" - wait for bearish confirmation candle
- "volume_increase" - wait for volume spike confirmation
- "macd_cross" - wait for MACD line to cross signal`

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: "Assess entry quality:\n\n" + marketData},
	}

	result, err := c.ChatWithReasoning(messages)
	if err != nil {
		return nil, "", fmt.Errorf("AI chat failed: %w", err)
	}

	if result.Reasoning != "" {
		log.Printf("[OpenRouter] Entry Assessment Reasoning:\n%s", result.Reasoning)
	}

	response := result.Content

	var assessment EntryAssessment
	if err := json.Unmarshal([]byte(response), &assessment); err != nil {
		start := bytes.Index([]byte(response), []byte("{"))
		end := bytes.LastIndex([]byte(response), []byte("}"))
		if start >= 0 && end > start {
			jsonStr := response[start : end+1]
			if err := json.Unmarshal([]byte(jsonStr), &assessment); err != nil {
				return nil, response, fmt.Errorf("failed to parse entry assessment: %w", err)
			}
		} else {
			return nil, response, fmt.Errorf("no JSON found in response")
		}
	}

	return &assessment, response, nil
}
