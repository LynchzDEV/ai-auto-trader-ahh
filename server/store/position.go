package store

import (
	"database/sql"
	"fmt"
	"math"
	"time"
)

// PositionStatus constants
const (
	PositionStatusOpen   = "OPEN"
	PositionStatusClosed = "CLOSED"
)

// PositionSource constants
const (
	PositionSourceSystem = "system"
	PositionSourceManual = "manual"
	PositionSourceSync   = "sync"
)

// TraderPosition represents a complete position lifecycle
type TraderPosition struct {
	ID                 int64     `json:"id"`
	TraderID           string    `json:"trader_id"`
	ExchangeID         string    `json:"exchange_id"`
	ExchangeType       string    `json:"exchange_type"`
	ExchangePositionID string    `json:"exchange_position_id"`
	Symbol             string    `json:"symbol"`
	Side               string    `json:"side"` // "long" or "short"
	EntryQuantity      float64   `json:"entry_quantity"`
	Quantity           float64   `json:"quantity"` // current remaining
	EntryPrice         float64   `json:"entry_price"`
	ExitPrice          float64   `json:"exit_price"`
	EntryOrderID       string    `json:"entry_order_id"`
	ExitOrderID        string    `json:"exit_order_id"`
	EntryTime          time.Time `json:"entry_time"`
	ExitTime           time.Time `json:"exit_time"`
	RealizedPnL        float64   `json:"realized_pnl"`
	Fee                float64   `json:"fee"`
	Leverage           int       `json:"leverage"`
	Status             string    `json:"status"` // OPEN, CLOSED
	CloseReason        string    `json:"close_reason"`
	Source             string    `json:"source"` // system, manual, sync
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// TraderStats represents trading performance metrics
type TraderStats struct {
	TotalTrades    int     `json:"total_trades"`
	WinTrades      int     `json:"win_trades"`
	LossTrades     int     `json:"loss_trades"`
	WinRate        float64 `json:"win_rate"`
	ProfitFactor   float64 `json:"profit_factor"`
	SharpeRatio    float64 `json:"sharpe_ratio"`
	TotalPnL       float64 `json:"total_pnl"`
	TotalFees      float64 `json:"total_fees"`
	AvgWin         float64 `json:"avg_win"`
	AvgLoss        float64 `json:"avg_loss"`
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
}

// SymbolStats represents per-symbol performance
type SymbolStats struct {
	Symbol      string  `json:"symbol"`
	TotalTrades int     `json:"total_trades"`
	WinTrades   int     `json:"win_trades"`
	WinRate     float64 `json:"win_rate"`
	TotalPnL    float64 `json:"total_pnl"`
	AvgPnL      float64 `json:"avg_pnl"`
	AvgHoldMins float64 `json:"avg_hold_mins"`
}

// DirectionStats represents long vs short comparison
type DirectionStats struct {
	Side       string  `json:"side"`
	TradeCount int     `json:"trade_count"`
	WinRate    float64 `json:"win_rate"`
	TotalPnL   float64 `json:"total_pnl"`
	AvgPnL     float64 `json:"avg_pnl"`
}

// HoldingTimeStats represents performance by holding duration
type HoldingTimeStats struct {
	Bucket     string  `json:"bucket"` // <1h, 1-4h, 4-24h, >24h
	TradeCount int     `json:"trade_count"`
	WinRate    float64 `json:"win_rate"`
	AvgPnL     float64 `json:"avg_pnl"`
}

// HistorySummary is comprehensive AI context
type HistorySummary struct {
	OverallStats   TraderStats        `json:"overall_stats"`
	BestSymbols    []SymbolStats      `json:"best_symbols"`
	WorstSymbols   []SymbolStats      `json:"worst_symbols"`
	LongStats      DirectionStats     `json:"long_stats"`
	ShortStats     DirectionStats     `json:"short_stats"`
	HoldingTime    []HoldingTimeStats `json:"holding_time"`
	AvgHoldMins    float64            `json:"avg_hold_mins"`
	RecentWinRate  float64            `json:"recent_win_rate"`
	WinStreak      int                `json:"win_streak"`
	LoseStreak     int                `json:"lose_streak"`
	CurrentStreak  int                `json:"current_streak"`
}

// PositionStore manages position data
type PositionStore struct{}

// NewPositionStore creates a new position store
func NewPositionStore() *PositionStore {
	return &PositionStore{}
}

// InitTables creates the position tables
func (s *PositionStore) InitTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS trader_positions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trader_id TEXT NOT NULL,
		exchange_id TEXT,
		exchange_type TEXT,
		exchange_position_id TEXT,
		symbol TEXT NOT NULL,
		side TEXT NOT NULL,
		entry_quantity REAL NOT NULL,
		quantity REAL NOT NULL,
		entry_price REAL NOT NULL,
		exit_price REAL DEFAULT 0,
		entry_order_id TEXT,
		exit_order_id TEXT,
		entry_time DATETIME NOT NULL,
		exit_time DATETIME,
		realized_pnl REAL DEFAULT 0,
		fee REAL DEFAULT 0,
		leverage INTEGER DEFAULT 1,
		status TEXT NOT NULL DEFAULT 'OPEN',
		close_reason TEXT,
		source TEXT DEFAULT 'system',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_positions_trader ON trader_positions(trader_id);
	CREATE INDEX IF NOT EXISTS idx_positions_symbol ON trader_positions(symbol);
	CREATE INDEX IF NOT EXISTS idx_positions_status ON trader_positions(status);
	CREATE INDEX IF NOT EXISTS idx_positions_exchange ON trader_positions(exchange_id, exchange_position_id);
	`
	_, err := db.Exec(query)
	return err
}

// Create creates a new position
func (s *PositionStore) Create(pos *TraderPosition) (int64, error) {
	query := `
	INSERT INTO trader_positions (
		trader_id, exchange_id, exchange_type, exchange_position_id,
		symbol, side, entry_quantity, quantity, entry_price,
		entry_order_id, entry_time, leverage, status, source
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	result, err := db.Exec(query,
		pos.TraderID, pos.ExchangeID, pos.ExchangeType, pos.ExchangePositionID,
		pos.Symbol, pos.Side, pos.EntryQuantity, pos.Quantity, pos.EntryPrice,
		pos.EntryOrderID, pos.EntryTime, pos.Leverage, PositionStatusOpen, pos.Source,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetOpenPositions returns all open positions for a trader
func (s *PositionStore) GetOpenPositions(traderID string) ([]TraderPosition, error) {
	query := `
	SELECT id, trader_id, exchange_id, exchange_type, exchange_position_id,
		symbol, side, entry_quantity, quantity, entry_price, exit_price,
		entry_order_id, exit_order_id, entry_time, COALESCE(exit_time, ''),
		realized_pnl, fee, leverage, status, close_reason, source, created_at, updated_at
	FROM trader_positions
	WHERE trader_id = ? AND status = ?
	ORDER BY entry_time DESC
	`
	rows, err := db.Query(query, traderID, PositionStatusOpen)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanPositions(rows)
}

// GetClosedPositions returns closed positions for a trader
func (s *PositionStore) GetClosedPositions(traderID string, limit int) ([]TraderPosition, error) {
	query := `
	SELECT id, trader_id, exchange_id, exchange_type, exchange_position_id,
		symbol, side, entry_quantity, quantity, entry_price, exit_price,
		entry_order_id, exit_order_id, entry_time, COALESCE(exit_time, ''),
		realized_pnl, fee, leverage, status, close_reason, source, created_at, updated_at
	FROM trader_positions
	WHERE trader_id = ? AND status = ?
	ORDER BY exit_time DESC
	LIMIT ?
	`
	rows, err := db.Query(query, traderID, PositionStatusClosed, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanPositions(rows)
}

// scanPositions scans rows into positions
func (s *PositionStore) scanPositions(rows *sql.Rows) ([]TraderPosition, error) {
	var positions []TraderPosition
	for rows.Next() {
		var pos TraderPosition
		var exitTimeStr string
		err := rows.Scan(
			&pos.ID, &pos.TraderID, &pos.ExchangeID, &pos.ExchangeType, &pos.ExchangePositionID,
			&pos.Symbol, &pos.Side, &pos.EntryQuantity, &pos.Quantity, &pos.EntryPrice, &pos.ExitPrice,
			&pos.EntryOrderID, &pos.ExitOrderID, &pos.EntryTime, &exitTimeStr,
			&pos.RealizedPnL, &pos.Fee, &pos.Leverage, &pos.Status, &pos.CloseReason, &pos.Source,
			&pos.CreatedAt, &pos.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if exitTimeStr != "" {
			pos.ExitTime, _ = time.Parse(time.RFC3339, exitTimeStr)
		}
		positions = append(positions, pos)
	}
	return positions, nil
}

// UpdatePositionQuantityAndPrice handles scale-in with weighted average
func (s *PositionStore) UpdatePositionQuantityAndPrice(id int64, addQty, addPrice float64) error {
	// Get current position
	var currentQty, currentPrice float64
	err := db.QueryRow("SELECT quantity, entry_price FROM trader_positions WHERE id = ?", id).
		Scan(&currentQty, &currentPrice)
	if err != nil {
		return err
	}

	// Calculate weighted average entry price
	newQty := currentQty + addQty
	newPrice := (currentPrice*currentQty + addPrice*addQty) / newQty
	newEntryQty := newQty // Update entry_quantity to track total

	query := `
	UPDATE trader_positions
	SET quantity = ?, entry_quantity = ?, entry_price = ?, updated_at = CURRENT_TIMESTAMP
	WHERE id = ?
	`
	_, err = db.Exec(query, newQty, newEntryQty, newPrice, id)
	return err
}

// ReducePositionQuantity handles partial close with weighted exit
func (s *PositionStore) ReducePositionQuantity(id int64, reduceQty, exitPrice, fee, pnl float64) error {
	// Get current position
	var currentQty, currentExitPrice, currentFee, currentPnL, entryQty float64
	err := db.QueryRow(`
		SELECT quantity, exit_price, fee, realized_pnl, entry_quantity
		FROM trader_positions WHERE id = ?
	`, id).Scan(&currentQty, &currentExitPrice, &currentFee, &currentPnL, &entryQty)
	if err != nil {
		return err
	}

	// Calculate weighted average exit price
	closedQty := entryQty - currentQty
	newClosedQty := closedQty + reduceQty
	var newExitPrice float64
	if closedQty > 0 {
		newExitPrice = (currentExitPrice*closedQty + exitPrice*reduceQty) / newClosedQty
	} else {
		newExitPrice = exitPrice
	}

	// Update position
	newQty := currentQty - reduceQty
	newFee := currentFee + fee
	newPnL := currentPnL + pnl

	query := `
	UPDATE trader_positions
	SET quantity = ?, exit_price = ?, fee = ?, realized_pnl = ?, updated_at = CURRENT_TIMESTAMP
	WHERE id = ?
	`
	_, err = db.Exec(query, newQty, newExitPrice, newFee, newPnL, id)
	return err
}

// ClosePosition marks a position as closed
func (s *PositionStore) ClosePosition(id int64, exitPrice, fee, pnl float64, reason string) error {
	// Restore quantity to entry_quantity for historical display
	query := `
	UPDATE trader_positions
	SET status = ?, exit_price = ?, exit_time = ?, fee = fee + ?,
		realized_pnl = realized_pnl + ?, close_reason = ?,
		quantity = entry_quantity, updated_at = CURRENT_TIMESTAMP
	WHERE id = ?
	`
	_, err := db.Exec(query, PositionStatusClosed, exitPrice, time.Now(), fee, pnl, reason, id)
	return err
}

// GetFullStats calculates complete trading statistics
func (s *PositionStore) GetFullStats(traderID string) (*TraderStats, error) {
	positions, err := s.GetClosedPositions(traderID, 1000)
	if err != nil {
		return nil, err
	}

	if len(positions) == 0 {
		return &TraderStats{}, nil
	}

	stats := &TraderStats{}
	var totalWin, totalLoss float64
	var pnls []float64

	for _, pos := range positions {
		stats.TotalTrades++
		stats.TotalPnL += pos.RealizedPnL
		stats.TotalFees += pos.Fee
		pnls = append(pnls, pos.RealizedPnL)

		if pos.RealizedPnL > 0 {
			stats.WinTrades++
			totalWin += pos.RealizedPnL
		} else {
			stats.LossTrades++
			totalLoss += math.Abs(pos.RealizedPnL)
		}
	}

	if stats.TotalTrades > 0 {
		stats.WinRate = float64(stats.WinTrades) / float64(stats.TotalTrades) * 100
	}
	if totalLoss > 0 {
		stats.ProfitFactor = totalWin / totalLoss
	}
	if stats.WinTrades > 0 {
		stats.AvgWin = totalWin / float64(stats.WinTrades)
	}
	if stats.LossTrades > 0 {
		stats.AvgLoss = totalLoss / float64(stats.LossTrades)
	}

	// Calculate Sharpe ratio
	stats.SharpeRatio = calculateSharpeRatio(pnls)

	// Calculate max drawdown
	stats.MaxDrawdownPct = calculateMaxDrawdown(pnls)

	return stats, nil
}

// GetHistorySummary returns comprehensive trading history for AI context
func (s *PositionStore) GetHistorySummary(traderID string) (*HistorySummary, error) {
	stats, err := s.GetFullStats(traderID)
	if err != nil {
		return nil, err
	}

	summary := &HistorySummary{
		OverallStats: *stats,
	}

	// Get symbol stats
	summary.BestSymbols, summary.WorstSymbols, err = s.getSymbolStats(traderID)
	if err != nil {
		return nil, err
	}

	// Get direction stats
	summary.LongStats, summary.ShortStats, err = s.getDirectionStats(traderID)
	if err != nil {
		return nil, err
	}

	// Get holding time stats
	summary.HoldingTime, summary.AvgHoldMins, err = s.getHoldingTimeStats(traderID)
	if err != nil {
		return nil, err
	}

	// Get streaks
	summary.WinStreak, summary.LoseStreak, summary.CurrentStreak, err = s.getStreaks(traderID)
	if err != nil {
		return nil, err
	}

	// Get recent win rate (last 20 trades)
	summary.RecentWinRate, err = s.getRecentWinRate(traderID, 20)
	if err != nil {
		return nil, err
	}

	return summary, nil
}

func (s *PositionStore) getSymbolStats(traderID string) ([]SymbolStats, []SymbolStats, error) {
	query := `
	SELECT symbol,
		COUNT(*) as total,
		SUM(CASE WHEN realized_pnl > 0 THEN 1 ELSE 0 END) as wins,
		SUM(realized_pnl) as total_pnl,
		AVG((julianday(exit_time) - julianday(entry_time)) * 24 * 60) as avg_hold
	FROM trader_positions
	WHERE trader_id = ? AND status = 'CLOSED'
	GROUP BY symbol
	ORDER BY total_pnl DESC
	`
	rows, err := db.Query(query, traderID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var all []SymbolStats
	for rows.Next() {
		var s SymbolStats
		err := rows.Scan(&s.Symbol, &s.TotalTrades, &s.WinTrades, &s.TotalPnL, &s.AvgHoldMins)
		if err != nil {
			return nil, nil, err
		}
		if s.TotalTrades > 0 {
			s.WinRate = float64(s.WinTrades) / float64(s.TotalTrades) * 100
			s.AvgPnL = s.TotalPnL / float64(s.TotalTrades)
		}
		all = append(all, s)
	}

	// Best (top 3 by PnL)
	var best, worst []SymbolStats
	for i := 0; i < len(all) && i < 3; i++ {
		best = append(best, all[i])
	}
	// Worst (bottom 3 by PnL)
	for i := len(all) - 1; i >= 0 && len(worst) < 3; i-- {
		if all[i].TotalPnL < 0 {
			worst = append(worst, all[i])
		}
	}

	return best, worst, nil
}

func (s *PositionStore) getDirectionStats(traderID string) (DirectionStats, DirectionStats, error) {
	query := `
	SELECT side,
		COUNT(*) as total,
		SUM(CASE WHEN realized_pnl > 0 THEN 1 ELSE 0 END) as wins,
		SUM(realized_pnl) as total_pnl
	FROM trader_positions
	WHERE trader_id = ? AND status = 'CLOSED'
	GROUP BY side
	`
	rows, err := db.Query(query, traderID)
	if err != nil {
		return DirectionStats{}, DirectionStats{}, err
	}
	defer rows.Close()

	var longStats, shortStats DirectionStats
	for rows.Next() {
		var side string
		var total, wins int
		var totalPnL float64
		err := rows.Scan(&side, &total, &wins, &totalPnL)
		if err != nil {
			return DirectionStats{}, DirectionStats{}, err
		}

		stats := DirectionStats{
			Side:       side,
			TradeCount: total,
			TotalPnL:   totalPnL,
		}
		if total > 0 {
			stats.WinRate = float64(wins) / float64(total) * 100
			stats.AvgPnL = totalPnL / float64(total)
		}

		if side == "long" {
			longStats = stats
		} else {
			shortStats = stats
		}
	}

	return longStats, shortStats, nil
}

func (s *PositionStore) getHoldingTimeStats(traderID string) ([]HoldingTimeStats, float64, error) {
	query := `
	SELECT
		CASE
			WHEN (julianday(exit_time) - julianday(entry_time)) * 24 < 1 THEN '<1h'
			WHEN (julianday(exit_time) - julianday(entry_time)) * 24 < 4 THEN '1-4h'
			WHEN (julianday(exit_time) - julianday(entry_time)) * 24 < 24 THEN '4-24h'
			ELSE '>24h'
		END as bucket,
		COUNT(*) as total,
		SUM(CASE WHEN realized_pnl > 0 THEN 1 ELSE 0 END) as wins,
		AVG(realized_pnl) as avg_pnl
	FROM trader_positions
	WHERE trader_id = ? AND status = 'CLOSED'
	GROUP BY bucket
	`
	rows, err := db.Query(query, traderID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var stats []HoldingTimeStats
	for rows.Next() {
		var s HoldingTimeStats
		var wins int
		err := rows.Scan(&s.Bucket, &s.TradeCount, &wins, &s.AvgPnL)
		if err != nil {
			return nil, 0, err
		}
		if s.TradeCount > 0 {
			s.WinRate = float64(wins) / float64(s.TradeCount) * 100
		}
		stats = append(stats, s)
	}

	// Calculate average hold time
	var avgHold float64
	err = db.QueryRow(`
		SELECT AVG((julianday(exit_time) - julianday(entry_time)) * 24 * 60)
		FROM trader_positions WHERE trader_id = ? AND status = 'CLOSED'
	`, traderID).Scan(&avgHold)

	return stats, avgHold, err
}

func (s *PositionStore) getStreaks(traderID string) (int, int, int, error) {
	positions, err := s.GetClosedPositions(traderID, 100)
	if err != nil {
		return 0, 0, 0, err
	}

	if len(positions) == 0 {
		return 0, 0, 0, nil
	}

	var maxWin, maxLose, current int
	for _, pos := range positions {
		if pos.RealizedPnL > 0 {
			if current >= 0 {
				current++
			} else {
				current = 1
			}
			if current > maxWin {
				maxWin = current
			}
		} else {
			if current <= 0 {
				current--
			} else {
				current = -1
			}
			if -current > maxLose {
				maxLose = -current
			}
		}
	}

	return maxWin, maxLose, current, nil
}

func (s *PositionStore) getRecentWinRate(traderID string, n int) (float64, error) {
	var wins, total int
	err := db.QueryRow(`
		SELECT
			SUM(CASE WHEN realized_pnl > 0 THEN 1 ELSE 0 END),
			COUNT(*)
		FROM (
			SELECT realized_pnl FROM trader_positions
			WHERE trader_id = ? AND status = 'CLOSED'
			ORDER BY exit_time DESC LIMIT ?
		)
	`, traderID, n).Scan(&wins, &total)
	if err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, nil
	}
	return float64(wins) / float64(total) * 100, nil
}

// calculateSharpeRatio calculates the Sharpe ratio from PnL values
func calculateSharpeRatio(pnls []float64) float64 {
	if len(pnls) < 2 {
		return 0
	}

	// Calculate mean
	var sum float64
	for _, pnl := range pnls {
		sum += pnl
	}
	mean := sum / float64(len(pnls))

	// Calculate standard deviation
	var variance float64
	for _, pnl := range pnls {
		variance += (pnl - mean) * (pnl - mean)
	}
	variance /= float64(len(pnls) - 1)
	stdDev := math.Sqrt(variance)

	if stdDev == 0 {
		return 0
	}

	return mean / stdDev
}

// calculateMaxDrawdown calculates max drawdown from PnL sequence
func calculateMaxDrawdown(pnls []float64) float64 {
	if len(pnls) == 0 {
		return 0
	}

	equity := 10000.0 // Virtual starting equity
	peak := equity
	maxDrawdown := 0.0

	for _, pnl := range pnls {
		equity += pnl
		if equity > peak {
			peak = equity
		}
		drawdown := (peak - equity) / peak * 100
		if drawdown > maxDrawdown {
			maxDrawdown = drawdown
		}
	}

	return maxDrawdown
}

// GetOpenPositionBySymbol returns open position for a symbol
func (s *PositionStore) GetOpenPositionBySymbol(traderID, symbol, side string) (*TraderPosition, error) {
	query := `
	SELECT id, trader_id, exchange_id, exchange_type, exchange_position_id,
		symbol, side, entry_quantity, quantity, entry_price, exit_price,
		entry_order_id, exit_order_id, entry_time, COALESCE(exit_time, ''),
		realized_pnl, fee, leverage, status, close_reason, source, created_at, updated_at
	FROM trader_positions
	WHERE trader_id = ? AND symbol = ? AND side = ? AND status = ?
	`
	var pos TraderPosition
	var exitTimeStr string
	err := db.QueryRow(query, traderID, symbol, side, PositionStatusOpen).Scan(
		&pos.ID, &pos.TraderID, &pos.ExchangeID, &pos.ExchangeType, &pos.ExchangePositionID,
		&pos.Symbol, &pos.Side, &pos.EntryQuantity, &pos.Quantity, &pos.EntryPrice, &pos.ExitPrice,
		&pos.EntryOrderID, &pos.ExitOrderID, &pos.EntryTime, &exitTimeStr,
		&pos.RealizedPnL, &pos.Fee, &pos.Leverage, &pos.Status, &pos.CloseReason, &pos.Source,
		&pos.CreatedAt, &pos.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if exitTimeStr != "" {
		pos.ExitTime, _ = time.Parse(time.RFC3339, exitTimeStr)
	}
	return &pos, nil
}

// ExistsWithExchangePositionID checks if position exists by exchange ID
func (s *PositionStore) ExistsWithExchangePositionID(exchangeID, exchangePositionID string) (bool, error) {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM trader_positions
		WHERE exchange_id = ? AND exchange_position_id = ?
	`, exchangeID, exchangePositionID).Scan(&count)
	return count > 0, err
}

// GetRecentClosedForAI returns recent closed positions formatted for AI context
func (s *PositionStore) GetRecentClosedForAI(traderID string, limit int) ([]map[string]interface{}, error) {
	positions, err := s.GetClosedPositions(traderID, limit)
	if err != nil {
		return nil, err
	}

	var result []map[string]interface{}
	for _, pos := range positions {
		holdDuration := pos.ExitTime.Sub(pos.EntryTime)
		result = append(result, map[string]interface{}{
			"symbol":       pos.Symbol,
			"side":         pos.Side,
			"entry_price":  pos.EntryPrice,
			"exit_price":   pos.ExitPrice,
			"realized_pnl": pos.RealizedPnL,
			"hold_minutes": holdDuration.Minutes(),
			"entry_time":   pos.EntryTime.Format(time.RFC3339),
			"exit_time":    pos.ExitTime.Format(time.RFC3339),
		})
	}
	return result, nil
}
