package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is the base AI client implementation
type Client struct {
	config     *Config
	httpClient *http.Client
}

// NewClient creates a new AI client with the given options
func NewClient(opts ...Option) *Client {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Option is a functional option for configuring the client
type Option func(*Config)

// WithAPIKey sets the API key
func WithAPIKey(key string) Option {
	return func(c *Config) {
		c.APIKey = key
	}
}

// WithProvider sets the provider
func WithProvider(provider string) Option {
	return func(c *Config) {
		c.Provider = provider
		if url, ok := DefaultBaseURLs[provider]; ok {
			c.BaseURL = url
		}
	}
}

// WithModel sets the model
func WithModel(model string) Option {
	return func(c *Config) {
		c.Model = model
	}
}

// WithBaseURL sets the base URL
func WithBaseURL(url string) Option {
	return func(c *Config) {
		c.BaseURL = url
	}
}

// WithTimeout sets the timeout
func WithTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.Timeout = timeout
	}
}

// WithMaxRetries sets the max retries
func WithMaxRetries(n int) Option {
	return func(c *Config) {
		c.MaxRetries = n
	}
}

// WithTokenUsageCallback sets the token usage callback
func WithTokenUsageCallback(cb TokenUsageCallback) Option {
	return func(c *Config) {
		c.OnTokenUsage = cb
	}
}

// SetAPIKey implements AIClient
func (c *Client) SetAPIKey(apiKey, customURL, customModel string) {
	c.config.APIKey = apiKey
	if customURL != "" {
		c.config.BaseURL = customURL
	}
	if customModel != "" {
		c.config.Model = customModel
	}
}

// SetTimeout implements AIClient
func (c *Client) SetTimeout(timeout time.Duration) {
	c.config.Timeout = timeout
	c.httpClient.Timeout = timeout
}

// GetProvider implements AIClient
func (c *Client) GetProvider() string {
	return c.config.Provider
}

// GetModel implements AIClient
func (c *Client) GetModel() string {
	return c.config.Model
}

