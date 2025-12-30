package mcp

import "time"

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`    // system, user, assistant
	Content string `json:"content"` // message content
}

// Request represents an AI API request
type Request struct {
	Model            string    `json:"model"`
	Messages         []Message `json:"messages"`
	Temperature      float64   `json:"temperature,omitempty"`
	MaxTokens        int       `json:"max_tokens,omitempty"`
	TopP             float64   `json:"top_p,omitempty"`
	FrequencyPenalty float64   `json:"frequency_penalty,omitempty"`
	PresencePenalty  float64   `json:"presence_penalty,omitempty"`
	Stop             []string  `json:"stop,omitempty"`
}

// Usage represents token usage information
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Response represents an AI API response
type Response struct {
	Content   string
	Usage     Usage
	Model     string
	Provider  string
	Duration  time.Duration
	Timestamp time.Time
}

// AIClient is the interface for AI providers
type AIClient interface {
	// SetAPIKey sets the API key and optionally custom URL and model
	SetAPIKey(apiKey, customURL, customModel string)
	// SetTimeout sets the request timeout
	SetTimeout(timeout time.Duration)
	// CallWithMessages makes a simple call with system and user prompts
	CallWithMessages(systemPrompt, userPrompt string) (string, error)
	// CallWithRequest makes a call with full request control
	CallWithRequest(req *Request) (*Response, error)
	// GetProvider returns the provider name
	GetProvider() string
	// GetModel returns the model name
	GetModel() string
}

// TokenUsageCallback is called after each API call with usage info
type TokenUsageCallback func(usage Usage, provider, model string)

// Config holds client configuration
type Config struct {
	APIKey       string
	BaseURL      string
	Model        string
	Provider     string
	Timeout      time.Duration
	MaxRetries   int
	RetryDelay   time.Duration
	OnTokenUsage TokenUsageCallback
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Timeout:    120 * time.Second,
		MaxRetries: 3,
		RetryDelay: 2 * time.Second,
	}
}

// Provider constants
const (
	ProviderOpenRouter = "openrouter"
	ProviderOpenAI     = "openai"
	ProviderAnthropic  = "anthropic"
	ProviderDeepSeek   = "deepseek"
	ProviderGoogle     = "google"
	ProviderQwen       = "qwen"
)

// Default base URLs
var DefaultBaseURLs = map[string]string{
	ProviderOpenRouter: "https://openrouter.ai/api/v1",
	ProviderOpenAI:     "https://api.openai.com/v1",
	ProviderAnthropic:  "https://api.anthropic.com/v1",
	ProviderDeepSeek:   "https://api.deepseek.com/v1",
	ProviderGoogle:     "https://generativelanguage.googleapis.com/v1beta",
	ProviderQwen:       "https://dashscope.aliyuncs.com/api/v1",
}
