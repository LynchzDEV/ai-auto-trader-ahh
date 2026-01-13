package decision

import "time"

// Language type for bilingual support
type Language string

const (
	LangChinese Language = "zh-CN"
	LangEnglish Language = "en-US"
)

// Valid action constants
const (
	ActionOpenLong   = "open_long"
	ActionOpenShort  = "open_short"
	ActionCloseLong  = "close_long"
	ActionCloseShort = "close_short"
	ActionHold       = "hold"
	ActionWait       = "wait"
)

// Decision represents a single trading decision from AI
type Decision struct {
	Symbol string `json:"symbol"`
	Action string `json:"action"` // "open_long", "open_short", "close_long", "close_short", "hold", "wait"

	// Opening position parameters
	Leverage        int     `json:"leverage,omitempty"`
	PositionSizeUSD float64 `json:"position_size_usd,omitempty"`
	StopLoss        float64 `json:"stop_loss,omitempty"`
	TakeProfit      float64 `json:"take_profit,omitempty"`

	// Common parameters
	Confidence int     `json:"confidence,omitempty"` // Confidence level (0-100)
	RiskUSD    float64 `json:"risk_usd,omitempty"`   // Maximum USD risk
	Reasoning  string  `json:"reasoning"`
}

// FullDecision is the complete AI response with chain of thought
type FullDecision struct {
	SystemPrompt        string     `json:"system_prompt"`
	UserPrompt          string     `json:"user_prompt"`
	CoTTrace            string     `json:"cot_trace"` // Chain of thought
	Decisions           []Decision `json:"decisions"`
	RawResponse         string     `json:"raw_response"`
	Timestamp           time.Time  `json:"timestamp"`
	AIRequestDurationMs int64      `json:"ai_request_duration_ms,omitempty"`
}

// PositionInfo represents current trading position
type PositionInfo struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"` // "long" or "short"
	EntryPrice       float64 `json:"entry_price"`
	MarkPrice        float64 `json:"mark_price"`
	Quantity         float64 `json:"quantity"`
	Leverage         int     `json:"leverage"`
	UnrealizedPnL    float64 `json:"unrealized_pnl"`
	UnrealizedPnLPct float64 `json:"unrealized_pnl_pct"`
	PeakPnLPct       float64 `json:"peak_pnl_pct"` // Historical peak profit percentage
	LiquidationPrice float64 `json:"liquidation_price"`
	MarginUsed       float64 `json:"margin_used"`
	UpdateTime       int64   `json:"update_time"` // Position update timestamp (milliseconds)
}

// AccountInfo represents account metrics
type AccountInfo struct {
	TotalEquity      float64 `json:"total_equity"`      // Account equity
	AvailableBalance float64 `json:"available_balance"` // Available balance
	UnrealizedPnL    float64 `json:"unrealized_pnl"`    // Unrealized profit/loss
	TotalPnL         float64 `json:"total_pnl"`         // Total profit/loss
	TotalPnLPct      float64 `json:"total_pnl_pct"`     // Total profit/loss percentage
	MarginUsed       float64 `json:"margin_used"`       // Used margin
	MarginUsedPct    float64 `json:"margin_used_pct"`   // Margin usage rate
	PositionCount    int     `json:"position_count"`    // Number of positions
}

// CandidateCoin represents a coin candidate for trading
type CandidateCoin struct {
	Symbol  string   `json:"symbol"`
	Sources []string `json:"sources"` // Sources: "ai500" and/or "oi_top"
}

// TradingStats represents historical trading statistics
type TradingStats struct {
	TotalTrades    int     `json:"total_trades"`     // Total number of trades (closed)
	WinRate        float64 `json:"win_rate"`         // Win rate (%)
	ProfitFactor   float64 `json:"profit_factor"`    // Profit factor
	SharpeRatio    float64 `json:"sharpe_ratio"`     // Sharpe ratio
	TotalPnL       float64 `json:"total_pnl"`        // Total profit/loss
	AvgWin         float64 `json:"avg_win"`          // Average win
	AvgLoss        float64 `json:"avg_loss"`         // Average loss
	MaxDrawdownPct float64 `json:"max_drawdown_pct"` // Maximum drawdown (%)
}

