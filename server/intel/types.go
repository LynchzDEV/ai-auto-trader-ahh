package intel

import "time"

// MarketIntel contains aggregated market intelligence data
type MarketIntel struct {
	FetchedAt     time.Time                   `json:"fetched_at"`
	FearGreed     *FearGreedData              `json:"fear_greed,omitempty"`
	News          []NewsItem                  `json:"news,omitempty"`
	CoinData      map[string]*CoinInfo        `json:"coin_data,omitempty"`
	GlobalData    *GlobalMarketData           `json:"global_data,omitempty"`
	SocialData    map[string]*LunarCrushData  `json:"social_data,omitempty"`     // LunarCrush sentiment
	TechAnalysis  map[string]*TradingViewData `json:"tech_analysis,omitempty"`   // TradingView ratings
	CoinIDMapping map[string]string           `json:"coin_id_mapping,omitempty"` // Mapping from symbol -> CoinGecko ID
}

// FearGreedData represents the Fear & Greed Index
type FearGreedData struct {
	Value           int       `json:"value"`                // 0-100
	Classification  string    `json:"value_classification"` // Extreme Fear, Fear, Neutral, Greed, Extreme Greed
	Timestamp       time.Time `json:"timestamp"`
	TimeUntilUpdate string    `json:"time_until_update,omitempty"`
}

// NewsItem represents a crypto news headline
type NewsItem struct {
	Title      string    `json:"title"`
	Source     string    `json:"source"`
	URL        string    `json:"url"`
	Published  time.Time `json:"published_at"`
	Sentiment  string    `json:"sentiment,omitempty"`  // positive, negative, neutral
	Currencies []string  `json:"currencies,omitempty"` // Related coins
}

// CoinInfo contains detailed coin data from CoinGecko
type CoinInfo struct {
	ID                string    `json:"id"`
	Symbol            string    `json:"symbol"`
	Name              string    `json:"name"`
	CurrentPrice      float64   `json:"current_price"`
	MarketCap         float64   `json:"market_cap"`
	MarketCapRank     int       `json:"market_cap_rank"`
	TotalVolume       float64   `json:"total_volume"`
	High24h           float64   `json:"high_24h"`
	Low24h            float64   `json:"low_24h"`
	PriceChange24h    float64   `json:"price_change_24h"`
	PriceChangePct24h float64   `json:"price_change_percentage_24h"`
	PriceChangePct7d  float64   `json:"price_change_percentage_7d"`
	PriceChangePct30d float64   `json:"price_change_percentage_30d"`
	ATH               float64   `json:"ath"`
	ATHChangePct      float64   `json:"ath_change_percentage"`
	ATHDate           time.Time `json:"ath_date"`
	ATL               float64   `json:"atl"`
	ATLChangePct      float64   `json:"atl_change_percentage"`
	CirculatingSupply float64   `json:"circulating_supply"`
	TotalSupply       float64   `json:"total_supply"`
	LastUpdated       time.Time `json:"last_updated"`
}

// GlobalMarketData contains overall crypto market stats
type GlobalMarketData struct {
	TotalMarketCap         float64 `json:"total_market_cap"`
	TotalVolume            float64 `json:"total_volume"`
	BTCDominance           float64 `json:"btc_dominance"`
	ETHDominance           float64 `json:"eth_dominance"`
	MarketCapChangePct24h  float64 `json:"market_cap_change_percentage_24h"`
	ActiveCryptocurrencies int     `json:"active_cryptocurrencies"`
}

// CoinGecko ID mapping for common trading pairs
var CoinGeckoIDs = map[string]string{
	"BTCUSDT":   "bitcoin",
	"ETHUSDT":   "ethereum",
	"BNBUSDT":   "binancecoin",
	"XRPUSDT":   "ripple",
	"SOLUSDT":   "solana",
	"ADAUSDT":   "cardano",
	"DOGEUSDT":  "dogecoin",
	"DOTUSDT":   "polkadot",
	"MATICUSDT": "matic-network",
	"LTCUSDT":   "litecoin",
	"LINKUSDT":  "chainlink",
	"AVAXUSDT":  "avalanche-2",
	"ATOMUSDT":  "cosmos",
	"UNIUSDT":   "uniswap",
	"XLMUSDT":   "stellar",
	"ETCUSDT":   "ethereum-classic",
	"APTUSDT":   "aptos",
	"NEARUSDT":  "near",
	"FILUSDT":   "filecoin",
	"ARBUSDT":   "arbitrum",
	"OPUSDT":    "optimism",
	"XMRUSDT":   "monero",
	"DASHUSDT":  "dash",
	"ZENUSDT":   "horizen",
	"ICPUSDT":   "internet-computer",
}

// GetCoinGeckoID returns the CoinGecko ID for a trading symbol
func GetCoinGeckoID(symbol string) string {
	if id, ok := CoinGeckoIDs[symbol]; ok {
		return id
	}
	// Symbol not in known mapping, return empty (unknown coins won't have CoinGecko data)
	return ""
}
