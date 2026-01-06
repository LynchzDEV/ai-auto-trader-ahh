package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Strategy represents a trading strategy
type Strategy struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	IsActive    bool           `json:"is_active"`
	Config      StrategyConfig `json:"config"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// StrategyConfig holds all strategy configuration
type StrategyConfig struct {
	// Coin source configuration
	CoinSource CoinSourceConfig `json:"coin_source"`

	// Indicator configuration
	Indicators IndicatorConfig `json:"indicators"`

	// Risk control configuration
	RiskControl RiskControlConfig `json:"risk_control"`

	// AI configuration
	AI AIConfig `json:"ai"`

	// Custom AI prompt additions
	CustomPrompt string `json:"custom_prompt"`

	// Trading interval in minutes
	TradingInterval int `json:"trading_interval"`

	// Turbo Mode (Aggressive)
	TurboMode bool `json:"turbo_mode"`
}

// AIConfig defines AI model settings
type AIConfig struct {
	// Enable reasoning mode (uses models like deepseek-r1 that show chain-of-thought)
	EnableReasoning bool `json:"enable_reasoning"`

	// Reasoning model to use when reasoning is enabled (default: deepseek/deepseek-r1)
	ReasoningModel string `json:"reasoning_model"`
}

// CoinSourceConfig defines how to select coins
type CoinSourceConfig struct {
	SourceType  string   `json:"source_type"` // "static" | "dynamic"
	StaticCoins []string `json:"static_coins"`
}

// IndicatorConfig defines which indicators to use
type IndicatorConfig struct {
	// Kline settings
	PrimaryTimeframe string `json:"primary_timeframe"` // "1m", "5m", "15m", "1h", "4h"
	KlineCount       int    `json:"kline_count"`

	// Enabled indicators
	EnableEMA    bool `json:"enable_ema"`
	EnableMACD   bool `json:"enable_macd"`
	EnableRSI    bool `json:"enable_rsi"`
	EnableATR    bool `json:"enable_atr"`
	EnableBOLL   bool `json:"enable_boll"`
	EnableVolume bool `json:"enable_volume"`

	// Indicator periods
	EMAPeriods []int `json:"ema_periods"` // e.g., [9, 21]
	RSIPeriod  int   `json:"rsi_period"`  // e.g., 14
	ATRPeriod  int   `json:"atr_period"`  // e.g., 14
	BOLLPeriod int   `json:"boll_period"` // e.g., 20
	MACDFast   int   `json:"macd_fast"`   // e.g., 12
	MACDSlow   int   `json:"macd_slow"`   // e.g., 26
	MACDSignal int   `json:"macd_signal"` // e.g., 9
}

// RiskControlConfig defines risk management rules
type RiskControlConfig struct {
	// Position limits
	MaxPositions int `json:"max_positions"`

	// LEGACY fields (for backward compatibility with existing strategies)
	MaxLeverage        int     `json:"max_leverage"`         // Legacy: single leverage for all symbols
	MaxPositionPercent float64 `json:"max_position_percent"` // Legacy: % of balance per position

	// NEW: Leverage limits (separate for BTC/ETH vs altcoins)
	BTCETHMaxLeverage  int `json:"btc_eth_max_leverage"` // Max leverage for BTC/ETH (default: 10)
	AltcoinMaxLeverage int `json:"altcoin_max_leverage"` // Max leverage for altcoins (default: 20)

	// NEW: Position value ratios (position size = equity * ratio)
	BTCETHMaxPositionValueRatio  float64 `json:"btc_eth_max_position_value_ratio"` // Max position value ratio for BTC/ETH (default: 5.0)
	AltcoinMaxPositionValueRatio float64 `json:"altcoin_max_position_value_ratio"` // Max position value ratio for altcoins (default: 1.0)

	// Minimum position sizes
	MinPositionSize       float64 `json:"min_position_size"`         // Min position size for altcoins (default: 12 USDT)
	MinPositionSizeBTCETH float64 `json:"min_position_size_btc_eth"` // Min position size for BTC/ETH (default: 60 USDT)
	MinPositionUSD        float64 `json:"min_position_usd"`          // Legacy: single min for all (fallback)

	// Margin and buffer
	MaxMarginUsage float64 `json:"max_margin_usage"` // Max % of balance in margin (default: 90)
	MarginBuffer   float64 `json:"margin_buffer"`    // Safety buffer multiplier (default: 0.98 = use 98% of max)

	// AI decision thresholds
	MinConfidence      int     `json:"min_confidence"`        // Min AI confidence to trade (default: 70)
	MinRiskRewardRatio float64 `json:"min_risk_reward_ratio"` // Min TP/SL ratio (default: 3.0)

	// Daily loss and drawdown limits
	MaxDailyLossPct float64 `json:"max_daily_loss_pct"` // Max daily loss % before stopping (default: 5.0)
	MaxDrawdownPct  float64 `json:"max_drawdown_pct"`   // Max drawdown % from peak to close position (default: 40.0)
	StopTradingMins int     `json:"stop_trading_mins"`  // Minutes to pause after daily loss triggered (default: 60)

	// Drawdown monitoring thresholds
	DrawdownCloseThreshold float64 `json:"drawdown_close_threshold"` // Close position if drawdown from peak exceeds this % (default: 40.0)
	MinProfitForDrawdown   float64 `json:"min_profit_for_drawdown"`  // Only apply drawdown close when profit > this % (default: 5.0)

	// SAFETY: Emergency Shutdown
	EnableEmergencyShutdown bool    `json:"enable_emergency_shutdown"` // Stop trading if balance drops below limit
	EmergencyMinBalance     float64 `json:"emergency_min_balance"`     // Minimum balance to keep trading (default: 60 USD)
}

// DefaultStrategyConfig returns a sensible default strategy
func DefaultStrategyConfig() StrategyConfig {
	return StrategyConfig{
		CoinSource: CoinSourceConfig{
			SourceType:  "static",
			StaticCoins: []string{"BTCUSDT", "ETHUSDT"},
		},
		Indicators: IndicatorConfig{
			PrimaryTimeframe: "5m",
			KlineCount:       100,
			EnableEMA:        true,
			EnableMACD:       true,
			EnableRSI:        true,
			EnableATR:        true,
			EnableBOLL:       false,
			EnableVolume:     true,
			EMAPeriods:       []int{9, 21},
			RSIPeriod:        14,
			ATRPeriod:        14,
			BOLLPeriod:       20,
			MACDFast:         12,
			MACDSlow:         26,
			MACDSignal:       9,
		},
		RiskControl: RiskControlConfig{
			MaxPositions: 3,

			// Leverage limits (0 = use legacy MaxLeverage field)
			BTCETHMaxLeverage:  0,
			AltcoinMaxLeverage: 0,

			// Position value ratios
			BTCETHMaxPositionValueRatio:  5.0,
			AltcoinMaxPositionValueRatio: 1.0,

			// Minimum position sizes
			MinPositionSize:       12.0, // USDT for altcoins
			MinPositionSizeBTCETH: 60.0, // USDT for BTC/ETH

			// Margin settings
			MaxMarginUsage: 90.0,
			MarginBuffer:   0.98, // Use 98% of max affordable

			// AI thresholds
			MinConfidence:      70,
			MinRiskRewardRatio: 3.0, // Minimum 3:1 reward/risk

			// Daily loss and drawdown
			MaxDailyLossPct: 5.0,  // Stop trading after 5% daily loss
			MaxDrawdownPct:  40.0, // Max drawdown threshold
			StopTradingMins: 60,   // Pause 60 mins after trigger

			// Drawdown monitoring
			DrawdownCloseThreshold: 40.0, // Close at 40% drawdown from peak
			MinProfitForDrawdown:   5.0,  // Only apply when profit > 5%

			// Emergency Shutdown
			EnableEmergencyShutdown: true,
			EmergencyMinBalance:     60.0,
		},
		AI: AIConfig{
			EnableReasoning: false,
			ReasoningModel:  "deepseek/deepseek-r1",
		},
		CustomPrompt:    "",
		TradingInterval: 5,
	}
}

// StrategyStore handles strategy persistence
type StrategyStore struct{}

func NewStrategyStore() *StrategyStore {
	return &StrategyStore{}
}

func (s *StrategyStore) Create(strategy *Strategy) error {
	if strategy.ID == "" {
		strategy.ID = uuid.New().String()
	}
	strategy.CreatedAt = time.Now()
	strategy.UpdatedAt = time.Now()

	configJSON, err := json.Marshal(strategy.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	_, err = db.Exec(`
		INSERT INTO strategies (id, name, description, is_active, config, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, strategy.ID, strategy.Name, strategy.Description, strategy.IsActive, string(configJSON),
		strategy.CreatedAt, strategy.UpdatedAt)

	return err
}

