package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Trader represents a trading bot instance
type Trader struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	StrategyID     string       `json:"strategy_id"`
	Exchange       string       `json:"exchange"`
	Status         string       `json:"status"` // "running", "stopped", "error"
	InitialBalance float64      `json:"initial_balance"`
	Config         TraderConfig `json:"config"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

// TraderConfig holds trader-specific configuration
type TraderConfig struct {
	// AI Model settings (pre-configured, not selectable yet)
	AIProvider string `json:"ai_provider"` // "openrouter"
	AIModel    string `json:"ai_model"`    // model name

	// Exchange credentials
	APIKey    string `json:"api_key"`
	SecretKey string `json:"secret_key"`
	Testnet   bool   `json:"testnet"`
}

// TraderStore handles trader persistence
type TraderStore struct{}

func NewTraderStore() *TraderStore {
	return &TraderStore{}
}

func (s *TraderStore) Create(trader *Trader) error {
	if trader.ID == "" {
		trader.ID = uuid.New().String()
	}
	if trader.Status == "" {
		trader.Status = "stopped"
	}
	trader.CreatedAt = time.Now()
	trader.UpdatedAt = time.Now()

	configJSON, err := json.Marshal(trader.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	_, err = db.Exec(`
		INSERT INTO traders (id, name, strategy_id, exchange, status, initial_balance, config, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, trader.ID, trader.Name, trader.StrategyID, trader.Exchange, trader.Status,
		trader.InitialBalance, string(configJSON), trader.CreatedAt, trader.UpdatedAt)

	return err
}

