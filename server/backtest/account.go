package backtest

import (
	"fmt"
	"math"
)

// Account manages simulated trading account
type Account struct {
	cash          float64
	positions     map[string]*Position
	realizedPnL   float64
	feeRate       float64 // Fee rate as decimal (e.g., 0.0004 for 4 bps)
	slippageRate  float64 // Slippage rate as decimal
}

// NewAccount creates a new simulated account
func NewAccount(initialBalance, feeBps, slippageBps float64) *Account {
	return &Account{
		cash:         initialBalance,
		positions:    make(map[string]*Position),
		feeRate:      feeBps / 10000,
		slippageRate: slippageBps / 10000,
	}
}

// GetCash returns available cash
func (a *Account) GetCash() float64 {
	return a.cash
}

// GetRealizedPnL returns total realized P&L
func (a *Account) GetRealizedPnL() float64 {
	return a.realizedPnL
}

// GetPositions returns all positions
func (a *Account) GetPositions() map[string]*Position {
	return a.positions
}

// GetPosition returns a specific position
func (a *Account) GetPosition(symbol, side string) *Position {
	key := positionKey(symbol, side)
	return a.positions[key]
}

// HasPosition checks if a position exists
func (a *Account) HasPosition(symbol, side string) bool {
	return a.GetPosition(symbol, side) != nil
}

// Open opens a new position or adds to existing
func (a *Account) Open(symbol, side string, quantity float64, leverage int, price float64, ts int64) (*Position, float64, float64, error) {
	if quantity <= 0 {
		return nil, 0, 0, fmt.Errorf("quantity must be positive: %f", quantity)
	}
	if leverage <= 0 {
		leverage = 1
	}

	// Apply slippage
	execPrice := a.applySlippage(price, side, true)

	// Calculate trade values
	notional := execPrice * quantity
	margin := notional / float64(leverage)
	fee := notional * a.feeRate

	// Check if we have enough cash
	required := margin + fee
	if required > a.cash {
		return nil, 0, 0, fmt.Errorf("insufficient cash: need %.2f, have %.2f", required, a.cash)
	}

	// Deduct from cash
	a.cash -= required

	// Calculate liquidation price
	liqPrice := a.computeLiquidationPrice(execPrice, leverage, side)

	// Create or update position
	key := positionKey(symbol, side)
	pos := a.positions[key]

	if pos == nil {
		// New position
		pos = &Position{
			Symbol:           symbol,
			Side:             side,
			Quantity:         quantity,
			EntryPrice:       execPrice,
			Leverage:         leverage,
			Margin:           margin,
			Notional:         notional,
			LiquidationPrice: liqPrice,
			OpenTime:         ts,
			AccumulatedFee:   fee,
		}
		a.positions[key] = pos
	} else {
		// Add to existing - calculate weighted average entry
		totalQty := pos.Quantity + quantity
		pos.EntryPrice = (pos.EntryPrice*pos.Quantity + execPrice*quantity) / totalQty
		pos.Quantity = totalQty
		pos.Margin += margin
		pos.Notional += notional
		pos.AccumulatedFee += fee
		pos.LiquidationPrice = a.computeLiquidationPrice(pos.EntryPrice, pos.Leverage, pos.Side)
	}

	return pos, fee, execPrice, nil
}

// Close closes all or part of a position
func (a *Account) Close(symbol, side string, quantity float64, price float64) (float64, float64, float64, error) {
	key := positionKey(symbol, side)
	pos := a.positions[key]

	if pos == nil {
		return 0, 0, 0, fmt.Errorf("no position to close: %s %s", symbol, side)
	}

	if quantity <= 0 || quantity > pos.Quantity {
		quantity = pos.Quantity // Close all
	}

	// Apply slippage
	execPrice := a.applySlippage(price, side, false)

	// Calculate realized P&L
	var realized float64
	if side == "long" {
		realized = (execPrice - pos.EntryPrice) * quantity
	} else {
		realized = (pos.EntryPrice - execPrice) * quantity
	}

	// Calculate fees
	closeNotional := execPrice * quantity
	closeFee := closeNotional * a.feeRate

	// Proportional opening fee
	ratio := quantity / pos.Quantity
	openFee := pos.AccumulatedFee * ratio
	totalFee := closeFee + openFee

	// Return margin to cash
	marginReturn := pos.Margin * ratio
	a.cash += marginReturn + realized - closeFee

	// Update realized P&L (realized minus total fees)
	netRealized := realized - totalFee
	a.realizedPnL += netRealized

	// Update or remove position
	if quantity >= pos.Quantity {
		delete(a.positions, key)
	} else {
		pos.Quantity -= quantity
		pos.Margin -= marginReturn
		pos.Notional -= closeNotional
		pos.AccumulatedFee -= openFee
	}

	return netRealized, totalFee, execPrice, nil
}

