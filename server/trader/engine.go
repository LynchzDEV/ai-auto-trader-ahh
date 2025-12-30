package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"auto-trader-ahh/ai"
	"auto-trader-ahh/config"
	"auto-trader-ahh/exchange"
	"auto-trader-ahh/market"
	"auto-trader-ahh/store"
)

type Engine struct {
	id           string
	name         string
	cfg          *config.Config
	strategy     *store.Strategy
	aiClient     *ai.Client
	binance      *exchange.BinanceClient
	dataProvider *market.DataProvider

	running bool
	stopCh  chan struct{}
	mu      sync.RWMutex

	// State
	lastDecisions map[string]*ai.TradingDecision
	positions     map[string]*exchange.Position
	account       *exchange.AccountInfo

	// Stores
	decisionStore *store.DecisionStore
	equityStore   *store.EquityStore
}

type TradeLog struct {
	Timestamp  time.Time
	Symbol     string
	Action     string
	Decision   *ai.TradingDecision
	RawAI      string
	MarketData string
	Error      string
}

// NewEngine creates a new trading engine with strategy support
func NewEngine(id, name string, aiClient *ai.Client, binance *exchange.BinanceClient, strategy *store.Strategy, cfg *config.Config) *Engine {
	dataProvider := market.NewDataProvider(binance)

	return &Engine{
		id:            id,
		name:          name,
		cfg:           cfg,
		strategy:      strategy,
		aiClient:      aiClient,
		binance:       binance,
		dataProvider:  dataProvider,
		stopCh:        make(chan struct{}),
		lastDecisions: make(map[string]*ai.TradingDecision),
		positions:     make(map[string]*exchange.Position),
		decisionStore: store.NewDecisionStore(),
		equityStore:   store.NewEquityStore(),
	}
}

func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("engine already running")
	}
	e.running = true
	e.stopCh = make(chan struct{})
	e.mu.Unlock()

	log.Printf("[%s] Starting trading engine...", e.name)

	// Verify Binance connection
	account, err := e.binance.GetAccountInfo(ctx)
	if err != nil {
		e.running = false
		return fmt.Errorf("failed to connect to Binance: %w", err)
	}
	e.account = account
	log.Printf("[%s] Connected to Binance. Balance: $%.2f", e.name, account.TotalWalletBalance)

	// Set leverage for all pairs from strategy
	coins := e.getTradingPairs()
	leverage := e.strategy.Config.RiskControl.MaxLeverage
	for _, pair := range coins {
		if err := e.binance.SetLeverage(ctx, pair, leverage); err != nil {
			log.Printf("[%s] Warning: failed to set leverage for %s: %v", e.name, pair, err)
		}
	}

	// Start trading loop
	go e.tradingLoop(ctx)

	return nil
}

func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return
	}

	log.Printf("[%s] Stopping trading engine...", e.name)
	close(e.stopCh)
	e.running = false
}

