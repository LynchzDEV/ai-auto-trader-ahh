package intel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// CryptoPanic free public API (no auth required)
const cryptoPanicURL = "https://cryptopanic.com/api/free/v1/posts/?public=true"

// cryptoPanicResponse represents the API response
type cryptoPanicResponse struct {
	Count   int `json:"count"`
	Results []struct {
		Kind        string `json:"kind"`
		Domain      string `json:"domain"`
		Title       string `json:"title"`
		PublishedAt string `json:"published_at"`
		URL         string `json:"url"`
		Currencies  []struct {
			Code  string `json:"code"`
			Title string `json:"title"`
		} `json:"currencies"`
		Votes struct {
			Positive  int `json:"positive"`
			Negative  int `json:"negative"`
			Important int `json:"important"`
			Liked     int `json:"liked"`
			Disliked  int `json:"disliked"`
		} `json:"votes"`
	} `json:"results"`
}

// FetchNews fetches latest crypto news from CryptoPanic
func FetchNews(ctx context.Context, limit int) ([]NewsItem, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", cryptoPanicURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch news: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("news API returned status %d", resp.StatusCode)
	}

	var result cryptoPanicResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var news []NewsItem
	for i, item := range result.Results {
		if i >= limit {
			break
		}

		published, _ := time.Parse(time.RFC3339, item.PublishedAt)

		// Determine sentiment from votes
		sentiment := "neutral"
		totalVotes := item.Votes.Positive + item.Votes.Negative
		if totalVotes > 0 {
			positiveRatio := float64(item.Votes.Positive) / float64(totalVotes)
			if positiveRatio > 0.6 {
				sentiment = "positive"
			} else if positiveRatio < 0.4 {
				sentiment = "negative"
			}
		}

		// Extract currency codes
		var currencies []string
		for _, c := range item.Currencies {
			currencies = append(currencies, c.Code)
		}

		news = append(news, NewsItem{
			Title:      item.Title,
			Source:     item.Domain,
			URL:        item.URL,
			Published:  published,
			Sentiment:  sentiment,
			Currencies: currencies,
		})
	}

	return news, nil
}

// FilterNewsForSymbols filters news items relevant to specific trading symbols
func FilterNewsForSymbols(news []NewsItem, symbols []string) []NewsItem {
	// Create a set of base currencies from symbols
	baseCurrencies := make(map[string]bool)
	for _, symbol := range symbols {
		// Extract base currency (e.g., BTC from BTCUSDT)
		base := symbol
		if len(symbol) > 4 && symbol[len(symbol)-4:] == "USDT" {
			base = symbol[:len(symbol)-4]
		}
		baseCurrencies[strings.ToUpper(base)] = true
	}

	// Also always include BTC news as it affects the whole market
	baseCurrencies["BTC"] = true

	var filtered []NewsItem
	for _, item := range news {
		// Check if any currency in the news matches our symbols
		for _, currency := range item.Currencies {
			if baseCurrencies[strings.ToUpper(currency)] {
				filtered = append(filtered, item)
				break
			}
		}
	}

	return filtered
}

// FormatNews formats news items for AI consumption
func FormatNews(news []NewsItem, maxItems int) string {
	if len(news) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Recent Crypto News\n\n")

	count := 0
	for _, item := range news {
		if count >= maxItems {
			break
		}

		// Format time ago
		timeAgo := formatTimeAgo(item.Published)

		// Sentiment indicator
		sentimentIcon := "📰"
		switch item.Sentiment {
		case "positive":
			sentimentIcon = "📈"
		case "negative":
			sentimentIcon = "📉"
		}

		// Currencies mentioned
		currencyStr := ""
		if len(item.Currencies) > 0 {
			currencyStr = fmt.Sprintf(" [%s]", strings.Join(item.Currencies, ", "))
		}

		sb.WriteString(fmt.Sprintf("- %s %s%s (%s, %s)\n",
			sentimentIcon, item.Title, currencyStr, item.Source, timeAgo))
		count++
	}
	sb.WriteString("\n")

	return sb.String()
}

// formatTimeAgo formats a time as "X ago" string
func formatTimeAgo(t time.Time) string {
	diff := time.Since(t)

	if diff < time.Minute {
		return "just now"
	} else if diff < time.Hour {
		mins := int(diff.Minutes())
		return fmt.Sprintf("%dm ago", mins)
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		return fmt.Sprintf("%dh ago", hours)
	} else {
		days := int(diff.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
}
