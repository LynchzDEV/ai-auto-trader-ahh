package store

import (
	"database/sql"
	"time"
)

// Order status constants
const (
	OrderStatusNew             = "NEW"
	OrderStatusPartiallyFilled = "PARTIALLY_FILLED"
	OrderStatusFilled          = "FILLED"
	OrderStatusCanceled        = "CANCELED"
	OrderStatusRejected        = "REJECTED"
	OrderStatusExpired         = "EXPIRED"
)

// Order type constants
const (
	OrderTypeMarket     = "MARKET"
	OrderTypeLimit      = "LIMIT"
	OrderTypeStopMarket = "STOP_MARKET"
	OrderTypeStopLimit  = "STOP_LIMIT"
	OrderTypeTakeProfit = "TAKE_PROFIT"
)

// TraderOrder represents a complete order record
type TraderOrder struct {
	ID              int64     `json:"id"`
	TraderID        string    `json:"trader_id"`
	ExchangeID      string    `json:"exchange_id"`
	ExchangeType    string    `json:"exchange_type"`
	ExchangeOrderID string    `json:"exchange_order_id"`
	ClientOrderID   string    `json:"client_order_id"`
	Symbol          string    `json:"symbol"`
	Side            string    `json:"side"`          // BUY, SELL
	PositionSide    string    `json:"position_side"` // LONG, SHORT
	Type            string    `json:"type"`          // MARKET, LIMIT, STOP, etc.
	TimeInForce     string    `json:"time_in_force"` // GTC, IOC, FOK
	Quantity        float64   `json:"quantity"`
	Price           float64   `json:"price"`
	StopPrice       float64   `json:"stop_price"`
	Status          string    `json:"status"`
	FilledQuantity  float64   `json:"filled_quantity"`
	AvgFillPrice    float64   `json:"avg_fill_price"`
	Commission      float64   `json:"commission"`
	Leverage        int       `json:"leverage"`
	ReduceOnly      bool      `json:"reduce_only"`
	ClosePosition   bool      `json:"close_position"`
	WorkingType     string    `json:"working_type"` // MARK_PRICE, CONTRACT_PRICE
	PriceProtect    bool      `json:"price_protect"`
	OrderAction     string    `json:"order_action"` // OPEN, CLOSE, ADD, REDUCE
	PositionID      int64     `json:"position_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	FilledAt        time.Time `json:"filled_at"`
}

// TraderFill represents an individual fill record
type TraderFill struct {
	ID              int64     `json:"id"`
	TraderID        string    `json:"trader_id"`
	OrderID         int64     `json:"order_id"`
	ExchangeID      string    `json:"exchange_id"`
	ExchangeTradeID string    `json:"exchange_trade_id"`
	Symbol          string    `json:"symbol"`
	Side            string    `json:"side"`
	Price           float64   `json:"price"`
	Quantity        float64   `json:"quantity"`
	QuoteQuantity   float64   `json:"quote_quantity"`
	Commission      float64   `json:"commission"`
	RealizedPnL     float64   `json:"realized_pnl"`
	IsMaker         bool      `json:"is_maker"`
	Timestamp       time.Time `json:"timestamp"`
	CreatedAt       time.Time `json:"created_at"`
}

// OrderStore manages order data
type OrderStore struct{}

// NewOrderStore creates a new order store
func NewOrderStore() *OrderStore {
	return &OrderStore{}
}

// InitTables creates the order tables
func (s *OrderStore) InitTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS trader_orders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trader_id TEXT NOT NULL,
		exchange_id TEXT,
		exchange_type TEXT,
		exchange_order_id TEXT,
		client_order_id TEXT,
		symbol TEXT NOT NULL,
		side TEXT NOT NULL,
		position_side TEXT,
		type TEXT NOT NULL,
		time_in_force TEXT,
		quantity REAL NOT NULL,
		price REAL,
		stop_price REAL,
		status TEXT NOT NULL,
		filled_quantity REAL DEFAULT 0,
		avg_fill_price REAL DEFAULT 0,
		commission REAL DEFAULT 0,
		leverage INTEGER DEFAULT 1,
		reduce_only BOOLEAN DEFAULT 0,
		close_position BOOLEAN DEFAULT 0,
		working_type TEXT,
		price_protect BOOLEAN DEFAULT 0,
		order_action TEXT,
		position_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		filled_at DATETIME,
		UNIQUE(exchange_id, exchange_order_id)
	);

	CREATE INDEX IF NOT EXISTS idx_orders_trader ON trader_orders(trader_id);
	CREATE INDEX IF NOT EXISTS idx_orders_symbol ON trader_orders(symbol);
	CREATE INDEX IF NOT EXISTS idx_orders_status ON trader_orders(status);
	CREATE INDEX IF NOT EXISTS idx_orders_exchange ON trader_orders(exchange_id, exchange_order_id);

	CREATE TABLE IF NOT EXISTS trader_fills (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trader_id TEXT NOT NULL,
		order_id INTEGER,
		exchange_id TEXT,
		exchange_trade_id TEXT,
		symbol TEXT NOT NULL,
		side TEXT NOT NULL,
		price REAL NOT NULL,
		quantity REAL NOT NULL,
		quote_quantity REAL,
		commission REAL DEFAULT 0,
		realized_pnl REAL DEFAULT 0,
		is_maker BOOLEAN DEFAULT 0,
		timestamp DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(exchange_id, exchange_trade_id)
	);

	CREATE INDEX IF NOT EXISTS idx_fills_trader ON trader_fills(trader_id);
	CREATE INDEX IF NOT EXISTS idx_fills_order ON trader_fills(order_id);
	CREATE INDEX IF NOT EXISTS idx_fills_symbol ON trader_fills(symbol);
	`
	_, err := db.Exec(query)
	return err
}