func (s *StrategyStore) Update(strategy *Strategy) error {
	strategy.UpdatedAt = time.Now()

	configJSON, err := json.Marshal(strategy.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	_, err = db.Exec(`
		UPDATE strategies
		SET name = ?, description = ?, is_active = ?, config = ?, updated_at = ?
		WHERE id = ?
	`, strategy.Name, strategy.Description, strategy.IsActive, string(configJSON),
		strategy.UpdatedAt, strategy.ID)

	return err
}

func (s *StrategyStore) Delete(id string) error {
	_, err := db.Exec(`DELETE FROM strategies WHERE id = ?`, id)
	return err
}

func (s *StrategyStore) Get(id string) (*Strategy, error) {
	row := db.QueryRow(`
		SELECT id, name, description, is_active, config, created_at, updated_at
		FROM strategies WHERE id = ?
	`, id)

	return s.scanStrategy(row)
}

func (s *StrategyStore) GetActive() (*Strategy, error) {
	row := db.QueryRow(`
		SELECT id, name, description, is_active, config, created_at, updated_at
		FROM strategies WHERE is_active = 1 LIMIT 1
	`)

	strategy, err := s.scanStrategy(row)
	if err == sql.ErrNoRows {
		// Return default strategy if none active
		return &Strategy{
			ID:       "default",
			Name:     "Default Strategy",
			IsActive: true,
			Config:   DefaultStrategyConfig(),
		}, nil
	}
	return strategy, err
}

func (s *StrategyStore) List() ([]*Strategy, error) {
	rows, err := db.Query(`
		SELECT id, name, description, is_active, config, created_at, updated_at
		FROM strategies ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var strategies []*Strategy
	for rows.Next() {
		strategy, err := s.scanStrategyRow(rows)
		if err != nil {
			return nil, err
		}
		strategies = append(strategies, strategy)
	}

	return strategies, rows.Err()
}

func (s *StrategyStore) SetActive(id string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Deactivate all strategies
	if _, err := tx.Exec(`UPDATE strategies SET is_active = 0`); err != nil {
		return err
	}

	// Activate the selected one
	if _, err := tx.Exec(`UPDATE strategies SET is_active = 1 WHERE id = ?`, id); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *StrategyStore) scanStrategy(row *sql.Row) (*Strategy, error) {
	var strategy Strategy
	var configJSON string

	err := row.Scan(
		&strategy.ID, &strategy.Name, &strategy.Description,
		&strategy.IsActive, &configJSON,
		&strategy.CreatedAt, &strategy.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(configJSON), &strategy.Config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &strategy, nil
}

func (s *StrategyStore) scanStrategyRow(rows *sql.Rows) (*Strategy, error) {
	var strategy Strategy
	var configJSON string

	err := rows.Scan(
		&strategy.ID, &strategy.Name, &strategy.Description,
		&strategy.IsActive, &configJSON,
		&strategy.CreatedAt, &strategy.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(configJSON), &strategy.Config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &strategy, nil
}
