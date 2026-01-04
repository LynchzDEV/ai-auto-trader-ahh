package store

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func Init(dataDir string) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "trading.db")
	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	if err := migrate(); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	log.Printf("Database initialized: %s", dbPath)
	return nil
}

func GetDB() *sql.DB {
	return db
}

func Close() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

func migrate() error {
	migrations := []string{
		// Strategies table
		`CREATE TABLE IF NOT EXISTS strategies (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			is_active BOOLEAN DEFAULT 0,
			config TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Traders table
		`CREATE TABLE IF NOT EXISTS traders (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			strategy_id TEXT,
			exchange TEXT NOT NULL DEFAULT 'binance',
			status TEXT DEFAULT 'stopped',
			initial_balance REAL DEFAULT 0,
			config TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (strategy_id) REFERENCES strategies(id)
		)`,

		// Decisions table (AI decision logs)
		`CREATE TABLE IF NOT EXISTS decisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			trader_id TEXT NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			market_data TEXT,
			ai_response TEXT,
			decisions TEXT,
			executed BOOLEAN DEFAULT 0,
			FOREIGN KEY (trader_id) REFERENCES traders(id)
		)`,

		// Create indexes
		`CREATE INDEX IF NOT EXISTS idx_decisions_trader ON decisions(trader_id)`,
	}

	for _, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	// Initialize new stores
	positionStore := NewPositionStore()
	if err := positionStore.InitTables(); err != nil {
		return fmt.Errorf("position store init failed: %w", err)
	}

	orderStore := NewOrderStore()
	if err := orderStore.InitTables(); err != nil {
		return fmt.Errorf("order store init failed: %w", err)
	}

	equityStore := NewEquityStore()
	if err := equityStore.InitTables(); err != nil {
		return fmt.Errorf("equity store init failed: %w", err)
	}

	tradeStore := NewTradeStore()
	if err := tradeStore.InitTables(); err != nil {
		return fmt.Errorf("trade store init failed: %w", err)
	}

	return nil
}
