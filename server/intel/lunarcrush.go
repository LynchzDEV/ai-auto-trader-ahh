package intel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	lunarCrushBaseURL  = "https://lunarcrush.com/api4/public"
	lunarCrushCoinsURL = lunarCrushBaseURL + "/coins/list/v2"
)

// LunarCrushData contains social sentiment data from LunarCrush
type LunarCrushData struct {
	ID               int     `json:"id"`
	Symbol           string  `json:"symbol"`
	Name             string  `json:"name"`
	GalaxyScore      float64 `json:"galaxy_score"`       // 0-100, proprietary sentiment score
	GalaxyScorePrev  float64 `json:"galaxy_score_previous"`
	Sentiment        float64 `json:"sentiment"`          // % of positive posts (0-100)
	AltRank          int     `json:"alt_rank"`           // Relative performance rank
	AltRankPrev      int     `json:"alt_rank_previous"`
	SocialVolume24h  int64   `json:"social_volume_24h"`  // Total social posts
	Interactions24h  int64   `json:"interactions_24h"`   // Total interactions
	SocialDominance  float64 `json:"social_dominance"`   // % of total social volume
	Categories       string  `json:"categories"`
}

// lunarCrushResponse represents the API response
type lunarCrushResponse struct {
	Config struct {
		Generated int64 `json:"generated"`
		TotalRows int   `json:"total_rows"`
	} `json:"config"`
	Data []LunarCrushData `json:"data"`
}

// FetchLunarCrushData fetches social sentiment data for specific coins
// Requires LUNARCRUSH_API_KEY environment variable
func FetchLunarCrushData(ctx context.Context, apiKey string, symbols []string) (map[string]*LunarCrushData, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("LunarCrush API key not configured")
	}

	if len(symbols) == 0 {
		return nil, nil
	}

	// Fetch top coins (includes all major coins)
	url := fmt.Sprintf("%s?limit=100&sort=market_cap_rank", lunarCrushCoinsURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch LunarCrush data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("LunarCrush API key invalid")
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("LunarCrush rate limit exceeded")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LunarCrush API returned status %d", resp.StatusCode)
	}

	var result lunarCrushResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Build map of symbol -> data
	dataMap := make(map[string]*LunarCrushData)
	for i := range result.Data {
		coin := &result.Data[i]
		dataMap[strings.ToUpper(coin.Symbol)] = coin
	}

	// Filter to only requested symbols
	filtered := make(map[string]*LunarCrushData)
	for _, symbol := range symbols {
		// Extract base currency (e.g., BTC from BTCUSDT)
		base := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")
		if data, ok := dataMap[base]; ok {
			filtered[base] = data
		}
	}

	return filtered, nil
}

// FormatLunarCrushData formats LunarCrush data for AI consumption
func FormatLunarCrushData(data map[string]*LunarCrushData, symbols []string) string {
	if len(data) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Social Sentiment (Source: LunarCrush)\n\n")

	for _, symbol := range symbols {
		base := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")
		coin, ok := data[base]
		if !ok {
			continue
		}

		// Galaxy Score analysis
		galaxyTrend := "STABLE"
		if coin.GalaxyScore > coin.GalaxyScorePrev+5 {
			galaxyTrend = "RISING"
		} else if coin.GalaxyScore < coin.GalaxyScorePrev-5 {
			galaxyTrend = "FALLING"
		}

		galaxySignal := "NEUTRAL"
		if coin.GalaxyScore >= 70 {
			galaxySignal = "BULLISH"
		} else if coin.GalaxyScore >= 50 {
			galaxySignal = "SLIGHTLY BULLISH"
		} else if coin.GalaxyScore <= 30 {
			galaxySignal = "BEARISH"
		} else if coin.GalaxyScore <= 50 {
			galaxySignal = "SLIGHTLY BEARISH"
		}

		// Sentiment analysis
		sentimentLabel := "NEUTRAL"
		if coin.Sentiment >= 70 {
			sentimentLabel = "VERY POSITIVE"
		} else if coin.Sentiment >= 55 {
			sentimentLabel = "POSITIVE"
		} else if coin.Sentiment <= 30 {
			sentimentLabel = "VERY NEGATIVE"
		} else if coin.Sentiment <= 45 {
			sentimentLabel = "NEGATIVE"
		}

		// AltRank analysis (lower is better)
		altRankSignal := ""
		if coin.AltRank < coin.AltRankPrev-10 {
			altRankSignal = " [IMPROVING]"
		} else if coin.AltRank > coin.AltRankPrev+10 {
			altRankSignal = " [DECLINING]"
		}

		sb.WriteString(fmt.Sprintf("### %s (%s)\n", base, coin.Name))
		sb.WriteString(fmt.Sprintf("- Galaxy Score: %.0f/100 [%s, %s]\n",
			coin.GalaxyScore, galaxySignal, galaxyTrend))
		sb.WriteString(fmt.Sprintf("- Sentiment: %.0f%% positive [%s]\n",
			coin.Sentiment, sentimentLabel))
		sb.WriteString(fmt.Sprintf("- Social Volume: %d posts (24h)\n", coin.SocialVolume24h))
		sb.WriteString(fmt.Sprintf("- Social Dominance: %.2f%%\n", coin.SocialDominance))
		sb.WriteString(fmt.Sprintf("- AltRank: #%d%s\n", coin.AltRank, altRankSignal))
		sb.WriteString("\n")
	}

	return sb.String()
}
