package decision

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Regex patterns for parsing
var (
	reReasoningTag   = regexp.MustCompile(`(?s)<reasoning>(.*?)</reasoning>`)
	reDecisionTag    = regexp.MustCompile(`(?s)<decision>(.*?)</decision>`)
	reJSONFence      = regexp.MustCompile("(?s)```(?:json)?\\s*([\\s\\S]*?)```")
	reJSONArray      = regexp.MustCompile(`(?s)\[[\s\S]*\]`)
	reArrayHead      = regexp.MustCompile(`^\s*\[\s*\{`)
	reArrayOpenSpace = regexp.MustCompile(`^\[\s+\{`)
	reInvisibleRunes = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`)
)

// ParseFullDecisionResponse parses AI response into decisions
func ParseFullDecisionResponse(aiResponse string, cfg *ValidationConfig) (*FullDecision, error) {
	cotTrace := extractCoTTrace(aiResponse)

	decisions, err := extractDecisions(aiResponse)
	if err != nil {
		return &FullDecision{
			CoTTrace:  cotTrace,
			Decisions: []Decision{},
		}, fmt.Errorf("failed to extract decisions: %w", err)
	}

	if err := ValidateDecisions(decisions, cfg); err != nil {
		return &FullDecision{
			CoTTrace:  cotTrace,
			Decisions: decisions,
		}, fmt.Errorf("decision validation failed: %w", err)
	}

	return &FullDecision{
		CoTTrace:  cotTrace,
		Decisions: decisions,
	}, nil
}

// extractCoTTrace extracts chain of thought from AI response
func extractCoTTrace(response string) string {
	// Try <reasoning> tags first
	if match := reReasoningTag.FindStringSubmatch(response); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}

	// Try content before <decision> tag
	if decisionIdx := strings.Index(response, "<decision>"); decisionIdx > 0 {
		return strings.TrimSpace(response[:decisionIdx])
	}

	// Fallback: content before [ character
	jsonStart := strings.Index(response, "[")
	if jsonStart > 0 {
		return strings.TrimSpace(response[:jsonStart])
	}

	return strings.TrimSpace(response)
}

// extractDecisions extracts JSON decisions from AI response
func extractDecisions(response string) ([]Decision, error) {
	s := removeInvisibleRunes(response)
	s = strings.TrimSpace(s)
	s = fixMissingQuotes(s)

	var jsonPart string

	// Try <decision> tag first
	if match := reDecisionTag.FindStringSubmatch(s); len(match) > 1 {
		jsonPart = strings.TrimSpace(match[1])
	} else {
		jsonPart = s
	}

	jsonPart = fixMissingQuotes(jsonPart)

	// Try code fence extraction
	if m := reJSONFence.FindStringSubmatch(jsonPart); len(m) > 1 {
		jsonContent := strings.TrimSpace(m[1])
		jsonContent = compactArrayOpen(jsonContent)
		jsonContent = fixMissingQuotes(jsonContent)

		if err := validateJSONFormat(jsonContent); err != nil {
			return nil, fmt.Errorf("JSON format validation failed: %w\nJSON: %s", err, truncate(jsonContent, 200))
		}

		var decisions []Decision
		if err := json.Unmarshal([]byte(jsonContent), &decisions); err != nil {
			return nil, fmt.Errorf("JSON parsing failed: %w\nJSON: %s", err, truncate(jsonContent, 200))
		}
		return decisions, nil
	}

	// Fallback to raw array extraction
	jsonContent := strings.TrimSpace(reJSONArray.FindString(jsonPart))
	if jsonContent == "" {
		// Safe fallback - AI didn't output JSON decision
		cotSummary := jsonPart
		if len(cotSummary) > 240 {
			cotSummary = cotSummary[:240] + "..."
		}

		fallbackDecision := Decision{
			Symbol:    "ALL",
			Action:    ActionWait,
			Reasoning: fmt.Sprintf("Model didn't output structured JSON decision, entering safe wait; summary: %s", cotSummary),
		}
		return []Decision{fallbackDecision}, nil
	}

	jsonContent = compactArrayOpen(jsonContent)
	jsonContent = fixMissingQuotes(jsonContent)

	if err := validateJSONFormat(jsonContent); err != nil {
		return nil, fmt.Errorf("JSON format validation failed: %w\nJSON: %s", err, truncate(jsonContent, 200))
	}

	var decisions []Decision
	if err := json.Unmarshal([]byte(jsonContent), &decisions); err != nil {
		return nil, fmt.Errorf("JSON parsing failed: %w\nJSON: %s", err, truncate(jsonContent, 200))
	}

	return decisions, nil
}

// fixMissingQuotes fixes common quote and bracket issues from AI output
func fixMissingQuotes(jsonStr string) string {
	// Curly quotes to straight quotes
	jsonStr = strings.ReplaceAll(jsonStr, "\u201c", "\"") // "
	jsonStr = strings.ReplaceAll(jsonStr, "\u201d", "\"") // "
	jsonStr = strings.ReplaceAll(jsonStr, "\u2018", "'")  // '
	jsonStr = strings.ReplaceAll(jsonStr, "\u2019", "'")  // '

	// Full-width brackets and punctuation
	jsonStr = strings.ReplaceAll(jsonStr, "［", "[")
	jsonStr = strings.ReplaceAll(jsonStr, "］", "]")
	jsonStr = strings.ReplaceAll(jsonStr, "｛", "{")
	jsonStr = strings.ReplaceAll(jsonStr, "｝", "}")
	jsonStr = strings.ReplaceAll(jsonStr, "：", ":")
	jsonStr = strings.ReplaceAll(jsonStr, "，", ",")

	// Chinese brackets
	jsonStr = strings.ReplaceAll(jsonStr, "【", "[")
	jsonStr = strings.ReplaceAll(jsonStr, "】", "]")
	jsonStr = strings.ReplaceAll(jsonStr, "〔", "[")
	jsonStr = strings.ReplaceAll(jsonStr, "〕", "]")
	jsonStr = strings.ReplaceAll(jsonStr, "、", ",")

	// Full-width spaces
	jsonStr = strings.ReplaceAll(jsonStr, "　", " ")

	return jsonStr
}

// validateJSONFormat validates JSON structure before parsing
func validateJSONFormat(jsonStr string) error {
	trimmed := strings.TrimSpace(jsonStr)

	// Must start with [{
	if !reArrayHead.MatchString(trimmed) {
		if strings.HasPrefix(trimmed, "[") && !strings.Contains(trimmed[:min(20, len(trimmed))], "{") {
			return fmt.Errorf("not a valid decision array (must contain objects {}), actual: %s", truncate(trimmed, 50))
		}
		return fmt.Errorf("JSON must start with [{ (whitespace allowed), actual: %s", truncate(trimmed, 20))
	}

	// No range symbols
	if strings.Contains(jsonStr, "~") {
		return fmt.Errorf("JSON cannot contain range symbol ~, all numbers must be precise single values")
	}

	// No thousand separators in numbers (e.g., 1,000)
	for i := 0; i < len(jsonStr)-4; i++ {
		if jsonStr[i] >= '0' && jsonStr[i] <= '9' &&
			jsonStr[i+1] == ',' &&
			jsonStr[i+2] >= '0' && jsonStr[i+2] <= '9' &&
			jsonStr[i+3] >= '0' && jsonStr[i+3] <= '9' &&
			jsonStr[i+4] >= '0' && jsonStr[i+4] <= '9' {
			return fmt.Errorf("JSON numbers cannot contain thousand separator comma, found: %s", jsonStr[i:min(i+10, len(jsonStr))])
		}
	}

	return nil
}

// removeInvisibleRunes removes zero-width and other invisible Unicode characters
func removeInvisibleRunes(s string) string {
	return reInvisibleRunes.ReplaceAllString(s, "")
}

// compactArrayOpen normalizes array opening spacing
func compactArrayOpen(s string) string {
	return reArrayOpenSpace.ReplaceAllString(strings.TrimSpace(s), "[{")
}

// truncate truncates a string to max length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// min returns minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