func (e *Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

func (e *Engine) getTradingPairs() []string {
	if e.strategy != nil && len(e.strategy.Config.CoinSource.StaticCoins) > 0 {
		return e.strategy.Config.CoinSource.StaticCoins
	}
	return e.cfg.TradingPairs
}

func (e *Engine) getTradingInterval() time.Duration {
	if e.strategy != nil && e.strategy.Config.TradingInterval > 0 {
		return time.Duration(e.strategy.Config.TradingInterval) * time.Minute
	}
	return time.Duration(e.cfg.TradingInterval) * time.Minute
}

func (e *Engine) getMinConfidence() int {
	if e.strategy != nil {
		return e.strategy.Config.RiskControl.MinConfidence
	}
	return 70
}

func (e *Engine) tradingLoop(ctx context.Context) {
	interval := e.getTradingInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("[%s] Trading loop started (interval: %v)", e.name, interval)

	// Run immediately on start
	e.runTradingCycle(ctx)

	for {
		select {
		case <-e.stopCh:
			log.Printf("[%s] Trading loop stopped", e.name)
			return
		case <-ctx.Done():
			log.Printf("[%s] Context cancelled, stopping trading loop", e.name)
			return
		case <-ticker.C:
			e.runTradingCycle(ctx)
		}
	}
}

func (e *Engine) runTradingCycle(ctx context.Context) {
	log.Printf("[%s] === Starting trading cycle ===", e.name)

	// Update account info
	account, err := e.binance.GetAccountInfo(ctx)
	if err != nil {
		log.Printf("[%s] Error getting account info: %v", e.name, err)
	} else {
		e.mu.Lock()
		e.account = account
		e.mu.Unlock()

		// Save equity snapshot
		e.equityStore.Create(&store.EquitySnapshot{
			TraderID:      e.id,
			Equity:        account.TotalMarginBalance,
			Balance:       account.TotalWalletBalance,
			UnrealizedPnL: account.TotalUnrealizedProfit,
		})
	}

	// Update positions
	positions, err := e.binance.GetPositions(ctx)
	if err != nil {
		log.Printf("[%s] Error getting positions: %v", e.name, err)
	} else {
		e.mu.Lock()
		e.positions = make(map[string]*exchange.Position)
		for i := range positions {
			e.positions[positions[i].Symbol] = &positions[i]
		}
		e.mu.Unlock()
	}

	// Process each trading pair
	allDecisions := make([]map[string]interface{}, 0)
	for _, symbol := range e.getTradingPairs() {
		log.Printf("[%s] Analyzing %s...", e.name, symbol)

		tradeLog := e.analyzeAndTrade(ctx, symbol)

		decisionData := map[string]interface{}{
			"symbol": symbol,
			"action": "NONE",
		}

		if tradeLog.Error != "" {
			log.Printf("[%s][%s] Error: %s", e.name, symbol, tradeLog.Error)
			decisionData["error"] = tradeLog.Error
		} else if tradeLog.Decision != nil {
			log.Printf("[%s][%s] Decision: %s (Confidence: %.0f%%)",
				e.name, symbol, tradeLog.Decision.Action, tradeLog.Decision.Confidence)
			log.Printf("[%s][%s] Reasoning: %s", e.name, symbol, tradeLog.Decision.Reasoning)

			decisionData["action"] = tradeLog.Decision.Action
			decisionData["confidence"] = tradeLog.Decision.Confidence
			decisionData["reasoning"] = tradeLog.Decision.Reasoning
		}

		allDecisions = append(allDecisions, decisionData)

		// Small delay between pairs to avoid rate limits
		time.Sleep(2 * time.Second)
	}

	// Save decision record
	decisionsJSON, _ := json.Marshal(allDecisions)
	e.decisionStore.Create(&store.Decision{
		TraderID:  e.id,
		Decisions: string(decisionsJSON),
		Executed:  true,
	})

	log.Printf("[%s] === Trading cycle complete ===", e.name)
}

func (e *Engine) analyzeAndTrade(ctx context.Context, symbol string) *TradeLog {
	tradeLog := &TradeLog{
		Timestamp: time.Now(),
		Symbol:    symbol,
	}

	// Get market data with strategy config
	timeframe := "5m"
	klineCount := 100
	if e.strategy != nil {
		timeframe = e.strategy.Config.Indicators.PrimaryTimeframe
		klineCount = e.strategy.Config.Indicators.KlineCount
	}

	marketData, err := e.dataProvider.GetMarketDataWithConfig(ctx, symbol, timeframe, klineCount)
	if err != nil {
		tradeLog.Error = fmt.Sprintf("failed to get market data: %v", err)
		return tradeLog
	}

	// Format data for AI
	formattedData := e.dataProvider.FormatForAI(marketData)
	tradeLog.MarketData = formattedData

	// Add account info
	e.mu.RLock()
	if e.account != nil {
		formattedData += fmt.Sprintf("\n--- Account Info ---\n")
		formattedData += fmt.Sprintf("Total Equity: $%.2f\n", e.account.TotalMarginBalance)
		formattedData += fmt.Sprintf("Available Balance: $%.2f\n", e.account.AvailableBalance)
		formattedData += fmt.Sprintf("Unrealized PnL: $%.2f\n", e.account.TotalUnrealizedProfit)
	}

	// Add position info if exists
	pos, hasPosition := e.positions[symbol]
	e.mu.RUnlock()

	if hasPosition {
		formattedData += fmt.Sprintf("\n--- Current Position ---\n")
		formattedData += fmt.Sprintf("Side: %s\n", map[bool]string{true: "LONG", false: "SHORT"}[pos.PositionAmt > 0])
		formattedData += fmt.Sprintf("Size: %.4f\n", pos.PositionAmt)
		formattedData += fmt.Sprintf("Entry Price: $%.2f\n", pos.EntryPrice)
		formattedData += fmt.Sprintf("Mark Price: $%.2f\n", pos.MarkPrice)
		formattedData += fmt.Sprintf("Unrealized PnL: $%.2f\n", pos.UnrealizedProfit)
	} else {
		formattedData += "\n--- No Current Position ---\n"
	}

	// Add strategy rules
	if e.strategy != nil && e.strategy.Config.CustomPrompt != "" {
		formattedData += fmt.Sprintf("\n--- Strategy Rules ---\n%s\n", e.strategy.Config.CustomPrompt)
	}

	// Get AI decision
	decision, rawResponse, err := e.aiClient.GetTradingDecision(formattedData)
	tradeLog.RawAI = rawResponse

	if err != nil {
		tradeLog.Error = fmt.Sprintf("AI decision failed: %v", err)
		return tradeLog
	}

	tradeLog.Decision = decision
	tradeLog.Action = decision.Action

	// Store last decision
	e.mu.Lock()
	e.lastDecisions[symbol] = decision
	e.mu.Unlock()

	// Execute trade if confidence is high enough
	minConfidence := float64(e.getMinConfidence())
	if decision.Confidence >= minConfidence {
		if err := e.executeTrade(ctx, symbol, decision, hasPosition, pos); err != nil {
			tradeLog.Error = fmt.Sprintf("trade execution failed: %v", err)
		}
	} else {
		log.Printf("[%s][%s] Confidence too low (%.0f%% < %.0f%%), skipping trade",
			e.name, symbol, decision.Confidence, minConfidence)
	}

	return tradeLog
}

func (e *Engine) executeTrade(ctx context.Context, symbol string, decision *ai.TradingDecision, hasPosition bool, currentPos *exchange.Position) error {
	// Get account info for position sizing
	account, err := e.binance.GetAccountInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get account info: %w", err)
	}

	// Get current price
	ticker, err := e.binance.GetTicker(ctx, symbol)
	if err != nil {
		return fmt.Errorf("failed to get price: %w", err)
	}

	// Calculate position size from strategy config
	maxPositionPct := e.cfg.MaxPositionPct
	leverage := e.cfg.Leverage
	if e.strategy != nil {
		maxPositionPct = e.strategy.Config.RiskControl.MaxPositionPercent
		leverage = e.strategy.Config.RiskControl.MaxLeverage
	}

	positionValue := account.AvailableBalance * (maxPositionPct / 100) * float64(leverage)
	quantity := positionValue / ticker.Price

	// Check max positions
	if e.strategy != nil {
		maxPositions := e.strategy.Config.RiskControl.MaxPositions
		e.mu.RLock()
		currentPositions := len(e.positions)
		e.mu.RUnlock()

		if !hasPosition && currentPositions >= maxPositions && decision.Action != "CLOSE" && decision.Action != "HOLD" {
			log.Printf("[%s][%s] Max positions (%d) reached, skipping new position", e.name, symbol, maxPositions)
			return nil
		}
	}

	switch decision.Action {
	case "BUY":
		if hasPosition && currentPos.PositionAmt > 0 {
			log.Printf("[%s][%s] Already in LONG position, skipping BUY", e.name, symbol)
			return nil
		}
		if hasPosition && currentPos.PositionAmt < 0 {
			log.Printf("[%s][%s] Closing SHORT position before opening LONG", e.name, symbol)
			if _, err := e.binance.ClosePosition(ctx, symbol, currentPos.PositionAmt); err != nil {
				return fmt.Errorf("failed to close short: %w", err)
			}
		}
		log.Printf("[%s][%s] Opening LONG: %.4f @ $%.2f", e.name, symbol, quantity, ticker.Price)
		if _, err := e.binance.PlaceOrder(ctx, symbol, "BUY", "MARKET", quantity, 0); err != nil {
			return fmt.Errorf("failed to open long: %w", err)
		}

	case "SELL":
		if hasPosition && currentPos.PositionAmt < 0 {
			log.Printf("[%s][%s] Already in SHORT position, skipping SELL", e.name, symbol)
			return nil
		}
		if hasPosition && currentPos.PositionAmt > 0 {
			log.Printf("[%s][%s] Closing LONG position before opening SHORT", e.name, symbol)
			if _, err := e.binance.ClosePosition(ctx, symbol, currentPos.PositionAmt); err != nil {
				return fmt.Errorf("failed to close long: %w", err)
			}
		}
		log.Printf("[%s][%s] Opening SHORT: %.4f @ $%.2f", e.name, symbol, quantity, ticker.Price)
		if _, err := e.binance.PlaceOrder(ctx, symbol, "SELL", "MARKET", quantity, 0); err != nil {
			return fmt.Errorf("failed to open short: %w", err)
		}

	case "CLOSE":
		if !hasPosition {
			log.Printf("[%s][%s] No position to close", e.name, symbol)
			return nil
		}
		log.Printf("[%s][%s] Closing position: %.4f", e.name, symbol, currentPos.PositionAmt)
		if _, err := e.binance.ClosePosition(ctx, symbol, currentPos.PositionAmt); err != nil {
			return fmt.Errorf("failed to close position: %w", err)
		}

	case "HOLD":
		log.Printf("[%s][%s] Holding - no action taken", e.name, symbol)

	default:
		log.Printf("[%s][%s] Unknown action: %s", e.name, symbol, decision.Action)
	}

	return nil
}

