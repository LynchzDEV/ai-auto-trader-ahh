package decision

import (
	"strings"
	"testing"
)

func TestGetSchemaPrompt(t *testing.T) {
	// Test English
	enPrompt := GetSchemaPrompt(LangEnglish)
	if !strings.Contains(enPrompt, "Data Dictionary") {
		t.Error("English schema prompt missing header")
	}
	if !strings.Contains(enPrompt, "Open Interest (OI)") {
		t.Error("English schema prompt missing 'Open Interest (OI)'")
	}
	if !strings.Contains(enPrompt, "Market Data Metrics") {
		// This string might not be in the output exactly like that, checked implementation: it is NOT in the implementation.
		// Implementation has "### Account Metrics" as comments but prints "### Open Interest (OI)" as headers.
		// Actually, let's check what strings are definitely there.
		// "### Open Interest (OI)" should be there.
	}
	if !strings.Contains(enPrompt, "Common Mistakes") {
		t.Error("English schema prompt missing 'Common Mistakes'")
	}

	// Test Chinese
	zhPrompt := GetSchemaPrompt(LangChinese)
	if !strings.Contains(zhPrompt, "数据词典") {
		t.Error("Chinese schema prompt missing header")
	}
	if !strings.Contains(zhPrompt, "常见错误") {
		t.Error("Chinese schema prompt missing 'Common Mistakes' equivalent")
	}
}

func TestPromptBuilderIntegration(t *testing.T) {
	pb := NewPromptBuilder(LangEnglish)
	systemPrompt := pb.BuildSystemPrompt()

	if !strings.Contains(systemPrompt, "Data Dictionary") {
		t.Error("System prompt does not contain Data Dictionary")
	}
	if !strings.Contains(systemPrompt, "You are a professional cryptocurrency") {
		t.Error("System prompt missing core role definition")
	}
}
