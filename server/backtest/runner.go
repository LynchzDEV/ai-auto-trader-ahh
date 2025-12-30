package backtest

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"auto-trader/decision"
	"auto-trader/mcp"
)

// Runner executes a backtest simulation
type Runner struct {
	config      *Config
	account     *Account
	state       *State
	engine      *decision.Engine
	klines      map[string][]Kline // symbol -> klines
	metadata    *RunMetadata
	equityCurve []EquityPoint
	trades      []TradeEvent
	decisions   []DecisionLog
	mu          sync.RWMutex
	cancel      context.CancelFunc
}

// NewRunner creates a new backtest runner
func NewRunner(cfg *Config, client mcp.AIClient) *Runner {
	if err := cfg.Validate(); err != nil {
		log.Printf("Config validation warning: %v", err)
	}

	lang := decision.LangEnglish
	if cfg.Language == "zh-CN" {
		lang = decision.LangChinese
	}

	r := &Runner{
		config:  cfg,
		account: NewAccount(cfg.InitialBalance, cfg.FeeBps, cfg.SlippageBps),
		state:   NewState(cfg.InitialBalance),
		engine:  decision.NewEngine(client, lang),
		klines:  make(map[string][]Kline),
		metadata: &RunMetadata{
			RunID:       cfg.RunID,
			UserID:      cfg.UserID,
			Name:        cfg.Name,
			Description: cfg.Description,
			Status:      StatusPending,
			Config:      cfg,
		},
		equityCurve: make([]EquityPoint, 0),
		trades:      make([]TradeEvent, 0),
		decisions:   make([]DecisionLog, 0),
	}

	// Set validation config
	r.engine.SetValidationConfig(&decision.ValidationConfig{
		AccountEquity:     cfg.InitialBalance,
		BTCETHLeverage:    cfg.BTCETHLeverage,
		AltcoinLeverage:   cfg.AltcoinLeverage,
		BTCETHPosRatio:    cfg.BTCETHPosRatio,
		AltcoinPosRatio:   cfg.AltcoinPosRatio,
		MinPositionBTCETH: 60,
		MinPositionAlt:    12,
		MinRiskReward:     3.0,
	})

	return r
}

// LoadKlines loads historical klines for backtesting
func (r *Runner) LoadKlines(symbol string, klines []Kline) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.klines[symbol] = klines
}

// GetMetadata returns current run metadata
func (r *Runner) GetMetadata() *RunMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.metadata
}

// GetState returns current state
func (r *Runner) GetState() *State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state
}

// GetEquityCurve returns the equity curve
func (r *Runner) GetEquityCurve() []EquityPoint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.equityCurve
}

// GetTrades returns all trade events
func (r *Runner) GetTrades() []TradeEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.trades
}

// GetMetrics calculates and returns performance metrics
func (r *Runner) GetMetrics() *Metrics {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return CalculateMetrics(r.config.InitialBalance, r.equityCurve, r.trades)
}

// Start begins the backtest simulation
func (r *Runner) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	r.mu.Lock()
	r.metadata.Status = StatusRunning
	r.metadata.StartedAt = time.Now()
	r.mu.Unlock()

	// Run the simulation
	err := r.loop(ctx)

	r.mu.Lock()
	if err != nil {
		r.metadata.Status = StatusFailed
		r.metadata.Error = err.Error()
	} else if r.state.Liquidated {
		r.metadata.Status = StatusLiquidated
	} else {
		r.metadata.Status = StatusCompleted
	}
	r.metadata.CompletedAt = time.Now()
	r.mu.Unlock()

	return err
}

// Stop stops the running backtest
func (r *Runner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
}

