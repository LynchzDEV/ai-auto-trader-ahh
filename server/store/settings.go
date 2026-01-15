package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

// SettingsStore handles global app settings persistence
type SettingsStore struct{}

func NewSettingsStore() *SettingsStore {
	return &SettingsStore{}
}

// InitTables creates the settings table if it doesn't exist
func (s *SettingsStore) InitTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := db.Exec(query)
	return err
}

// Get retrieves a setting value by key
func (s *SettingsStore) Get(key string) (string, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// Set saves a setting value
func (s *SettingsStore) Set(key, value string) error {
	_, err := db.Exec(`
		INSERT OR REPLACE INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
	`, key, value, time.Now())
	return err
}

// Delete removes a setting
func (s *SettingsStore) Delete(key string) error {
	_, err := db.Exec(`DELETE FROM settings WHERE key = ?`, key)
	return err
}

// GetAll retrieves all settings as a map
func (s *SettingsStore) GetAll() (map[string]string, error) {
	rows, err := db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		settings[key] = value
	}

	return settings, rows.Err()
}

// SetMultiple saves multiple settings at once
func (s *SettingsStore) SetMultiple(settings map[string]string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now()
	for key, value := range settings {
		if _, err := stmt.Exec(key, value, now); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GlobalSettings represents the app-wide configuration
type GlobalSettings struct {
	// OpenRouter AI Configuration
	OpenRouterAPIKey string `json:"openrouter_api_key"`
	OpenRouterModel  string `json:"openrouter_model"`

	// Default Binance Configuration (used when trader doesn't specify)
	BinanceAPIKey    string `json:"binance_api_key"`
	BinanceSecretKey string `json:"binance_secret_key"`
	BinanceTestnet   bool   `json:"binance_testnet"`
}

// GetGlobalSettings retrieves all global settings as a struct
func (s *SettingsStore) GetGlobalSettings() (*GlobalSettings, error) {
	all, err := s.GetAll()
	if err != nil {
		return nil, err
	}

	settings := &GlobalSettings{
		OpenRouterAPIKey: all["openrouter_api_key"],
		OpenRouterModel:  all["openrouter_model"],
		BinanceAPIKey:    all["binance_api_key"],
		BinanceSecretKey: all["binance_secret_key"],
		BinanceTestnet:   all["binance_testnet"] == "true",
	}

	return settings, nil
}

// SaveGlobalSettings saves global settings
func (s *SettingsStore) SaveGlobalSettings(settings *GlobalSettings) error {
	data := map[string]string{
		"openrouter_api_key": settings.OpenRouterAPIKey,
		"openrouter_model":   settings.OpenRouterModel,
		"binance_api_key":    settings.BinanceAPIKey,
		"binance_secret_key": settings.BinanceSecretKey,
		"binance_testnet":    boolToString(settings.BinanceTestnet),
	}
	return s.SetMultiple(data)
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// MarshalJSON masks sensitive fields when serializing
func (gs GlobalSettings) MarshalJSON() ([]byte, error) {
	type Alias GlobalSettings
	return json.Marshal(&struct {
		Alias
		OpenRouterAPIKey string `json:"openrouter_api_key"`
		BinanceAPIKey    string `json:"binance_api_key"`
		BinanceSecretKey string `json:"binance_secret_key"`
	}{
		Alias:            Alias(gs),
		OpenRouterAPIKey: maskSecret(gs.OpenRouterAPIKey),
		BinanceAPIKey:    maskSecret(gs.BinanceAPIKey),
		BinanceSecretKey: maskSecret(gs.BinanceSecretKey),
	})
}

func maskSecret(s string) string {
	if len(s) <= 8 {
		if len(s) == 0 {
			return ""
		}
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}

// =============================================================================
// Daily Loss State Persistence (per-trader)
// =============================================================================

// DailyLossState holds the daily loss tracking state for a trader
type DailyLossState struct {
	TraderID       string    `json:"trader_id"`
	StopUntil      time.Time `json:"stop_until"`      // Don't trade until this time
	InitialBalance float64   `json:"initial_balance"` // Balance at start of tracking period
	LastResetTime  time.Time `json:"last_reset_time"` // When daily P&L was last reset
}

// GetDailyLossState retrieves the daily loss state for a specific trader
func (s *SettingsStore) GetDailyLossState(traderID string) (*DailyLossState, error) {
	key := "daily_loss_state:" + traderID
	value, err := s.Get(key)
	if err != nil {
		return nil, err
	}
	if value == "" {
		return nil, nil // No state saved
	}

	var state DailyLossState
	if err := json.Unmarshal([]byte(value), &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// SaveDailyLossState saves the daily loss state for a specific trader
func (s *SettingsStore) SaveDailyLossState(state *DailyLossState) error {
	if state == nil || state.TraderID == "" {
		return nil
	}

	key := "daily_loss_state:" + state.TraderID
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.Set(key, string(data))
}

// ClearDailyLossState removes the daily loss state for a specific trader
func (s *SettingsStore) ClearDailyLossState(traderID string) error {
	key := "daily_loss_state:" + traderID
	return s.Delete(key)
}
