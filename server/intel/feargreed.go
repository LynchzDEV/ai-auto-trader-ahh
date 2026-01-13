package intel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const fearGreedURL = "https://api.alternative.me/fng/?limit=1"

// fearGreedResponse represents the API response
type fearGreedResponse struct {
	Name string `json:"name"`
	Data []struct {
		Value               string `json:"value"`
		ValueClassification string `json:"value_classification"`
		Timestamp           string `json:"timestamp"`
		TimeUntilUpdate     string `json:"time_until_update"`
	} `json:"data"`
	Metadata struct {
		Error string `json:"error"`
	} `json:"metadata"`
}

// FetchFearGreed fetches the current Fear & Greed Index
func FetchFearGreed(ctx context.Context) (*FearGreedData, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fearGreedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch fear & greed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fear & greed API returned status %d", resp.StatusCode)
	}

	var result fearGreedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("no fear & greed data returned")
	}

	data := result.Data[0]
	value, _ := strconv.Atoi(data.Value)
	timestamp, _ := strconv.ParseInt(data.Timestamp, 10, 64)

	return &FearGreedData{
		Value:           value,
		Classification:  data.ValueClassification,
		Timestamp:       time.Unix(timestamp, 0),
		TimeUntilUpdate: data.TimeUntilUpdate,
	}, nil
}

// FormatFearGreed formats Fear & Greed data for AI consumption
func FormatFearGreed(fg *FearGreedData) string {
	if fg == nil {
		return ""
	}

	emoji := "😐"
	warning := ""
	switch {
	case fg.Value <= 20:
		emoji = "😱"
		warning = "EXTREME FEAR - Potential buying opportunity, but high risk of further drops"
	case fg.Value <= 40:
		emoji = "😰"
		warning = "FEAR - Market is cautious, potential accumulation zone"
	case fg.Value <= 60:
		emoji = "😐"
		warning = "NEUTRAL - Market sentiment is balanced"
	case fg.Value <= 80:
		emoji = "😀"
		warning = "GREED - Market is optimistic, be cautious of overextension"
	default:
		emoji = "🤑"
		warning = "EXTREME GREED - High risk of correction, consider taking profits"
	}

	return fmt.Sprintf(`## Market Sentiment (Fear & Greed Index)
- Value: %d/100 %s
- Classification: %s
- Signal: %s
`, fg.Value, emoji, fg.Classification, warning)
}