// loop is the main simulation loop
func (r *Runner) loop(ctx context.Context) error {
	// Get decision timeframe klines
	if len(r.klines) == 0 {
		return fmt.Errorf("no klines loaded")
	}

	// Find the symbol with most klines for iteration
	var primarySymbol string
	var primaryKlines []Kline
	for symbol, klines := range r.klines {
		if len(klines) > len(primaryKlines) {
			primarySymbol = symbol
			primaryKlines = klines
		}
	}

	if len(primaryKlines) == 0 {
		return fmt.Errorf("no klines available for simulation")
	}

	// Filter klines to time range
	var filteredKlines []Kline
	for _, k := range primaryKlines {
		if k.CloseTime >= r.config.StartTS && k.OpenTime <= r.config.EndTS {
			filteredKlines = append(filteredKlines, k)
		}
	}

	totalBars := len(filteredKlines)
	r.mu.Lock()
	r.metadata.TotalBars = totalBars
	r.mu.Unlock()

	log.Printf("Starting backtest: %s, %d bars from %s to %s",
		primarySymbol, totalBars,
		time.Unix(r.config.StartTS/1000, 0).Format(time.RFC3339),
		time.Unix(r.config.EndTS/1000, 0).Format(time.RFC3339))

	// Main loop through bars
	for i, bar := range filteredKlines {
		select {
		case <-ctx.Done():
			log.Printf("Backtest cancelled at bar %d", i)
			return ctx.Err()
		default:
		}

		r.state.BarIndex = i
		r.state.BarTimestamp = bar.CloseTime

		// Build price map for all symbols
		priceMap := r.buildPriceMap(bar.CloseTime)

		// Check liquidation
		liqEvents, liqNote, err := r.account.CheckLiquidation(priceMap, bar.CloseTime, r.state.DecisionCycle)
		if err != nil {
			return fmt.Errorf("liquidation check failed: %w", err)
		}
		if len(liqEvents) > 0 {
			r.mu.Lock()
			r.trades = append(r.trades, liqEvents...)
			r.state.Liquidated = true
			r.state.LiquidationNote = liqNote
			r.mu.Unlock()
			log.Printf("Liquidation at bar %d: %s", i, liqNote)
			break
		}

		// Check if decision should trigger
		if (i+1)%r.config.DecisionCadenceNBars == 0 {
			r.state.DecisionCycle++

			// Make AI decision
			decisionCtx := r.buildDecisionContext(bar.CloseTime, priceMap)
			fullDecision, err := r.engine.MakeDecision(decisionCtx)

			decisionLog := DecisionLog{
				Timestamp:    bar.CloseTime,
				Cycle:        r.state.DecisionCycle,
				BarIndex:     i,
				SystemPrompt: fullDecision.SystemPrompt,
				UserPrompt:   fullDecision.UserPrompt,
				RawResponse:  fullDecision.RawResponse,
				CoTTrace:     fullDecision.CoTTrace,
				Decisions:    fullDecision.Decisions,
				DurationMs:   fullDecision.AIRequestDurationMs,
			}
			if err != nil {
				decisionLog.Error = err.Error()
				log.Printf("Decision error at cycle %d: %v", r.state.DecisionCycle, err)
			}

			r.mu.Lock()
			r.decisions = append(r.decisions, decisionLog)
			r.mu.Unlock()

			// Execute decisions
			if err == nil {
				r.executeDecisions(fullDecision.Decisions, bar.CloseTime, priceMap)
			}
		}

		// Update equity
		equity, unrealized, _ := r.account.TotalEquity(priceMap)
		r.state.Equity = equity
		r.state.UnrealizedPnL = unrealized

		if equity > r.state.MaxEquity {
			r.state.MaxEquity = equity
		}
		if equity < r.state.MinEquity || r.state.MinEquity == 0 {
			r.state.MinEquity = equity
		}
		if r.state.MaxEquity > 0 {
			dd := (r.state.MaxEquity - equity) / r.state.MaxEquity * 100
			if dd > r.state.MaxDrawdownPct {
				r.state.MaxDrawdownPct = dd
			}
		}

		// Record equity point
		eqPoint := EquityPoint{
			Timestamp:   bar.CloseTime,
			Equity:      equity,
			Available:   r.account.GetCash(),
			PnL:         equity - r.config.InitialBalance,
			PnLPct:      (equity - r.config.InitialBalance) / r.config.InitialBalance * 100,
			DrawdownPct: r.state.MaxDrawdownPct,
			Cycle:       r.state.DecisionCycle,
		}

		r.mu.Lock()
		r.equityCurve = append(r.equityCurve, eqPoint)
		r.metadata.CurrentBar = i
		r.metadata.Progress = float64(i+1) / float64(totalBars) * 100
		r.metadata.CurrentEquity = equity
		r.mu.Unlock()

		r.state.LastUpdate = time.Now()
		r.account.SaveToState(r.state)
	}

	log.Printf("Backtest completed: %d cycles, final equity: %.2f", r.state.DecisionCycle, r.state.Equity)
	return nil
}

