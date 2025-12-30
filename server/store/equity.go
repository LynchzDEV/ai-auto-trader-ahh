package store

import (
	"time"
)

// EquitySnapshot represents account equity at a point in time
type EquitySnapshot struct {
	ID            int64     `json:"id"`
	TraderID      string    `json:"trader_id"`
	Timestamp     time.Time `json:"timestamp"`
	TotalEquity   float64   `json:"total_equity"`
	Balance       float64   `json:"balance"`
	UnrealizedPnL float64   `json:"unrealized_pnl"`
	PositionCount int       `json:"position_count"`
	MarginUsagePct float64  `json:"margin_usage_pct"`
}

// EquityStore manages equity snapshot data
type EquityStore struct{}

// NewEquityStore creates a new equity store
func NewEquityStore() *EquityStore {
	return &EquityStore{}
}

// InitTables creates the equity tables
func (s *EquityStore) InitTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS trader_equity_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trader_id TEXT NOT NULL,
		timestamp DATETIME NOT NULL,
		total_equity REAL NOT NULL,
		balance REAL,
		unrealized_pnl REAL,
		position_count INTEGER,
		margin_usage_pct REAL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_equity_trader ON trader_equity_snapshots(trader_id);
	CREATE INDEX IF NOT EXISTS idx_equity_timestamp ON trader_equity_snapshots(timestamp);
	CREATE INDEX IF NOT EXISTS idx_equity_trader_time ON trader_equity_snapshots(trader_id, timestamp);
	`
	_, err := db.Exec(query)
	return err
}

// Save records an equity snapshot
func (s *EquityStore) Save(snapshot *EquitySnapshot) error {
	query := `
	INSERT INTO trader_equity_snapshots (
		trader_id, timestamp, total_equity, balance,
		unrealized_pnl, position_count, margin_usage_pct
	) VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.Exec(query,
		snapshot.TraderID, snapshot.Timestamp, snapshot.TotalEquity, snapshot.Balance,
		snapshot.UnrealizedPnL, snapshot.PositionCount, snapshot.MarginUsagePct,
	)
	return err
}