// TotalEquity calculates total equity given current prices
func (a *Account) TotalEquity(priceMap map[string]float64) (equity, unrealized float64, perSymbol map[string]float64) {
	perSymbol = make(map[string]float64)
	totalMargin := 0.0

	for key, pos := range a.positions {
		price, ok := priceMap[pos.Symbol]
		if !ok {
			price = pos.EntryPrice // Fallback to entry price
		}

		var pnl float64
		if pos.Side == "long" {
			pnl = (price - pos.EntryPrice) * pos.Quantity
		} else {
			pnl = (pos.EntryPrice - price) * pos.Quantity
		}

		unrealized += pnl
		perSymbol[key] = pnl
		totalMargin += pos.Margin
	}

	equity = a.cash + totalMargin + unrealized
	return equity, unrealized, perSymbol
}

// CheckLiquidation checks if any positions should be liquidated
func (a *Account) CheckLiquidation(priceMap map[string]float64, ts int64, cycle int) ([]TradeEvent, string, error) {
	var events []TradeEvent
	var notes []string

	for key, pos := range a.positions {
		price, ok := priceMap[pos.Symbol]
		if !ok {
			continue
		}

		liquidated := false
		if pos.Side == "long" && price <= pos.LiquidationPrice {
			liquidated = true
		} else if pos.Side == "short" && price >= pos.LiquidationPrice {
			liquidated = true
		}

		if liquidated {
			// Close at liquidation price
			realized, fee, execPrice, err := a.Close(pos.Symbol, pos.Side, pos.Quantity, pos.LiquidationPrice)
			if err != nil {
				return nil, "", err
			}

			event := TradeEvent{
				Timestamp:       ts,
				Symbol:          pos.Symbol,
				Action:          "liquidated",
				Side:            pos.Side,
				Quantity:        pos.Quantity,
				Price:           execPrice,
				Fee:             fee,
				RealizedPnL:     realized,
				Leverage:        pos.Leverage,
				Cycle:           cycle,
				LiquidationFlag: true,
				Note:            fmt.Sprintf("Liquidated at %.4f (liq price: %.4f)", price, pos.LiquidationPrice),
			}
			events = append(events, event)
			notes = append(notes, fmt.Sprintf("%s %s liquidated", pos.Symbol, pos.Side))

			delete(a.positions, key)
		}
	}

	if len(notes) > 0 {
		return events, fmt.Sprintf("Liquidations: %v", notes), nil
	}
	return nil, "", nil
}

// applySlippage applies slippage to execution price
func (a *Account) applySlippage(price float64, side string, isOpen bool) float64 {
	if a.slippageRate == 0 {
		return price
	}

	// Long: pay more on open, get less on close
	// Short: get more on open (sell high), pay more on close (buy back high)
	if side == "long" {
		if isOpen {
			return price * (1 + a.slippageRate)
		}
		return price * (1 - a.slippageRate)
	}
	// short
	if isOpen {
		return price * (1 - a.slippageRate)
	}
	return price * (1 + a.slippageRate)
}

// computeLiquidationPrice calculates the liquidation price
func (a *Account) computeLiquidationPrice(entry float64, leverage int, side string) float64 {
	// Liquidation when position loses ~100% of margin
	// margin = notional / leverage
	// loss% = 1 / leverage
	if side == "long" {
		return entry * (1 - 1.0/float64(leverage))
	}
	return entry * (1 + 1.0/float64(leverage))
}

// RestoreFromState restores account from a saved state
func (a *Account) RestoreFromState(state *State) {
	a.cash = state.Cash
	a.realizedPnL = state.RealizedPnL
	a.positions = make(map[string]*Position)
	for k, v := range state.Positions {
		posCopy := *v
		a.positions[k] = &posCopy
	}
}

// SaveToState saves account to state
func (a *Account) SaveToState(state *State) {
	state.Cash = a.cash
	state.RealizedPnL = a.realizedPnL
	state.Positions = make(map[string]*Position)
	for k, v := range a.positions {
		posCopy := *v
		state.Positions[k] = &posCopy
	}
}

// positionKey creates a unique key for a position
func positionKey(symbol, side string) string {
	return symbol + "_" + side
}

