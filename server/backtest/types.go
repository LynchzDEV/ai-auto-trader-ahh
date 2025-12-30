package backtest

import (
	"time"

	"auto-trader/decision"
)

// RunStatus represents the status of a backtest run
type RunStatus string

const (
	StatusPending    RunStatus = "pending"
	StatusRunning    RunStatus = "running"
	StatusPaused     RunStatus = "paused"
	StatusCompleted  RunStatus = "completed"
	StatusFailed     RunStatus = "failed"
	StatusLiquidated RunStatus = "liquidated"
)

// FillPolicy determines how orders are filled in simulation
type FillPolicy string

const (
	FillPolicyNextOpen FillPolicy = "next_open"  // Fill at next bar's open
	FillPolicyBarVWAP  FillPolicy = "bar_vwap"   // Fill at bar's VWAP
	FillPolicyMidPrice FillPolicy = "mid_price"  // Fill at (high+low)/2
	FillPolicyClose    FillPolicy = "close"      // Fill at bar's close
)

// Config holds backtest configuration
type Config struct {
	RunID                string     `json:"run_id"`
	UserID               string     `json:"user_id"`
	Name                 string     `json:"name"`
	Description          string     `json:"description"`
	Symbols              []string   `json:"symbols"`
	Timeframes           []string   `json:"timeframes"`
	DecisionTimeframe    string     `json:"decision_timeframe"`
	DecisionCadenceNBars int        `json:"decision_cadence_n_bars"`
	StartTS              int64      `json:"start_ts"`
	EndTS                int64      `json:"end_ts"`
	InitialBalance       float64    `json:"initial_balance"`
	FeeBps               float64    `json:"fee_bps"`       // Fee in basis points
	SlippageBps          float64    `json:"slippage_bps"`  // Slippage in basis points
	FillPolicy           FillPolicy `json:"fill_policy"`
	BTCETHLeverage       int        `json:"btc_eth_leverage"`
	AltcoinLeverage      int        `json:"altcoin_leverage"`
	BTCETHPosRatio       float64    `json:"btc_eth_pos_ratio"`
	AltcoinPosRatio      float64    `json:"altcoin_pos_ratio"`
	CacheAI              bool       `json:"cache_ai"`
	ReplayOnly           bool       `json:"replay_only"`
	Language             string     `json:"language"`
}

// DefaultConfig returns a default backtest configuration
func DefaultConfig() *Config {
	return &Config{
		Symbols:              []string{"BTCUSDT", "ETHUSDT"},
		Timeframes:           []string{"1h"},
		DecisionTimeframe:    "1h",
		DecisionCadenceNBars: 4, // Decision every 4 bars
		InitialBalance:       10000,
		FeeBps:               4,    // 0.04%
		SlippageBps:          5,    // 0.05%
		FillPolicy:           FillPolicyNextOpen,
		BTCETHLeverage:       20,
		AltcoinLeverage:      10,
		BTCETHPosRatio:       0.3,
		AltcoinPosRatio:      0.15,
		Language:             "en-US",
	}
}

// Validate validates and normalizes the config
func (c *Config) Validate() error {
	if c.InitialBalance <= 0 {
		c.InitialBalance = 10000
	}
	if c.DecisionCadenceNBars <= 0 {
		c.DecisionCadenceNBars = 4
	}
	if c.DecisionTimeframe == "" && len(c.Timeframes) > 0 {
		c.DecisionTimeframe = c.Timeframes[0]
	}
	if c.BTCETHLeverage <= 0 {
		c.BTCETHLeverage = 20
	}
	if c.AltcoinLeverage <= 0 {
		c.AltcoinLeverage = 10
	}
	if c.BTCETHPosRatio <= 0 {
		c.BTCETHPosRatio = 0.3
	}
	if c.AltcoinPosRatio <= 0 {
		c.AltcoinPosRatio = 0.15
	}
	if c.FillPolicy == "" {
		c.FillPolicy = FillPolicyNextOpen
	}
	if c.Language == "" {
		c.Language = "en-US"
	}
	return nil
}

// Position represents a simulated position
type Position struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"` // "long" or "short"
	Quantity         float64 `json:"quantity"`
	EntryPrice       float64 `json:"entry_price"`
	Leverage         int     `json:"leverage"`
	Margin           float64 `json:"margin"`
	Notional         float64 `json:"notional"`
	LiquidationPrice float64 `json:"liquidation_price"`
	OpenTime         int64   `json:"open_time"`
	AccumulatedFee   float64 `json:"accumulated_fee"`
}

// State represents the current backtest state
type State struct {
	BarIndex        int                  `json:"bar_index"`
	BarTimestamp    int64                `json:"bar_timestamp"`
	DecisionCycle   int                  `json:"decision_cycle"`
	Cash            float64              `json:"cash"`
	Equity          float64              `json:"equity"`
	UnrealizedPnL   float64              `json:"unrealized_pnl"`
	RealizedPnL     float64              `json:"realized_pnl"`
	MaxEquity       float64              `json:"max_equity"`
	MinEquity       float64              `json:"min_equity"`
	MaxDrawdownPct  float64              `json:"max_drawdown_pct"`
	Positions       map[string]*Position `json:"positions"`
	LastUpdate      time.Time            `json:"last_update"`
	Liquidated      bool                 `json:"liquidated"`
	LiquidationNote string               `json:"liquidation_note"`
}