// GetLatest returns the N most recent snapshots (chronological order for plotting)
func (s *EquityStore) GetLatest(traderID string, limit int) ([]EquitySnapshot, error) {
	// Get in reverse order (newest first), then reverse for chronological
	query := `
	SELECT id, trader_id, timestamp, total_equity, COALESCE(balance, 0),
		COALESCE(unrealized_pnl, 0), COALESCE(position_count, 0), COALESCE(margin_usage_pct, 0)
	FROM trader_equity_snapshots
	WHERE trader_id = ?
	ORDER BY timestamp DESC
	LIMIT ?
	`
	rows, err := db.Query(query, traderID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []EquitySnapshot
	for rows.Next() {
		var s EquitySnapshot
		err := rows.Scan(
			&s.ID, &s.TraderID, &s.Timestamp, &s.TotalEquity, &s.Balance,
			&s.UnrealizedPnL, &s.PositionCount, &s.MarginUsagePct,
		)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}

	// Reverse for chronological order
	for i, j := 0, len(snapshots)-1; i < j; i, j = i+1, j-1 {
		snapshots[i], snapshots[j] = snapshots[j], snapshots[i]
	}

	return snapshots, nil
}

// GetByTimeRange returns snapshots within a time range
func (s *EquityStore) GetByTimeRange(traderID string, start, end time.Time) ([]EquitySnapshot, error) {
	query := `
	SELECT id, trader_id, timestamp, total_equity, COALESCE(balance, 0),
		COALESCE(unrealized_pnl, 0), COALESCE(position_count, 0), COALESCE(margin_usage_pct, 0)
	FROM trader_equity_snapshots
	WHERE trader_id = ? AND timestamp BETWEEN ? AND ?
	ORDER BY timestamp ASC
	`
	rows, err := db.Query(query, traderID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []EquitySnapshot
	for rows.Next() {
		var s EquitySnapshot
		err := rows.Scan(
			&s.ID, &s.TraderID, &s.Timestamp, &s.TotalEquity, &s.Balance,
			&s.UnrealizedPnL, &s.PositionCount, &s.MarginUsagePct,
		)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}

	return snapshots, nil
}

// GetAllTradersLatest returns the latest equity for all traders
func (s *EquityStore) GetAllTradersLatest() ([]EquitySnapshot, error) {
	query := `
	SELECT e.id, e.trader_id, e.timestamp, e.total_equity, COALESCE(e.balance, 0),
		COALESCE(e.unrealized_pnl, 0), COALESCE(e.position_count, 0), COALESCE(e.margin_usage_pct, 0)
	FROM trader_equity_snapshots e
	INNER JOIN (
		SELECT trader_id, MAX(timestamp) as max_ts
		FROM trader_equity_snapshots
		GROUP BY trader_id
	) latest ON e.trader_id = latest.trader_id AND e.timestamp = latest.max_ts
	ORDER BY e.total_equity DESC
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []EquitySnapshot
	for rows.Next() {
		var s EquitySnapshot
		err := rows.Scan(
			&s.ID, &s.TraderID, &s.Timestamp, &s.TotalEquity, &s.Balance,
			&s.UnrealizedPnL, &s.PositionCount, &s.MarginUsagePct,
		)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}

	return snapshots, nil
}

// CleanOldRecords deletes records older than specified days
func (s *EquityStore) CleanOldRecords(traderID string, retentionDays int) error {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	query := `DELETE FROM trader_equity_snapshots WHERE trader_id = ? AND timestamp < ?`
	_, err := db.Exec(query, traderID, cutoff)
	return err
}

// GetEquityChange calculates equity change over a period
func (s *EquityStore) GetEquityChange(traderID string, hours int) (float64, float64, error) {
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)

	// Get earliest snapshot after cutoff
	var startEquity float64
	err := db.QueryRow(`
		SELECT total_equity FROM trader_equity_snapshots
		WHERE trader_id = ? AND timestamp >= ?
		ORDER BY timestamp ASC LIMIT 1
	`, traderID, cutoff).Scan(&startEquity)
	if err != nil {
		return 0, 0, nil // No data
	}

	// Get latest snapshot
	var endEquity float64
	err = db.QueryRow(`
		SELECT total_equity FROM trader_equity_snapshots
		WHERE trader_id = ?
		ORDER BY timestamp DESC LIMIT 1
	`, traderID).Scan(&endEquity)
	if err != nil {
		return 0, 0, nil
	}

	change := endEquity - startEquity
	changePct := 0.0
	if startEquity > 0 {
		changePct = (change / startEquity) * 100
	}

	return change, changePct, nil
}

// GetPeakEquity returns the highest equity recorded
func (s *EquityStore) GetPeakEquity(traderID string) (float64, time.Time, error) {
	var peak float64
	var timestamp time.Time
	err := db.QueryRow(`
		SELECT total_equity, timestamp FROM trader_equity_snapshots
		WHERE trader_id = ?
		ORDER BY total_equity DESC LIMIT 1
	`, traderID).Scan(&peak, &timestamp)
	if err != nil {
		return 0, time.Time{}, err
	}
	return peak, timestamp, nil
}

// GetEquityCurveForChart returns equity data formatted for charting
func (s *EquityStore) GetEquityCurveForChart(traderID string, points int) ([]map[string]interface{}, error) {
	snapshots, err := s.GetLatest(traderID, points)
	if err != nil {
		return nil, err
	}

	var result []map[string]interface{}
	for _, snap := range snapshots {
		result = append(result, map[string]interface{}{
			"timestamp": snap.Timestamp.UnixMilli(),
			"equity":    snap.TotalEquity,
			"balance":   snap.Balance,
			"pnl":       snap.UnrealizedPnL,
		})
	}
	return result, nil
}

// GetDrawdownStats calculates drawdown statistics
func (s *EquityStore) GetDrawdownStats(traderID string) (float64, float64, float64, error) {
	snapshots, err := s.GetLatest(traderID, 1000)
	if err != nil {
		return 0, 0, 0, err
	}

	if len(snapshots) == 0 {
		return 0, 0, 0, nil
	}

	peak := snapshots[0].TotalEquity
	maxDrawdown := 0.0
	currentDrawdown := 0.0

	for _, snap := range snapshots {
		if snap.TotalEquity > peak {
			peak = snap.TotalEquity
		}
		if peak > 0 {
			drawdown := (peak - snap.TotalEquity) / peak * 100
			if drawdown > maxDrawdown {
				maxDrawdown = drawdown
			}
			currentDrawdown = drawdown
		}
	}

	return maxDrawdown, currentDrawdown, peak, nil
}