// buildPriceMap builds a map of current prices for all symbols
func (r *Runner) buildPriceMap(ts int64) map[string]float64 {
	priceMap := make(map[string]float64)

	for symbol, klines := range r.klines {
		// Find the kline at or before timestamp
		for i := len(klines) - 1; i >= 0; i-- {
			if klines[i].CloseTime <= ts {
				priceMap[symbol] = klines[i].Close
				break
			}
		}
	}

	return priceMap
}

// buildDecisionContext builds the context for AI decision
func (r *Runner) buildDecisionContext(ts int64, priceMap map[string]float64) *decision.Context {
	equity, unrealized, _ := r.account.TotalEquity(priceMap)

	// Convert positions
	var positions []decision.PositionInfo
	for _, pos := range r.account.GetPositions() {
		price := priceMap[pos.Symbol]
		var pnlPct float64
		if pos.Side == "long" {
			pnlPct = (price - pos.EntryPrice) / pos.EntryPrice * 100
		} else {
			pnlPct = (pos.EntryPrice - price) / pos.EntryPrice * 100
		}

		positions = append(positions, decision.PositionInfo{
			Symbol:           pos.Symbol,
			Side:             pos.Side,
			EntryPrice:       pos.EntryPrice,
			MarkPrice:        price,
			Quantity:         pos.Quantity,
			Leverage:         pos.Leverage,
			UnrealizedPnL:    (price - pos.EntryPrice) * pos.Quantity,
			UnrealizedPnLPct: pnlPct,
			LiquidationPrice: pos.LiquidationPrice,
			MarginUsed:       pos.Margin,
			UpdateTime:       ts,
		})
	}

	// Build market data map
	marketDataMap := make(map[string]*decision.MarketData)
	for symbol := range r.klines {
		price := priceMap[symbol]
		marketDataMap[symbol] = &decision.MarketData{
			Symbol: symbol,
			Price:  price,
		}
	}

	// Calculate margin usage
	totalMargin := 0.0
	for _, pos := range r.account.GetPositions() {
		totalMargin += pos.Margin
	}
	marginUsedPct := 0.0
	if equity > 0 {
		marginUsedPct = totalMargin / equity * 100
	}

	return &decision.Context{
		CurrentTime:    time.Unix(ts/1000, 0).Format(time.RFC3339),
		RuntimeMinutes: int(time.Since(r.metadata.StartedAt).Minutes()),
		CallCount:      r.state.DecisionCycle,
		Account: decision.AccountInfo{
			TotalEquity:      equity,
			AvailableBalance: r.account.GetCash(),
			UnrealizedPnL:    unrealized,
			TotalPnL:         equity - r.config.InitialBalance,
			TotalPnLPct:      (equity - r.config.InitialBalance) / r.config.InitialBalance * 100,
			MarginUsed:       totalMargin,
			MarginUsedPct:    marginUsedPct,
			PositionCount:    len(positions),
		},
		Positions:       positions,
		MarketDataMap:   marketDataMap,
		BTCETHLeverage:  r.config.BTCETHLeverage,
		AltcoinLeverage: r.config.AltcoinLeverage,
		BTCETHPosRatio:  r.config.BTCETHPosRatio,
		AltcoinPosRatio: r.config.AltcoinPosRatio,
	}
}

// executeDecisions executes AI decisions
func (r *Runner) executeDecisions(decisions []decision.Decision, ts int64, priceMap map[string]float64) {
	// Sort: closes first, then opens
	closes := decision.FilterClosingDecisions(decisions)
	opens := decision.FilterOpeningDecisions(decisions)

	// Execute closes first
	for _, dec := range closes {
		r.executeDecision(dec, ts, priceMap)
	}

	// Then opens
	for _, dec := range opens {
		r.executeDecision(dec, ts, priceMap)
	}
}