// CalculateMetrics calculates performance metrics from equity curve and trades
func CalculateMetrics(initialBalance float64, equityCurve []EquityPoint, trades []TradeEvent) *Metrics {
	metrics := &Metrics{
		SymbolStats: make(map[string]*SymbolStats),
	}

	if len(equityCurve) == 0 {
		return metrics
	}

	// Final equity and return
	finalEquity := equityCurve[len(equityCurve)-1].Equity
	metrics.FinalEquity = finalEquity
	metrics.TotalReturn = finalEquity - initialBalance
	metrics.TotalReturnPct = (metrics.TotalReturn / initialBalance) * 100

	// Max drawdown
	peak := equityCurve[0].Equity
	maxDD := 0.0
	for _, pt := range equityCurve {
		if pt.Equity > peak {
			peak = pt.Equity
		}
		dd := (peak - pt.Equity) / peak
		if dd > maxDD {
			maxDD = dd
		}
	}
	metrics.MaxDrawdown = peak * maxDD
	metrics.MaxDrawdownPct = maxDD * 100

	// Sharpe ratio (daily returns assumed)
	if len(equityCurve) > 1 {
		returns := make([]float64, len(equityCurve)-1)
		for i := 1; i < len(equityCurve); i++ {
			returns[i-1] = (equityCurve[i].Equity - equityCurve[i-1].Equity) / equityCurve[i-1].Equity
		}
		meanRet := mean(returns)
		stdRet := stdDev(returns)
		if stdRet > 0 {
			metrics.SharpeRatio = (meanRet / stdRet) * math.Sqrt(252) // Annualized
		}

		// Sortino ratio (downside deviation)
		negReturns := make([]float64, 0)
		for _, r := range returns {
			if r < 0 {
				negReturns = append(negReturns, r)
			}
		}
		if len(negReturns) > 0 {
			downsideDev := stdDev(negReturns)
			if downsideDev > 0 {
				metrics.SortinoRatio = (meanRet / downsideDev) * math.Sqrt(252)
			}
		}
	}

	// Trade statistics
	var wins, losses []float64
	symbolStats := make(map[string]*SymbolStats)

	for _, trade := range trades {
		if trade.Action == "liquidated" || trade.RealizedPnL == 0 {
			continue // Skip non-closing trades
		}

		metrics.TotalTrades++
		metrics.TotalFees += trade.Fee

		// Track per-symbol stats
		ss := symbolStats[trade.Symbol]
		if ss == nil {
			ss = &SymbolStats{Symbol: trade.Symbol}
			symbolStats[trade.Symbol] = ss
		}
		ss.TotalTrades++
		ss.TotalPnL += trade.RealizedPnL

		if trade.Side == "long" {
			ss.LongTrades++
		} else {
			ss.ShortTrades++
		}

		if trade.RealizedPnL > 0 {
			wins = append(wins, trade.RealizedPnL)
			metrics.WinningTrades++
			if trade.Side == "long" {
				ss.LongWinRate++
			} else {
				ss.ShortWinRate++
			}
		} else {
			losses = append(losses, trade.RealizedPnL)
			metrics.LosingTrades++
		}
	}

	// Win rate
	if metrics.TotalTrades > 0 {
		metrics.WinRate = float64(metrics.WinningTrades) / float64(metrics.TotalTrades) * 100
	}

	// Average win/loss and largest
	if len(wins) > 0 {
		metrics.AvgWin = sum(wins) / float64(len(wins))
		metrics.LargestWin = max(wins)
	}
	if len(losses) > 0 {
		metrics.AvgLoss = sum(losses) / float64(len(losses))
		metrics.LargestLoss = min(losses)
	}

	// Profit factor
	totalWins := sum(wins)
	totalLosses := math.Abs(sum(losses))
	if totalLosses > 0 {
		metrics.ProfitFactor = totalWins / totalLosses
	}

	// Finalize symbol stats
	for symbol, ss := range symbolStats {
		if ss.TotalTrades > 0 {
			ss.AvgPnL = ss.TotalPnL / float64(ss.TotalTrades)
		}
		if ss.LongTrades > 0 {
			ss.LongWinRate = (ss.LongWinRate / float64(ss.LongTrades)) * 100
		}
		if ss.ShortTrades > 0 {
			ss.ShortWinRate = (ss.ShortWinRate / float64(ss.ShortTrades)) * 100
		}
		ss.WinRate = float64(metrics.WinningTrades) / float64(ss.TotalTrades) * 100
		metrics.SymbolStats[symbol] = ss
	}

	return metrics
}

// Helper functions
func mean(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	return sum(data) / float64(len(data))
}

func sum(data []float64) float64 {
	total := 0.0
	for _, v := range data {
		total += v
	}
	return total
}

func stdDev(data []float64) float64 {
	if len(data) <= 1 {
		return 0
	}
	m := mean(data)
	variance := 0.0
	for _, v := range data {
		variance += (v - m) * (v - m)
	}
	return math.Sqrt(variance / float64(len(data)))
}

func max(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	m := data[0]
	for _, v := range data[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func min(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	m := data[0]
	for _, v := range data[1:] {
		if v < m {
			m = v
		}
	}
	return m
}