func (s *TraderStore) Update(trader *Trader) error {
	trader.UpdatedAt = time.Now()

	configJSON, err := json.Marshal(trader.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	_, err = db.Exec(`
		UPDATE traders
		SET name = ?, strategy_id = ?, exchange = ?, status = ?, initial_balance = ?, config = ?, updated_at = ?
		WHERE id = ?
	`, trader.Name, trader.StrategyID, trader.Exchange, trader.Status,
		trader.InitialBalance, string(configJSON), trader.UpdatedAt, trader.ID)

	return err
}

func (s *TraderStore) UpdateStatus(id, status string) error {
	_, err := db.Exec(`UPDATE traders SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now(), id)
	return err
}

func (s *TraderStore) Delete(id string) error {
	_, err := db.Exec(`DELETE FROM traders WHERE id = ?`, id)
	return err
}

func (s *TraderStore) Get(id string) (*Trader, error) {
	row := db.QueryRow(`
		SELECT id, name, strategy_id, exchange, status, initial_balance, config, created_at, updated_at
		FROM traders WHERE id = ?
	`, id)

	return s.scanTrader(row)
}

func (s *TraderStore) List() ([]*Trader, error) {
	rows, err := db.Query(`
		SELECT id, name, strategy_id, exchange, status, initial_balance, config, created_at, updated_at
		FROM traders ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var traders []*Trader
	for rows.Next() {
		trader, err := s.scanTraderRow(rows)
		if err != nil {
			return nil, err
		}
		traders = append(traders, trader)
	}

	return traders, rows.Err()
}

func (s *TraderStore) scanTrader(row *sql.Row) (*Trader, error) {
	var trader Trader
	var configJSON string
	var strategyID sql.NullString

	err := row.Scan(
		&trader.ID, &trader.Name, &strategyID, &trader.Exchange,
		&trader.Status, &trader.InitialBalance, &configJSON,
		&trader.CreatedAt, &trader.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if strategyID.Valid {
		trader.StrategyID = strategyID.String
	}

	if err := json.Unmarshal([]byte(configJSON), &trader.Config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &trader, nil
}

func (s *TraderStore) scanTraderRow(rows *sql.Rows) (*Trader, error) {
	var trader Trader
	var configJSON string
	var strategyID sql.NullString

	err := rows.Scan(
		&trader.ID, &trader.Name, &strategyID, &trader.Exchange,
		&trader.Status, &trader.InitialBalance, &configJSON,
		&trader.CreatedAt, &trader.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if strategyID.Valid {
		trader.StrategyID = strategyID.String
	}

	if err := json.Unmarshal([]byte(configJSON), &trader.Config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &trader, nil
}

// Decision represents an AI trading decision record
type Decision struct {
	ID         int64     `json:"id"`
	TraderID   string    `json:"trader_id"`
	Timestamp  time.Time `json:"timestamp"`
	MarketData string    `json:"market_data"`
	AIResponse string    `json:"ai_response"`
	Decisions  string    `json:"decisions"` // JSON array of decisions
	Executed   bool      `json:"executed"`
}

// DecisionStore handles decision persistence
type DecisionStore struct{}

func NewDecisionStore() *DecisionStore {
	return &DecisionStore{}
}

func (s *DecisionStore) Create(decision *Decision) error {
	decision.Timestamp = time.Now()

	result, err := db.Exec(`
		INSERT INTO decisions (trader_id, timestamp, market_data, ai_response, decisions, executed)
		VALUES (?, ?, ?, ?, ?, ?)
	`, decision.TraderID, decision.Timestamp, decision.MarketData,
		decision.AIResponse, decision.Decisions, decision.Executed)
	if err != nil {
		return err
	}

	id, _ := result.LastInsertId()
	decision.ID = id
	return nil
}

func (s *DecisionStore) ListByTrader(traderID string, limit int) ([]*Decision, error) {
	rows, err := db.Query(`
		SELECT id, trader_id, timestamp, market_data, ai_response, decisions, executed
		FROM decisions WHERE trader_id = ?
		ORDER BY timestamp DESC LIMIT ?
	`, traderID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []*Decision
	for rows.Next() {
		var d Decision
		if err := rows.Scan(&d.ID, &d.TraderID, &d.Timestamp, &d.MarketData,
			&d.AIResponse, &d.Decisions, &d.Executed); err != nil {
			return nil, err
		}
		decisions = append(decisions, &d)
	}

	return decisions, rows.Err()
}

func (s *DecisionStore) GetLatest(traderID string) (*Decision, error) {
	row := db.QueryRow(`
		SELECT id, trader_id, timestamp, market_data, ai_response, decisions, executed
		FROM decisions WHERE trader_id = ?
		ORDER BY timestamp DESC LIMIT 1
	`, traderID)

	var d Decision
	err := row.Scan(&d.ID, &d.TraderID, &d.Timestamp, &d.MarketData,
		&d.AIResponse, &d.Decisions, &d.Executed)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// EquitySnapshot represents a point in time equity value
type EquitySnapshot struct {
	ID            int64     `json:"id"`
	TraderID      string    `json:"trader_id"`
	Timestamp     time.Time `json:"timestamp"`
	Equity        float64   `json:"equity"`
	Balance       float64   `json:"balance"`
	UnrealizedPnL float64   `json:"unrealized_pnl"`
}

// EquityStore handles equity snapshot persistence
type EquityStore struct{}

func NewEquityStore() *EquityStore {
	return &EquityStore{}
}

func (s *EquityStore) Create(snapshot *EquitySnapshot) error {
	snapshot.Timestamp = time.Now()

	result, err := db.Exec(`
		INSERT INTO equity_snapshots (trader_id, timestamp, equity, balance, unrealized_pnl)
		VALUES (?, ?, ?, ?, ?)
	`, snapshot.TraderID, snapshot.Timestamp, snapshot.Equity,
		snapshot.Balance, snapshot.UnrealizedPnL)
	if err != nil {
		return err
	}

	id, _ := result.LastInsertId()
	snapshot.ID = id
	return nil
}

func (s *EquityStore) ListByTrader(traderID string, limit int) ([]*EquitySnapshot, error) {
	rows, err := db.Query(`
		SELECT id, trader_id, timestamp, equity, balance, unrealized_pnl
		FROM equity_snapshots WHERE trader_id = ?
		ORDER BY timestamp DESC LIMIT ?
	`, traderID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []*EquitySnapshot
	for rows.Next() {
		var s EquitySnapshot
		if err := rows.Scan(&s.ID, &s.TraderID, &s.Timestamp, &s.Equity,
			&s.Balance, &s.UnrealizedPnL); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, &s)
	}

	return snapshots, rows.Err()
}
