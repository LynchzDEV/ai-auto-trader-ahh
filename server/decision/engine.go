package decision

import (
	"fmt"
	"log"
	"time"

	"auto-trader-ahh/mcp"
)

// Engine is the decision making engine that uses AI to make trading decisions
type Engine struct {
	client        mcp.AIClient
	promptBuilder *PromptBuilder
	validationCfg *ValidationConfig
	lang          Language
}

// NewEngine creates a new decision engine
func NewEngine(client mcp.AIClient, lang Language) *Engine {
	return &Engine{
		client:        client,
		promptBuilder: NewPromptBuilder(lang),
		validationCfg: DefaultValidationConfig(),
		lang:          lang,
	}
}

// SetValidationConfig sets custom validation configuration
func (e *Engine) SetValidationConfig(cfg *ValidationConfig) {
	e.validationCfg = cfg
}

// UpdateValidationFromContext updates validation config from context
func (e *Engine) UpdateValidationFromContext(ctx *Context) {
	e.validationCfg.AccountEquity = ctx.Account.TotalEquity
	e.validationCfg.BTCETHLeverage = ctx.BTCETHLeverage
	e.validationCfg.AltcoinLeverage = ctx.AltcoinLeverage
	e.validationCfg.BTCETHPosRatio = ctx.BTCETHPosRatio
	e.validationCfg.AltcoinPosRatio = ctx.AltcoinPosRatio
}

// MakeDecision calls the AI to make a trading decision
func (e *Engine) MakeDecision(ctx *Context) (*FullDecision, error) {
	// Update validation config from context
	e.UpdateValidationFromContext(ctx)

	// Build prompts
	systemPrompt := e.promptBuilder.BuildSystemPrompt()
	userPrompt := e.promptBuilder.BuildUserPrompt(ctx)

	// Call AI
	start := time.Now()

	req := &mcp.Request{
		Model: e.client.GetModel(),
		Messages: []mcp.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.7,
		MaxTokens:   4096,
		Stream:      true,
	}

	fmt.Printf("[Decision] Requesting AI (streaming)... ")

	// Stream handler to accumulate chunks and print them
	var fullResponse string
	handler := func(chunk string) error {
		fullResponse += chunk
		// Optional: Print chunks to stdout if you want to see them as they come
		// fmt.Print(chunk)
		return nil
	}

	responseObj, err := e.client.CallStream(req, handler)
	duration := time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// Use content from response object which is built from stream
	response := responseObj.Content

	fmt.Printf("Done in %v\n", duration)

	// Parse response
	fullDecision, parseErr := ParseFullDecisionResponse(response, e.validationCfg)
	if parseErr != nil {
		log.Printf("WARNING: Decision parsing/validation error: %v", parseErr)
		// Still return what we parsed, but with the error
	}

	// Fill in metadata
	fullDecision.SystemPrompt = systemPrompt
	fullDecision.UserPrompt = userPrompt
	fullDecision.RawResponse = response
	fullDecision.Timestamp = time.Now()
	fullDecision.AIRequestDurationMs = duration.Milliseconds()

	return fullDecision, parseErr
}

// MakeDecisionWithRetry makes a decision with retry logic
func (e *Engine) MakeDecisionWithRetry(ctx *Context, maxRetries int) (*FullDecision, error) {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		fullDecision, err := e.MakeDecision(ctx)
		if err == nil {
			return fullDecision, nil
		}

		lastErr = err
		log.Printf("Decision attempt %d/%d failed: %v", attempt, maxRetries, err)

		if attempt < maxRetries {
			// Wait before retry with exponential backoff
			waitTime := time.Duration(attempt*2) * time.Second
			time.Sleep(waitTime)
		}
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// FilterActionableDecisions filters out hold/wait decisions
func FilterActionableDecisions(decisions []Decision) []Decision {
	var actionable []Decision
	for _, d := range decisions {
		if !IsPassiveAction(d.Action) {
			actionable = append(actionable, d)
		}
	}
	return actionable
}

// FilterOpeningDecisions returns only opening decisions
func FilterOpeningDecisions(decisions []Decision) []Decision {
	var opening []Decision
	for _, d := range decisions {
		if IsOpeningAction(d.Action) {
			opening = append(opening, d)
		}
	}
	return opening
}

// FilterClosingDecisions returns only closing decisions
func FilterClosingDecisions(decisions []Decision) []Decision {
	var closing []Decision
	for _, d := range decisions {
		if IsClosingAction(d.Action) {
			closing = append(closing, d)
		}
	}
	return closing
}

// GetDecisionsBySymbol returns decisions for a specific symbol
func GetDecisionsBySymbol(decisions []Decision, symbol string) []Decision {
	var result []Decision
	for _, d := range decisions {
		if d.Symbol == symbol {
			result = append(result, d)
		}
	}
	return result
}

// GetHighConfidenceDecisions returns decisions above a confidence threshold
func GetHighConfidenceDecisions(decisions []Decision, minConfidence int) []Decision {
	var result []Decision
	for _, d := range decisions {
		if d.Confidence >= minConfidence {
			result = append(result, d)
		}
	}
	return result
}

// SummarizeDecisions returns a human-readable summary of decisions
func SummarizeDecisions(decisions []Decision) string {
	if len(decisions) == 0 {
		return "No decisions"
	}

	if len(decisions) == 1 && decisions[0].Action == ActionWait {
		return "WAIT: " + decisions[0].Reasoning
	}

	var summary string
	for i, d := range decisions {
		if i > 0 {
			summary += " | "
		}
		switch d.Action {
		case ActionOpenLong:
			summary += fmt.Sprintf("LONG %s $%.0f @%dx", d.Symbol, d.PositionSizeUSD, d.Leverage)
		case ActionOpenShort:
			summary += fmt.Sprintf("SHORT %s $%.0f @%dx", d.Symbol, d.PositionSizeUSD, d.Leverage)
		case ActionCloseLong:
			summary += fmt.Sprintf("CLOSE LONG %s", d.Symbol)
		case ActionCloseShort:
			summary += fmt.Sprintf("CLOSE SHORT %s", d.Symbol)
		case ActionHold:
			summary += fmt.Sprintf("HOLD %s", d.Symbol)
		case ActionWait:
			summary += "WAIT"
		}
	}

	return summary
}