// NewState creates a new backtest state
func NewState(initialBalance float64) *State {
	return &State{
		Cash:       initialBalance,
		Equity:     initialBalance,
		MaxEquity:  initialBalance,
		MinEquity:  initialBalance,
		Positions:  make(map[string]*Position),
		LastUpdate: time.Now(),
	}
}

// EquityPoint represents a point on the equity curve
type EquityPoint struct {
	Timestamp   int64   `json:"timestamp"`
	Equity      float64 `json:"equity"`
	Available   float64 `json:"available"`
	PnL         float64 `json:"pnl"`
	PnLPct      float64 `json:"pnl_pct"`
	DrawdownPct float64 `json:"drawdown_pct"`
	Cycle       int     `json:"cycle"`
}

// TradeEvent represents a trade execution
type TradeEvent struct {
	Timestamp       int64   `json:"timestamp"`
	Symbol          string  `json:"symbol"`
	Action          string  `json:"action"`
	Side            string  `json:"side"`
	Quantity        float64 `json:"quantity"`
	Price           float64 `json:"price"`
	Fee             float64 `json:"fee"`
	Slippage        float64 `json:"slippage"`
	OrderValue      float64 `json:"order_value"`
	RealizedPnL     float64 `json:"realized_pnl"`
	Leverage        int     `json:"leverage"`
	Cycle           int     `json:"cycle"`
	PositionAfter   float64 `json:"position_after"`
	LiquidationFlag bool    `json:"liquidation_flag"`
	Note            string  `json:"note"`
}

// DecisionLog represents a logged AI decision
type DecisionLog struct {
	Timestamp       int64                `json:"timestamp"`
	Cycle           int                  `json:"cycle"`
	BarIndex        int                  `json:"bar_index"`
	SystemPrompt    string               `json:"system_prompt"`
	UserPrompt      string               `json:"user_prompt"`
	RawResponse     string               `json:"raw_response"`
	CoTTrace        string               `json:"cot_trace"`
	Decisions       []decision.Decision  `json:"decisions"`
	DurationMs      int64                `json:"duration_ms"`
	Error           string               `json:"error,omitempty"`
}

// Metrics represents backtest performance metrics
type Metrics struct {
	TotalReturn     float64            `json:"total_return"`
	TotalReturnPct  float64            `json:"total_return_pct"`
	MaxDrawdown     float64            `json:"max_drawdown"`
	MaxDrawdownPct  float64            `json:"max_drawdown_pct"`
	SharpeRatio     float64            `json:"sharpe_ratio"`
	SortinoRatio    float64            `json:"sortino_ratio"`
	WinRate         float64            `json:"win_rate"`
	ProfitFactor    float64            `json:"profit_factor"`
	TotalTrades     int                `json:"total_trades"`
	WinningTrades   int                `json:"winning_trades"`
	LosingTrades    int                `json:"losing_trades"`
	AvgWin          float64            `json:"avg_win"`
	AvgLoss         float64            `json:"avg_loss"`
	LargestWin      float64            `json:"largest_win"`
	LargestLoss     float64            `json:"largest_loss"`
	AvgHoldTime     float64            `json:"avg_hold_time_hours"`
	TotalFees       float64            `json:"total_fees"`
	FinalEquity     float64            `json:"final_equity"`
	SymbolStats     map[string]*SymbolStats `json:"symbol_stats"`
}

// SymbolStats represents per-symbol statistics
type SymbolStats struct {
	Symbol        string  `json:"symbol"`
	TotalTrades   int     `json:"total_trades"`
	WinRate       float64 `json:"win_rate"`
	TotalPnL      float64 `json:"total_pnl"`
	AvgPnL        float64 `json:"avg_pnl"`
	LongTrades    int     `json:"long_trades"`
	ShortTrades   int     `json:"short_trades"`
	LongWinRate   float64 `json:"long_win_rate"`
	ShortWinRate  float64 `json:"short_win_rate"`
}

// RunMetadata represents metadata for a backtest run
type RunMetadata struct {
	RunID          string    `json:"run_id"`
	UserID         string    `json:"user_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Status         RunStatus `json:"status"`
	Config         *Config   `json:"config"`
	StartedAt      time.Time `json:"started_at"`
	CompletedAt    time.Time `json:"completed_at,omitempty"`
	Progress       float64   `json:"progress"`
	CurrentBar     int       `json:"current_bar"`
	TotalBars      int       `json:"total_bars"`
	CurrentEquity  float64   `json:"current_equity"`
	Error          string    `json:"error,omitempty"`
}

// Kline represents a candlestick
type Kline struct {
	OpenTime  int64   `json:"open_time"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    float64 `json:"volume"`
	CloseTime int64   `json:"close_time"`
}