// GetStatus returns current engine status
func (e *Engine) GetStatus() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	positions := make([]map[string]interface{}, 0)
	for _, pos := range e.positions {
		positions = append(positions, map[string]interface{}{
			"symbol":    pos.Symbol,
			"amount":    pos.PositionAmt,
			"entry":     pos.EntryPrice,
			"markPrice": pos.MarkPrice,
			"pnl":       pos.UnrealizedProfit,
			"leverage":  pos.Leverage,
		})
	}

	decisions := make(map[string]interface{})
	for symbol, dec := range e.lastDecisions {
		decisions[symbol] = map[string]interface{}{
			"action":     dec.Action,
			"confidence": dec.Confidence,
			"reasoning":  dec.Reasoning,
		}
	}

	strategyName := "Default"
	if e.strategy != nil {
		strategyName = e.strategy.Name
	}

	return map[string]interface{}{
		"trader_id":    e.id,
		"trader_name":  e.name,
		"running":      e.running,
		"strategy":     strategyName,
		"pairs":        e.getTradingPairs(),
		"positions":    positions,
		"decisions":    decisions,
	}
}

// GetAccount returns account information
func (e *Engine) GetAccount() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.account == nil {
		return map[string]interface{}{"error": "No account data"}
	}

	return map[string]interface{}{
		"total_equity":     e.account.TotalMarginBalance,
		"wallet_balance":   e.account.TotalWalletBalance,
		"available":        e.account.AvailableBalance,
		"unrealized_pnl":   e.account.TotalUnrealizedProfit,
	}
}

// GetPositions returns current positions
func (e *Engine) GetPositions() []map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	positions := make([]map[string]interface{}, 0)
	for _, pos := range e.positions {
		pnlPct := 0.0
		if pos.EntryPrice > 0 {
			if pos.PositionAmt > 0 {
				pnlPct = ((pos.MarkPrice - pos.EntryPrice) / pos.EntryPrice) * 100
			} else {
				pnlPct = ((pos.EntryPrice - pos.MarkPrice) / pos.EntryPrice) * 100
			}
		}

		positions = append(positions, map[string]interface{}{
			"symbol":      pos.Symbol,
			"side":        map[bool]string{true: "LONG", false: "SHORT"}[pos.PositionAmt > 0],
			"amount":      pos.PositionAmt,
			"entry_price": pos.EntryPrice,
			"mark_price":  pos.MarkPrice,
			"pnl":         pos.UnrealizedProfit,
			"pnl_percent": pnlPct,
			"leverage":    pos.Leverage,
		})
	}

	return positions
}