// CreateOrder creates a new order with deduplication
func (s *OrderStore) CreateOrder(order *TraderOrder) (int64, error) {
	// Check if exists first
	if order.ExchangeID != "" && order.ExchangeOrderID != "" {
		var existingID int64
		err := db.QueryRow(`
			SELECT id FROM trader_orders
			WHERE exchange_id = ? AND exchange_order_id = ?
		`, order.ExchangeID, order.ExchangeOrderID).Scan(&existingID)
		if err == nil {
			return existingID, nil // Already exists
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
	}

	query := `
	INSERT INTO trader_orders (
		trader_id, exchange_id, exchange_type, exchange_order_id, client_order_id,
		symbol, side, position_side, type, time_in_force,
		quantity, price, stop_price, status, leverage,
		reduce_only, close_position, working_type, price_protect,
		order_action, position_id
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	result, err := db.Exec(query,
		order.TraderID, order.ExchangeID, order.ExchangeType, order.ExchangeOrderID, order.ClientOrderID,
		order.Symbol, order.Side, order.PositionSide, order.Type, order.TimeInForce,
		order.Quantity, order.Price, order.StopPrice, order.Status, order.Leverage,
		order.ReduceOnly, order.ClosePosition, order.WorkingType, order.PriceProtect,
		order.OrderAction, order.PositionID,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateOrderStatus updates order status and fill info
func (s *OrderStore) UpdateOrderStatus(id int64, status string, filledQty, avgPrice, commission float64) error {
	query := `
	UPDATE trader_orders
	SET status = ?, filled_quantity = ?, avg_fill_price = ?, commission = ?,
		updated_at = CURRENT_TIMESTAMP
	`
	args := []interface{}{status, filledQty, avgPrice, commission}

	if status == OrderStatusFilled {
		query += ", filled_at = CURRENT_TIMESTAMP"
	}

	query += " WHERE id = ?"
	args = append(args, id)

	_, err := db.Exec(query, args...)
	return err
}

// GetOrderByExchangeID gets an order by exchange ID
func (s *OrderStore) GetOrderByExchangeID(exchangeID, exchangeOrderID string) (*TraderOrder, error) {
	query := `
	SELECT id, trader_id, exchange_id, exchange_type, exchange_order_id, client_order_id,
		symbol, side, position_side, type, time_in_force,
		quantity, price, stop_price, status, filled_quantity, avg_fill_price,
		commission, leverage, reduce_only, close_position, working_type, price_protect,
		order_action, position_id, created_at, updated_at, COALESCE(filled_at, '')
	FROM trader_orders
	WHERE exchange_id = ? AND exchange_order_id = ?
	`
	var order TraderOrder
	var filledAtStr string
	err := db.QueryRow(query, exchangeID, exchangeOrderID).Scan(
		&order.ID, &order.TraderID, &order.ExchangeID, &order.ExchangeType, &order.ExchangeOrderID, &order.ClientOrderID,
		&order.Symbol, &order.Side, &order.PositionSide, &order.Type, &order.TimeInForce,
		&order.Quantity, &order.Price, &order.StopPrice, &order.Status, &order.FilledQuantity, &order.AvgFillPrice,
		&order.Commission, &order.Leverage, &order.ReduceOnly, &order.ClosePosition, &order.WorkingType, &order.PriceProtect,
		&order.OrderAction, &order.PositionID, &order.CreatedAt, &order.UpdatedAt, &filledAtStr,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if filledAtStr != "" {
		order.FilledAt, _ = time.Parse(time.RFC3339, filledAtStr)
	}
	return &order, nil
}

// GetOrders gets orders for a trader
func (s *OrderStore) GetOrders(traderID string, limit int) ([]TraderOrder, error) {
	query := `
	SELECT id, trader_id, exchange_id, exchange_type, exchange_order_id, client_order_id,
		symbol, side, position_side, type, time_in_force,
		quantity, price, stop_price, status, filled_quantity, avg_fill_price,
		commission, leverage, reduce_only, close_position, working_type, price_protect,
		order_action, position_id, created_at, updated_at, COALESCE(filled_at, '')
	FROM trader_orders
	WHERE trader_id = ?
	ORDER BY created_at DESC
	LIMIT ?
	`
	rows, err := db.Query(query, traderID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []TraderOrder
	for rows.Next() {
		var order TraderOrder
		var filledAtStr string
		err := rows.Scan(
			&order.ID, &order.TraderID, &order.ExchangeID, &order.ExchangeType, &order.ExchangeOrderID, &order.ClientOrderID,
			&order.Symbol, &order.Side, &order.PositionSide, &order.Type, &order.TimeInForce,
			&order.Quantity, &order.Price, &order.StopPrice, &order.Status, &order.FilledQuantity, &order.AvgFillPrice,
			&order.Commission, &order.Leverage, &order.ReduceOnly, &order.ClosePosition, &order.WorkingType, &order.PriceProtect,
			&order.OrderAction, &order.PositionID, &order.CreatedAt, &order.UpdatedAt, &filledAtStr,
		)
		if err != nil {
			return nil, err
		}
		if filledAtStr != "" {
			order.FilledAt, _ = time.Parse(time.RFC3339, filledAtStr)
		}
		orders = append(orders, order)
	}
	return orders, nil
}

// CreateFill creates a new fill with deduplication
func (s *OrderStore) CreateFill(fill *TraderFill) (int64, error) {
	// Check if exists first
	if fill.ExchangeID != "" && fill.ExchangeTradeID != "" {
		var existingID int64
		err := db.QueryRow(`
			SELECT id FROM trader_fills
			WHERE exchange_id = ? AND exchange_trade_id = ?
		`, fill.ExchangeID, fill.ExchangeTradeID).Scan(&existingID)
		if err == nil {
			return existingID, nil // Already exists
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
	}

	query := `
	INSERT INTO trader_fills (
		trader_id, order_id, exchange_id, exchange_trade_id,
		symbol, side, price, quantity, quote_quantity,
		commission, realized_pnl, is_maker, timestamp
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	result, err := db.Exec(query,
		fill.TraderID, fill.OrderID, fill.ExchangeID, fill.ExchangeTradeID,
		fill.Symbol, fill.Side, fill.Price, fill.Quantity, fill.QuoteQuantity,
		fill.Commission, fill.RealizedPnL, fill.IsMaker, fill.Timestamp,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetFills gets fills for a trader
func (s *OrderStore) GetFills(traderID string, limit int) ([]TraderFill, error) {
	query := `
	SELECT id, trader_id, order_id, exchange_id, exchange_trade_id,
		symbol, side, price, quantity, quote_quantity,
		commission, realized_pnl, is_maker, timestamp, created_at
	FROM trader_fills
	WHERE trader_id = ?
	ORDER BY timestamp DESC
	LIMIT ?
	`
	rows, err := db.Query(query, traderID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fills []TraderFill
	for rows.Next() {
		var fill TraderFill
		err := rows.Scan(
			&fill.ID, &fill.TraderID, &fill.OrderID, &fill.ExchangeID, &fill.ExchangeTradeID,
			&fill.Symbol, &fill.Side, &fill.Price, &fill.Quantity, &fill.QuoteQuantity,
			&fill.Commission, &fill.RealizedPnL, &fill.IsMaker, &fill.Timestamp, &fill.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		fills = append(fills, fill)
	}
	return fills, nil
}

// GetMaxTradeIDsByExchange returns max trade ID per symbol for incremental sync
func (s *OrderStore) GetMaxTradeIDsByExchange(traderID, exchangeID string) (map[string]string, error) {
	query := `
	SELECT symbol, MAX(exchange_trade_id)
	FROM trader_fills
	WHERE trader_id = ? AND exchange_id = ?
	GROUP BY symbol
	`
	rows, err := db.Query(query, traderID, exchangeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var symbol, maxID string
		if err := rows.Scan(&symbol, &maxID); err != nil {
			return nil, err
		}
		result[symbol] = maxID
	}
	return result, nil
}

// GetPendingOrders returns pending orders for a trader
func (s *OrderStore) GetPendingOrders(traderID string) ([]TraderOrder, error) {
	query := `
	SELECT id, trader_id, exchange_id, exchange_type, exchange_order_id, client_order_id,
		symbol, side, position_side, type, time_in_force,
		quantity, price, stop_price, status, filled_quantity, avg_fill_price,
		commission, leverage, reduce_only, close_position, working_type, price_protect,
		order_action, position_id, created_at, updated_at, COALESCE(filled_at, '')
	FROM trader_orders
	WHERE trader_id = ? AND status IN (?, ?)
	ORDER BY created_at DESC
	`
	rows, err := db.Query(query, traderID, OrderStatusNew, OrderStatusPartiallyFilled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []TraderOrder
	for rows.Next() {
		var order TraderOrder
		var filledAtStr string
		err := rows.Scan(
			&order.ID, &order.TraderID, &order.ExchangeID, &order.ExchangeType, &order.ExchangeOrderID, &order.ClientOrderID,
			&order.Symbol, &order.Side, &order.PositionSide, &order.Type, &order.TimeInForce,
			&order.Quantity, &order.Price, &order.StopPrice, &order.Status, &order.FilledQuantity, &order.AvgFillPrice,
			&order.Commission, &order.Leverage, &order.ReduceOnly, &order.ClosePosition, &order.WorkingType, &order.PriceProtect,
			&order.OrderAction, &order.PositionID, &order.CreatedAt, &order.UpdatedAt, &filledAtStr,
		)
		if err != nil {
			return nil, err
		}
		if filledAtStr != "" {
			order.FilledAt, _ = time.Parse(time.RFC3339, filledAtStr)
		}
		orders = append(orders, order)
	}
	return orders, nil
}