// CallWithMessages implements AIClient
func (c *Client) CallWithMessages(systemPrompt, userPrompt string) (string, error) {
	req := &Request{
		Model: c.config.Model,
		Messages: []Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.7,
		MaxTokens:   4096,
	}
	resp, err := c.CallWithRequest(req)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// CallWithRequest implements AIClient
func (c *Client) CallWithRequest(req *Request) (*Response, error) {
	if req.Model == "" {
		req.Model = c.config.Model
	}

	var lastErr error
	for attempt := 1; attempt <= c.config.MaxRetries; attempt++ {
		resp, err := c.doCall(req)
		if err == nil {
			return resp, nil
		}

		lastErr = err
		if !isRetryableError(err) {
			return nil, err
		}

		if attempt < c.config.MaxRetries {
			time.Sleep(c.config.RetryDelay * time.Duration(attempt))
		}
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// doCall makes a single API call
func (c *Client) doCall(req *Request) (*Response, error) {
	start := time.Now()

	// Build request based on provider
	var httpReq *http.Request
	var err error

	switch c.config.Provider {
	case ProviderAnthropic:
		httpReq, err = c.buildAnthropicRequest(req)
	default:
		// OpenAI-compatible (OpenRouter, OpenAI, DeepSeek, etc.)
		httpReq, err = c.buildOpenAIRequest(req)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	// Make request
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", httpResp.StatusCode, string(body))
	}

	// Parse response based on provider
	var resp *Response
	switch c.config.Provider {
	case ProviderAnthropic:
		resp, err = c.parseAnthropicResponse(body)
	default:
		resp, err = c.parseOpenAIResponse(body)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	resp.Duration = time.Since(start)
	resp.Timestamp = time.Now()
	resp.Provider = c.config.Provider
	resp.Model = req.Model

	// Call token usage callback if set
	if c.config.OnTokenUsage != nil {
		c.config.OnTokenUsage(resp.Usage, resp.Provider, resp.Model)
	}

	return resp, nil
}

// buildOpenAIRequest builds an OpenAI-compatible request
func (c *Client) buildOpenAIRequest(req *Request) (*http.Request, error) {
	payload := map[string]interface{}{
		"model":    req.Model,
		"messages": req.Messages,
	}

	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.TopP > 0 {
		payload["top_p"] = req.TopP
	}
	if len(req.Stop) > 0 {
		payload["stop"] = req.Stop
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := c.config.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	// OpenRouter-specific headers
	if c.config.Provider == ProviderOpenRouter {
		httpReq.Header.Set("HTTP-Referer", "https://auto-trader.local")
		httpReq.Header.Set("X-Title", "Auto Trader")
	}

	return httpReq, nil
}

// buildAnthropicRequest builds an Anthropic request
func (c *Client) buildAnthropicRequest(req *Request) (*http.Request, error) {
	// Anthropic has different format - system is separate
	var systemPrompt string
	var messages []map[string]string

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemPrompt = msg.Content
		} else {
			messages = append(messages, map[string]string{
				"role":    msg.Role,
				"content": msg.Content,
			})
		}
	}

	payload := map[string]interface{}{
		"model":      req.Model,
		"messages":   messages,
		"max_tokens": req.MaxTokens,
	}

	if systemPrompt != "" {
		payload["system"] = systemPrompt
	}
	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if len(req.Stop) > 0 {
		payload["stop_sequences"] = req.Stop
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := c.config.BaseURL + "/messages"
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.config.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	return httpReq, nil
}

// parseOpenAIResponse parses an OpenAI-compatible response
func (c *Client) parseOpenAIResponse(body []byte) (*Response, error) {
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if result.Error.Message != "" {
		return nil, fmt.Errorf("API error: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	return &Response{
		Content: result.Choices[0].Message.Content,
		Usage: Usage{
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
		},
	}, nil
}

// parseAnthropicResponse parses an Anthropic response
func (c *Client) parseAnthropicResponse(body []byte) (*Response, error) {
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if result.Error.Message != "" {
		return nil, fmt.Errorf("API error: %s", result.Error.Message)
	}

	var content string
	for _, c := range result.Content {
		if c.Type == "text" {
			content = c.Text
			break
		}
	}

	return &Response{
		Content: content,
		Usage: Usage{
			PromptTokens:     result.Usage.InputTokens,
			CompletionTokens: result.Usage.OutputTokens,
			TotalTokens:      result.Usage.InputTokens + result.Usage.OutputTokens,
		},
	}, nil
}

// isRetryableError checks if an error is retryable
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	retryablePatterns := []string{
		"eof",
		"timeout",
		"connection reset",
		"connection refused",
		"temporary failure",
		"no such host",
		"stream error",
		"internal_error",
		"rate limit",
		"429",
		"503",
		"502",
	}
	for _, pattern := range retryablePatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}

// NewOpenRouterClient creates a client configured for OpenRouter
func NewOpenRouterClient(apiKey, model string) *Client {
	return NewClient(
		WithProvider(ProviderOpenRouter),
		WithAPIKey(apiKey),
		WithModel(model),
	)
}

// NewDeepSeekClient creates a client configured for DeepSeek
func NewDeepSeekClient(apiKey string) *Client {
	return NewClient(
		WithProvider(ProviderDeepSeek),
		WithAPIKey(apiKey),
		WithModel("deepseek-chat"),
	)
}

// NewAnthropicClient creates a client configured for Anthropic
func NewAnthropicClient(apiKey, model string) *Client {
	return NewClient(
		WithProvider(ProviderAnthropic),
		WithAPIKey(apiKey),
		WithModel(model),
	)
}

// NewOpenAIClient creates a client configured for OpenAI
func NewOpenAIClient(apiKey, model string) *Client {
	return NewClient(
		WithProvider(ProviderOpenAI),
		WithAPIKey(apiKey),
		WithModel(model),
	)
}