// RecentOrder represents a recently completed trade
type RecentOrder struct {
	Symbol       string  `json:"symbol"`        // Trading pair
	Side         string  `json:"side"`          // long/short
	EntryPrice   float64 `json:"entry_price"`   // Entry price
	ExitPrice    float64 `json:"exit_price"`    // Exit price
	RealizedPnL  float64 `json:"realized_pnl"`  // Realized profit/loss
	PnLPct       float64 `json:"pnl_pct"`       // Profit/loss percentage
	EntryTime    string  `json:"entry_time"`    // Entry time
	ExitTime     string  `json:"exit_time"`     // Exit time
	HoldDuration string  `json:"hold_duration"` // Hold duration, e.g. "2h30m"
}

// MarketData represents market data for a symbol
type MarketData struct {
	Symbol        string    `json:"symbol"`
	Price         float64   `json:"price"`
	Change24h     float64   `json:"change_24h"`
	Volume24h     float64   `json:"volume_24h"`
	OpenInterest  float64   `json:"open_interest"`
	OIChange24h   float64   `json:"oi_change_24h"`
	FundingRate   float64   `json:"funding_rate"`
	HighPrice24h  float64   `json:"high_24h"`
	LowPrice24h   float64   `json:"low_24h"`
	Timestamp     time.Time `json:"timestamp"`
	Klines        []Kline   `json:"klines,omitempty"`
}

// Kline represents candlestick data
type Kline struct {
	OpenTime  int64   `json:"open_time"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    float64 `json:"volume"`
	CloseTime int64   `json:"close_time"`
}

// Context is the complete trading context passed to AI
type Context struct {
	CurrentTime     string                   `json:"current_time"`
	RuntimeMinutes  int                      `json:"runtime_minutes"`
	CallCount       int                      `json:"call_count"`
	Account         AccountInfo              `json:"account"`
	Positions       []PositionInfo           `json:"positions"`
	CandidateCoins  []CandidateCoin          `json:"candidate_coins"`
	PromptVariant   string                   `json:"prompt_variant,omitempty"`
	TradingStats    *TradingStats            `json:"trading_stats,omitempty"`
	RecentOrders    []RecentOrder            `json:"recent_orders,omitempty"`
	MarketDataMap   map[string]*MarketData   `json:"-"`
	MultiTFMarket   map[string]map[string]*MarketData `json:"-"` // symbol -> timeframe -> data
	BTCETHLeverage  int                      `json:"-"`
	AltcoinLeverage int                      `json:"-"`
	BTCETHPosRatio  float64                  `json:"-"` // Max position ratio for BTC/ETH
	AltcoinPosRatio float64                  `json:"-"` // Max position ratio for altcoins
	Timeframes      []string                 `json:"-"`

	// Noise Zone Config - passed to AI prompts
	NoiseZoneLowerBound float64 `json:"-"` // e.g., -1.0 means below -1% is significant loss
	NoiseZoneUpperBound float64 `json:"-"` // e.g., 1.5 means above +1.5% is profit zone

	// Market Intelligence - external data (news, sentiment, fundamentals)
	MarketIntelFormatted string `json:"-"` // Pre-formatted intel string for AI prompt
}

// ValidationConfig holds validation parameters
type ValidationConfig struct {
	AccountEquity     float64
	BTCETHLeverage    int
	AltcoinLeverage   int
	BTCETHPosRatio    float64
	AltcoinPosRatio   float64
	MinPositionBTCETH float64 // Minimum position size for BTC/ETH
	MinPositionAlt    float64 // Minimum position size for altcoins
	MinRiskReward     float64 // Minimum risk/reward ratio
}

// DefaultValidationConfig returns default validation parameters
func DefaultValidationConfig() *ValidationConfig {
	return &ValidationConfig{
		AccountEquity:     10000,
		BTCETHLeverage:    20,
		AltcoinLeverage:   10,
		BTCETHPosRatio:    0.3, // 30% max position
		AltcoinPosRatio:   0.15, // 15% max position
		MinPositionBTCETH: 60,
		MinPositionAlt:    12,
		MinRiskReward:     3.0,
	}
}
