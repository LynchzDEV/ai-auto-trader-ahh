package store

import (
	"time"
)

// Trade represents an executed trade from Binance
type Trade struct {
	ID          int64     `json:"id"`           // Binance trade ID
	TraderID    string    `json:"trader_id"`    // Our trader ID
	Symbol      string    `json:"symbol"`       // Trading pair
	Side        string    `json:"side"`         // BUY or SELL
	Price       float64   `json:"price"`        // Execution price
	Quantity    float64   `json:"quantity"`     // Executed quantity
	QuoteQty    float64   `json:"quote_qty"`    // Value in USDT
	RealizedPnL float64   `json:"realized_pnl"` // Realized PnL from this trade
	Commission  float64   `json:"commission"`   // Trading fee
	Timestamp   time.Time `json:"timestamp"`    // Trade time
	OrderID     int64     `json:"order_id"`     // Binance order ID
}

// TradeStore handles trade persistence
type TradeStore struct{}

func NewTradeStore() *TradeStore {
	return &TradeStore{}
}

// InitTables creates the trades table if it doesn't exist
func (s *TradeStore) InitTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS trades (
		id INTEGER PRIMARY KEY,
		trader_id TEXT NOT NULL,
		symbol TEXT NOT NULL,
		side TEXT NOT NULL,
		price REAL NOT NULL,
		quantity REAL NOT NULL,
		quote_qty REAL NOT NULL,
		realized_pnl REAL DEFAULT 0,
		commission REAL DEFAULT 0,
		timestamp DATETIME NOT NULL,
		order_id INTEGER,
		UNIQUE(id, trader_id)
	);
	CREATE INDEX IF NOT EXISTS idx_trades_trader ON trades(trader_id);
	CREATE INDEX IF NOT EXISTS idx_trades_timestamp ON trades(trader_id, timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_trades_symbol ON trades(trader_id, symbol);
	`
	_, err := db.Exec(query)
	return err
}

// Save saves a trade to the database
func (s *TradeStore) Save(trade *Trade) error {
	_, err := db.Exec(`
		INSERT OR REPLACE INTO trades (id, trader_id, symbol, side, price, quantity, quote_qty, realized_pnl, commission, timestamp, order_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, trade.ID, trade.TraderID, trade.Symbol, trade.Side, trade.Price, trade.Quantity,
		trade.QuoteQty, trade.RealizedPnL, trade.Commission, trade.Timestamp, trade.OrderID)
	return err
}

// SaveBatch saves multiple trades efficiently
func (s *TradeStore) SaveBatch(trades []*Trade) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO trades (id, trader_id, symbol, side, price, quantity, quote_qty, realized_pnl, commission, timestamp, order_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, trade := range trades {
		_, err := stmt.Exec(trade.ID, trade.TraderID, trade.Symbol, trade.Side, trade.Price,
			trade.Quantity, trade.QuoteQty, trade.RealizedPnL, trade.Commission, trade.Timestamp, trade.OrderID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetByTrader retrieves trades for a trader, ordered by timestamp desc
func (s *TradeStore) GetByTrader(traderID string, limit int) ([]*Trade, error) {
	rows, err := db.Query(`
		SELECT id, trader_id, symbol, side, price, quantity, quote_qty, realized_pnl, commission, timestamp, order_id
		FROM trades WHERE trader_id = ?
		ORDER BY timestamp DESC LIMIT ?
	`, traderID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trades []*Trade
	for rows.Next() {
		var t Trade
		if err := rows.Scan(&t.ID, &t.TraderID, &t.Symbol, &t.Side, &t.Price, &t.Quantity,
			&t.QuoteQty, &t.RealizedPnL, &t.Commission, &t.Timestamp, &t.OrderID); err != nil {
			return nil, err
		}
		trades = append(trades, &t)
	}

	return trades, rows.Err()
}

// GetLastTradeTime returns the timestamp of the most recent trade for a trader
func (s *TradeStore) GetLastTradeTime(traderID string) (int64, error) {
	var timestamp int64
	err := db.QueryRow(`
		SELECT COALESCE(MAX(timestamp), 0) FROM trades WHERE trader_id = ?
	`, traderID).Scan(&timestamp)
	if err != nil {
		return 0, err
	}
	// Convert to milliseconds if it's in seconds
	if timestamp > 0 && timestamp < 1e12 {
		timestamp *= 1000
	}
	return timestamp, nil
}

// GetTotalPnL returns the total realized PnL for a trader
func (s *TradeStore) GetTotalPnL(traderID string) (float64, error) {
	var pnl float64
	err := db.QueryRow(`
		SELECT COALESCE(SUM(realized_pnl), 0) FROM trades WHERE trader_id = ?
	`, traderID).Scan(&pnl)
	return pnl, err
}

// GetTradeStats returns trade statistics for a trader
func (s *TradeStore) GetTradeStats(traderID string) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total trades
	var totalTrades int
	db.QueryRow(`SELECT COUNT(*) FROM trades WHERE trader_id = ?`, traderID).Scan(&totalTrades)
	stats["total_trades"] = totalTrades

	// Total PnL
	var totalPnL float64
	db.QueryRow(`SELECT COALESCE(SUM(realized_pnl), 0) FROM trades WHERE trader_id = ?`, traderID).Scan(&totalPnL)
	stats["total_pnl"] = totalPnL

	// Winning/Losing trades
	var winningTrades, losingTrades int
	db.QueryRow(`SELECT COUNT(*) FROM trades WHERE trader_id = ? AND realized_pnl > 0`, traderID).Scan(&winningTrades)
	db.QueryRow(`SELECT COUNT(*) FROM trades WHERE trader_id = ? AND realized_pnl < 0`, traderID).Scan(&losingTrades)
	stats["winning_trades"] = winningTrades
	stats["losing_trades"] = losingTrades

	// Win rate
	if totalTrades > 0 {
		stats["win_rate"] = float64(winningTrades) / float64(totalTrades) * 100
	} else {
		stats["win_rate"] = 0.0
	}

	// Total commission
	var totalCommission float64
	db.QueryRow(`SELECT COALESCE(SUM(commission), 0) FROM trades WHERE trader_id = ?`, traderID).Scan(&totalCommission)
	stats["total_commission"] = totalCommission

	return stats, nil
}