// executeDecision executes a single decision
func (r *Runner) executeDecision(dec decision.Decision, ts int64, priceMap map[string]float64) {
	price, ok := priceMap[dec.Symbol]
	if !ok {
		log.Printf("No price for symbol %s, skipping decision", dec.Symbol)
		return
	}

	var event TradeEvent
	event.Timestamp = ts
	event.Symbol = dec.Symbol
	event.Action = dec.Action
	event.Cycle = r.state.DecisionCycle

	switch dec.Action {
	case decision.ActionOpenLong:
		leverage := dec.Leverage
		if leverage <= 0 {
			leverage = r.config.AltcoinLeverage
			if isBTCOrETH(dec.Symbol) {
				leverage = r.config.BTCETHLeverage
			}
		}

		quantity := dec.PositionSizeUSD / price
		pos, fee, execPrice, err := r.account.Open(dec.Symbol, "long", quantity, leverage, price, ts)
		if err != nil {
			log.Printf("Failed to open long %s: %v", dec.Symbol, err)
			return
		}

		event.Side = "long"
		event.Quantity = quantity
		event.Price = execPrice
		event.Fee = fee
		event.Leverage = leverage
		event.OrderValue = dec.PositionSizeUSD
		event.PositionAfter = pos.Quantity
		event.Note = dec.Reasoning

	case decision.ActionOpenShort:
		leverage := dec.Leverage
		if leverage <= 0 {
			leverage = r.config.AltcoinLeverage
			if isBTCOrETH(dec.Symbol) {
				leverage = r.config.BTCETHLeverage
			}
		}

		quantity := dec.PositionSizeUSD / price
		pos, fee, execPrice, err := r.account.Open(dec.Symbol, "short", quantity, leverage, price, ts)
		if err != nil {
			log.Printf("Failed to open short %s: %v", dec.Symbol, err)
			return
		}

		event.Side = "short"
		event.Quantity = quantity
		event.Price = execPrice
		event.Fee = fee
		event.Leverage = leverage
		event.OrderValue = dec.PositionSizeUSD
		event.PositionAfter = pos.Quantity
		event.Note = dec.Reasoning

	case decision.ActionCloseLong:
		pos := r.account.GetPosition(dec.Symbol, "long")
		if pos == nil {
			log.Printf("No long position to close for %s", dec.Symbol)
			return
		}

		realized, fee, execPrice, err := r.account.Close(dec.Symbol, "long", pos.Quantity, price)
		if err != nil {
			log.Printf("Failed to close long %s: %v", dec.Symbol, err)
			return
		}

		event.Side = "long"
		event.Quantity = pos.Quantity
		event.Price = execPrice
		event.Fee = fee
		event.RealizedPnL = realized
		event.Leverage = pos.Leverage
		event.Note = dec.Reasoning

	case decision.ActionCloseShort:
		pos := r.account.GetPosition(dec.Symbol, "short")
		if pos == nil {
			log.Printf("No short position to close for %s", dec.Symbol)
			return
		}

		realized, fee, execPrice, err := r.account.Close(dec.Symbol, "short", pos.Quantity, price)
		if err != nil {
			log.Printf("Failed to close short %s: %v", dec.Symbol, err)
			return
		}

		event.Side = "short"
		event.Quantity = pos.Quantity
		event.Price = execPrice
		event.Fee = fee
		event.RealizedPnL = realized
		event.Leverage = pos.Leverage
		event.Note = dec.Reasoning

	default:
		// hold or wait - no action
		return
	}

	r.mu.Lock()
	r.trades = append(r.trades, event)
	r.mu.Unlock()

	log.Printf("[%s] %s %s %.4f @ %.4f (fee: %.4f, PnL: %.4f)",
		time.Unix(ts/1000, 0).Format("2006-01-02 15:04"),
		dec.Action, dec.Symbol, event.Quantity, event.Price, event.Fee, event.RealizedPnL)
}

// isBTCOrETH checks if symbol is BTC or ETH
func isBTCOrETH(symbol string) bool {
	return symbol == "BTCUSDT" || symbol == "ETHUSDT"
}
