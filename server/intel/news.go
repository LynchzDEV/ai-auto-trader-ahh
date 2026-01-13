package intel

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Google News RSS Feed Base
const googleNewsRSSBase = "https://news.google.com/rss/search?hl=en-US&gl=US&ceid=US:en&q="

type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	PubDate string `xml:"pubDate"`
	Source  string `xml:"source"`
}

// FetchNews fetches latest crypto news from Google News RSS
func FetchNews(ctx context.Context, query string, limit int) ([]NewsItem, error) {
	// If no query provided, default to general crypto market
	if query == "" {
		query = "cryptocurrency+trading+market"
	}

	// URL Encode the query (basic replacement for now)
	encodedQuery := strings.ReplaceAll(query, " ", "+")
	url := googleNewsRSSBase + encodedQuery

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; TradingBot/1.0)")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch news: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("news feed returned status %d", resp.StatusCode)
	}

	var feed rssFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("failed to decode RSS: %w", err)
	}

	var news []NewsItem
	for i, item := range feed.Channel.Items {
		if i >= limit {
			break
		}

		// Parse time (RFC1123 is standard for RSS)
		published, err := time.Parse(time.RFC1123, item.PubDate)
		if err != nil {
			// Try alternative formats if needed, or just use current time fallback
			published = time.Now()
		}

		// Clean title ("Title - Source" -> "Title")
		title := item.Title
		source := item.Source
		if idx := strings.LastIndex(title, " - "); idx != -1 {
			if source == "" {
				source = title[idx+3:]
			}
			title = title[:idx]
		}

		// Basic currency detection in title
		var currencies []string
		upperTitle := strings.ToUpper(title)
		commonCoins := []string{"BTC", "ETH", "SOL", "XRP", "BNB"}
		for _, coin := range commonCoins {
			if strings.Contains(upperTitle, coin) {
				currencies = append(currencies, coin)
			}
		}

		news = append(news, NewsItem{
			Title:      title,
			Source:     source,
			URL:        item.Link,
			Published:  published,
			Sentiment:  "neutral", // AI will determine sentiment from title
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
