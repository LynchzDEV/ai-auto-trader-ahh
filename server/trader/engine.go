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
	"auto-trader-ahh/decision"
	"auto-trader-ahh/exchange"
	"auto-trader-ahh/market"
	"auto-trader-ahh/mcp"
	"auto-trader-ahh/store"
)

type Engine struct {
	id           string
	name         string
	cfg          *config.Config
	strategy     *store.Strategy
	aiClient     *ai.Client        // Legacy AI client (for backward compatibility)
	binance      *exchange.BinanceClient
	dataProvider *market.DataProvider

	// Decision Engine (NOFX-style XML parsing with CoT)
	mcpClient      mcp.AIClient
	decisionEngine *decision.Engine
	callCount      int       // Number of AI calls made
	startTime      time.Time // Engine start time

	running bool
	stopCh  chan struct{}
	mu      sync.RWMutex

	// State
	lastDecisions       map[string]*ai.TradingDecision
	lastFullDecision    *decision.FullDecision // Latest full decision with CoT
	positions           map[string]*exchange.Position
	account             *exchange.AccountInfo

	// Stores
	decisionStore *store.DecisionStore
	equityStore   *store.EquityStore

	// Position Management - Peak P&L tracking
	peakPnLCache      map[string]float64 // key: "symbol_side" -> peak P&L %
	peakPnLCacheMutex sync.RWMutex

	// Position Management - Hold duration tracking
	positionFirstSeenTime map[string]int64 // key: "symbol_side" -> timestamp in ms

	// Daily Loss Tracking
	dailyPnL       float64   // Today's realized + unrealized P&L
	lastResetTime  time.Time // When daily P&L was last reset
	stopUntil      time.Time // Don't trade until this time (after daily loss trigger)
	initialBalance float64   // Balance at start of day for daily loss calculation

	// Order sync
	orderSyncStop chan struct{}
}

type TradeLog struct {
	Timestamp  time.Time
	Symbol     string
	Action     string
	Decision   *ai.TradingDecision
	RawAI      string
	MarketData string
	Error      string
	CoTTrace   string // Chain of thought from AI reasoning
}

// NewEngine creates a new trading engine with strategy support
func NewEngine(id, name string, aiClient *ai.Client, binance *exchange.BinanceClient, strategy *store.Strategy, cfg *config.Config) *Engine {
	dataProvider := market.NewDataProvider(binance)

	// Create MCP client from config (uses OpenRouter by default)
	mcpClient := mcp.NewOpenRouterClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel)

	// Create decision engine with English language
	decisionEngine := decision.NewEngine(mcpClient, decision.LangEnglish)

	// Configure validation from strategy if available
	if strategy != nil {
		validationCfg := &decision.ValidationConfig{
			AccountEquity:     10000, // Will be updated at runtime
			BTCETHLeverage:    strategy.Config.RiskControl.BTCETHMaxLeverage,
			AltcoinLeverage:   strategy.Config.RiskControl.AltcoinMaxLeverage,
			BTCETHPosRatio:    strategy.Config.RiskControl.BTCETHMaxPositionValueRatio,
			AltcoinPosRatio:   strategy.Config.RiskControl.AltcoinMaxPositionValueRatio,
			MinPositionBTCETH: strategy.Config.RiskControl.MinPositionSizeBTCETH,
			MinPositionAlt:    strategy.Config.RiskControl.MinPositionSize,
			MinRiskReward:     strategy.Config.RiskControl.MinRiskRewardRatio,
		}
		decisionEngine.SetValidationConfig(validationCfg)
	}

	return &Engine{
		id:             id,
		name:           name,
		cfg:            cfg,
		strategy:       strategy,
		aiClient:       aiClient,
		binance:        binance,
		dataProvider:   dataProvider,
		mcpClient:      mcpClient,
		decisionEngine: decisionEngine,
		startTime:      time.Now(),
		stopCh:         make(chan struct{}),
		lastDecisions:  make(map[string]*ai.TradingDecision),
		positions:      make(map[string]*exchange.Position),
		decisionStore:  store.NewDecisionStore(),
		equityStore:    store.NewEquityStore(),

		// Initialize position management maps
		peakPnLCache:          make(map[string]float64),
		positionFirstSeenTime: make(map[string]int64),

		// Initialize daily tracking
		lastResetTime:  time.Now(),
		initialBalance: 0,
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
	e.orderSyncStop = make(chan struct{})
	e.mu.Unlock()

	log.Printf("[%s] Starting trading engine...", e.name)

	// Verify Binance connection
	account, err := e.binance.GetAccountInfo(ctx)
	if err != nil {
		e.running = false
		return fmt.Errorf("failed to connect to Binance: %w", err)
	}
	e.account = account
	e.initialBalance = account.TotalWalletBalance // Set initial balance for daily loss tracking
	e.lastResetTime = time.Now()
	log.Printf("[%s] Connected to Binance. Balance: $%.2f", e.name, account.TotalWalletBalance)

	// Set leverage for all pairs (separate limits for BTC/ETH vs altcoins)
	coins := e.getTradingPairs()
	for _, pair := range coins {
		leverage := e.getLeverageLimit(pair)
		if err := e.binance.SetLeverage(ctx, pair, leverage); err != nil {
			log.Printf("[%s] Warning: failed to set leverage for %s: %v", e.name, pair, err)
		} else {
			log.Printf("[%s] Set leverage for %s to %dx", e.name, pair, leverage)
		}
	}

	// Start background goroutines
	go e.tradingLoop(ctx)
	go e.startDrawdownMonitor(ctx)
	go e.startOrderSync(ctx)

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
	if e.orderSyncStop != nil {
		close(e.orderSyncStop)
	}
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

	// Reset daily P&L if new day
	e.resetDailyPnLIfNeeded()

	// Check if trading is paused due to daily loss
	if e.shouldStopTrading() {
		e.mu.RLock()
		stopUntil := e.stopUntil
		e.mu.RUnlock()
		log.Printf("[%s] Trading paused until %s, skipping cycle", e.name, stopUntil.Format(time.RFC3339))
		return
	}

	// Update account info
	account, err := e.binance.GetAccountInfo(ctx)
	if err != nil {
		log.Printf("[%s] Error getting account info: %v", e.name, err)
	} else {
		e.mu.Lock()
		e.account = account
		e.mu.Unlock()

		// Save equity snapshot
		e.equityStore.Save(&store.EquitySnapshot{
			TraderID:      e.id,
			TotalEquity:   account.TotalMarginBalance,
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

	// Check if daily loss limit has been exceeded
	if e.checkDailyLoss() {
		e.triggerTradingPause()
	}

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

	// For open actions, apply all risk controls
	isOpenAction := decision.Action == "BUY" || decision.Action == "SELL" ||
		decision.Action == "open_long" || decision.Action == "open_short"

	if isOpenAction && !hasPosition {
		// 1. Check max positions
		if err := e.enforceMaxPositions(); err != nil {
			log.Printf("[%s][%s] %v, skipping new position", e.name, symbol, err)
			return fmt.Errorf("skipped: %w", err)
		}

		// 2. Validate risk-reward ratio if SL/TP provided
		if decision.StopLoss > 0 && decision.TakeProfit > 0 {
			if err := e.validateRiskRewardRatio(decision.Action, decision.StopLoss, decision.TakeProfit, ticker.Price); err != nil {
				log.Printf("[%s][%s] %v, skipping trade", e.name, symbol, err)
				return fmt.Errorf("skipped: %w", err)
			}
		}
	}

	// Calculate position size using equity and leverage
	equity := account.TotalMarginBalance
	if equity <= 0 {
		equity = account.AvailableBalance
	}

	leverage := e.getLeverageLimit(symbol)

	// Get position percentage from strategy (fallback to legacy field, then config, then default 10%)
	positionPct := e.getPositionPercent()

	// Log balance info for debugging
	log.Printf("[%s][%s] Balance: equity=$%.2f, available=$%.2f, leverage=%dx, positionPct=%.1f%%",
		e.name, symbol, equity, account.AvailableBalance, leverage, positionPct)

	positionSizeUSD := account.AvailableBalance * (positionPct / 100) * float64(leverage)
	log.Printf("[%s][%s] Initial position size: $%.2f (%.1f%% of $%.2f * %dx)",
		e.name, symbol, positionSizeUSD, positionPct, account.AvailableBalance, leverage)

	if isOpenAction && !hasPosition {
		// 3. Enforce position value ratio (cap by equity * ratio)
		var wasCapped bool
		positionSizeUSD, wasCapped = e.enforcePositionValueRatio(positionSizeUSD, equity, symbol)
		if wasCapped {
			log.Printf("[%s][%s] Position capped to $%.2f by value ratio", e.name, symbol, positionSizeUSD)
		}

		// 4. Apply margin buffer (use 98% of calculated size)
		positionSizeUSD = e.applyMarginBuffer(positionSizeUSD)
		log.Printf("[%s][%s] After margin buffer: $%.2f", e.name, symbol, positionSizeUSD)

		// 5. Enforce minimum position size
		if err := e.enforceMinPositionSize(positionSizeUSD, symbol); err != nil {
			log.Printf("[%s][%s] %v, skipping trade", e.name, symbol, err)
			return fmt.Errorf("skipped: %w", err)
		}
	}

	quantity := positionSizeUSD / ticker.Price

	switch decision.Action {
	case "BUY", "open_long":
		if hasPosition && currentPos.PositionAmt > 0 {
			log.Printf("[%s][%s] Already in LONG position, skipping BUY", e.name, symbol)
			return fmt.Errorf("skipped: already in LONG position")
		}
		if hasPosition && currentPos.PositionAmt < 0 {
			log.Printf("[%s][%s] Closing SHORT position before opening LONG", e.name, symbol)
			if _, err := e.binance.ClosePosition(ctx, symbol, currentPos.PositionAmt); err != nil {
				return fmt.Errorf("failed to close short: %w", err)
			}
			e.clearPositionTracking(symbol, "SHORT")
		}
		log.Printf("[%s][%s] Opening LONG: %.4f @ $%.2f (size: $%.2f, leverage: %dx)",
			e.name, symbol, quantity, ticker.Price, positionSizeUSD, leverage)
		if _, err := e.binance.PlaceOrder(ctx, symbol, "BUY", "MARKET", quantity, 0); err != nil {
			return fmt.Errorf("failed to open long: %w", err)
		}
		e.setPositionFirstSeen(symbol, "LONG")

	case "SELL", "open_short":
		if hasPosition && currentPos.PositionAmt < 0 {
			log.Printf("[%s][%s] Already in SHORT position, skipping SELL", e.name, symbol)
			return fmt.Errorf("skipped: already in SHORT position")
		}
		if hasPosition && currentPos.PositionAmt > 0 {
			log.Printf("[%s][%s] Closing LONG position before opening SHORT", e.name, symbol)
			if _, err := e.binance.ClosePosition(ctx, symbol, currentPos.PositionAmt); err != nil {
				return fmt.Errorf("failed to close long: %w", err)
			}
			e.clearPositionTracking(symbol, "LONG")
		}
		log.Printf("[%s][%s] Opening SHORT: %.4f @ $%.2f (size: $%.2f, leverage: %dx)",
			e.name, symbol, quantity, ticker.Price, positionSizeUSD, leverage)
		if _, err := e.binance.PlaceOrder(ctx, symbol, "SELL", "MARKET", quantity, 0); err != nil {
			return fmt.Errorf("failed to open short: %w", err)
		}
		e.setPositionFirstSeen(symbol, "SHORT")

	case "CLOSE", "close_long", "close_short":
		if !hasPosition {
			log.Printf("[%s][%s] No position to close", e.name, symbol)
			return fmt.Errorf("skipped: no position to close")
		}
		side := "LONG"
		if currentPos.PositionAmt < 0 {
			side = "SHORT"
		}
		holdDuration := e.GetHoldDuration(symbol, side)
		log.Printf("[%s][%s] Closing %s position: %.4f (held for %v)", e.name, symbol, side, currentPos.PositionAmt, holdDuration)
		if _, err := e.binance.ClosePosition(ctx, symbol, currentPos.PositionAmt); err != nil {
			return fmt.Errorf("failed to close position: %w", err)
		}
		e.clearPositionTracking(symbol, side)

	case "HOLD", "hold", "wait":
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

// =============================================================================
// Decision Context Building
// =============================================================================

// buildDecisionContext creates a decision.Context for AI decision making
func (e *Engine) buildDecisionContext(ctx context.Context) *decision.Context {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Build account info
	accountInfo := decision.AccountInfo{}
	if e.account != nil {
		accountInfo = decision.AccountInfo{
			TotalEquity:      e.account.TotalMarginBalance,
			AvailableBalance: e.account.AvailableBalance,
			UnrealizedPnL:    e.account.TotalUnrealizedProfit,
			TotalPnL:         e.account.TotalUnrealizedProfit,
			PositionCount:    len(e.positions),
		}
		if e.account.TotalMarginBalance > 0 {
			accountInfo.MarginUsedPct = (e.account.TotalMarginBalance - e.account.AvailableBalance) / e.account.TotalMarginBalance * 100
		}
	}

	// Build position info
	positions := make([]decision.PositionInfo, 0)
	for _, pos := range e.positions {
		if pos.PositionAmt == 0 {
			continue
		}

		side := "long"
		if pos.PositionAmt < 0 {
			side = "short"
		}

		var pnlPct float64
		if pos.EntryPrice > 0 {
			if pos.PositionAmt > 0 {
				pnlPct = ((pos.MarkPrice - pos.EntryPrice) / pos.EntryPrice) * 100
			} else {
				pnlPct = ((pos.EntryPrice - pos.MarkPrice) / pos.EntryPrice) * 100
			}
		}

		positions = append(positions, decision.PositionInfo{
			Symbol:           pos.Symbol,
			Side:             side,
			EntryPrice:       pos.EntryPrice,
			MarkPrice:        pos.MarkPrice,
			Quantity:         pos.PositionAmt,
			Leverage:         pos.Leverage,
			UnrealizedPnL:    pos.UnrealizedProfit,
			UnrealizedPnLPct: pnlPct,
			PeakPnLPct:       e.GetPeakPnL(pos.Symbol, side),
		})
	}

	// Build candidate coins
	candidateCoins := make([]decision.CandidateCoin, 0)
	for _, symbol := range e.getTradingPairs() {
		candidateCoins = append(candidateCoins, decision.CandidateCoin{
			Symbol:  symbol,
			Sources: []string{"strategy"},
		})
	}

	// Get leverage limits from strategy
	btcEthLeverage := 10
	altcoinLeverage := 20
	btcEthPosRatio := 5.0
	altcoinPosRatio := 1.0

	if e.strategy != nil {
		if e.strategy.Config.RiskControl.BTCETHMaxLeverage > 0 {
			btcEthLeverage = e.strategy.Config.RiskControl.BTCETHMaxLeverage
		}
		if e.strategy.Config.RiskControl.AltcoinMaxLeverage > 0 {
			altcoinLeverage = e.strategy.Config.RiskControl.AltcoinMaxLeverage
		}
		if e.strategy.Config.RiskControl.BTCETHMaxPositionValueRatio > 0 {
			btcEthPosRatio = e.strategy.Config.RiskControl.BTCETHMaxPositionValueRatio
		}
		if e.strategy.Config.RiskControl.AltcoinMaxPositionValueRatio > 0 {
			altcoinPosRatio = e.strategy.Config.RiskControl.AltcoinMaxPositionValueRatio
		}
	}

	return &decision.Context{
		CurrentTime:     time.Now().Format(time.RFC3339),
		RuntimeMinutes:  int(time.Since(e.startTime).Minutes()),
		CallCount:       e.callCount,
		Account:         accountInfo,
		Positions:       positions,
		CandidateCoins:  candidateCoins,
		BTCETHLeverage:  btcEthLeverage,
		AltcoinLeverage: altcoinLeverage,
		BTCETHPosRatio:  btcEthPosRatio,
		AltcoinPosRatio: altcoinPosRatio,
	}
}

// decisionToTradingDecision converts a decision.Decision to ai.TradingDecision for compatibility
func decisionToTradingDecision(d *decision.Decision) *ai.TradingDecision {
	// Map action types
	action := d.Action
	switch d.Action {
	case decision.ActionOpenLong:
		action = "BUY"
	case decision.ActionOpenShort:
		action = "SELL"
	case decision.ActionCloseLong, decision.ActionCloseShort:
		action = "CLOSE"
	case decision.ActionHold, decision.ActionWait:
		action = "HOLD"
	}

	return &ai.TradingDecision{
		Action:     action,
		Symbol:     d.Symbol,
		Confidence: float64(d.Confidence),
		Reasoning:  d.Reasoning,
		StopLoss:   d.StopLoss,
		TakeProfit: d.TakeProfit,
	}
}

// makeDecisionWithEngine uses the decision engine to make trading decisions
func (e *Engine) makeDecisionWithEngine(ctx context.Context) (*decision.FullDecision, error) {
	// Build context for decision making
	decisionCtx := e.buildDecisionContext(ctx)

	// Increment call count
	e.mu.Lock()
	e.callCount++
	e.mu.Unlock()

	// Make decision using the engine
	fullDecision, err := e.decisionEngine.MakeDecisionWithRetry(decisionCtx, 3)
	if err != nil {
		return nil, fmt.Errorf("decision engine failed: %w", err)
	}

	// Store the full decision
	e.mu.Lock()
	e.lastFullDecision = fullDecision
	e.mu.Unlock()

	return fullDecision, nil
}

// GetLastCoT returns the chain of thought from the last decision
func (e *Engine) GetLastCoT() string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.lastFullDecision != nil {
		return e.lastFullDecision.CoTTrace
	}
	return ""
}

// GetDecisionEngineStatus returns status information about the decision engine
func (e *Engine) GetDecisionEngineStatus() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	status := map[string]interface{}{
		"call_count":      e.callCount,
		"runtime_minutes": int(time.Since(e.startTime).Minutes()),
		"has_mcp_client":  e.mcpClient != nil,
	}

	if e.lastFullDecision != nil {
		status["last_decision_time"] = e.lastFullDecision.Timestamp
		status["last_ai_duration_ms"] = e.lastFullDecision.AIRequestDurationMs
		status["last_cot_length"] = len(e.lastFullDecision.CoTTrace)
		status["last_decision_count"] = len(e.lastFullDecision.Decisions)
	}

	return status
}

// =============================================================================
// Helper Functions
// =============================================================================

// isBTCETH checks if a symbol is BTC or ETH
func isBTCETH(symbol string) bool {
	return symbol == "BTCUSDT" || symbol == "ETHUSDT" ||
		symbol == "BTCUSD" || symbol == "ETHUSD" ||
		symbol == "BTCUSDC" || symbol == "ETHUSDC"
}

// getPositionPercent returns the position percentage to use for sizing
// Falls back through: strategy new fields -> legacy MaxPositionPercent -> config -> default 10%
func (e *Engine) getPositionPercent() float64 {
	// Check strategy first
	if e.strategy != nil {
		rc := e.strategy.Config.RiskControl

		// Legacy MaxPositionPercent field (most likely for existing strategies)
		if rc.MaxPositionPercent > 0 {
			return rc.MaxPositionPercent
		}
	}

	// Fallback to config
	if e.cfg != nil && e.cfg.MaxPositionPct > 0 {
		return e.cfg.MaxPositionPct
	}

	// Default to 10%
	return 10.0
}

// getLeverageLimit returns the max leverage for a symbol based on its type
func (e *Engine) getLeverageLimit(symbol string) int {
	if e.strategy == nil {
		// No strategy, use config fallback
		if e.cfg != nil && e.cfg.Leverage > 0 {
			return e.cfg.Leverage
		}
		return 10 // Default
	}
	rc := e.strategy.Config.RiskControl

	// Check new separate leverage fields first
	if isBTCETH(symbol) {
		if rc.BTCETHMaxLeverage > 0 {
			return rc.BTCETHMaxLeverage
		}
	} else {
		if rc.AltcoinMaxLeverage > 0 {
			return rc.AltcoinMaxLeverage
		}
	}

	// Fallback to legacy MaxLeverage field (for existing strategies)
	if rc.MaxLeverage > 0 {
		return rc.MaxLeverage
	}

	// Fallback to config
	if e.cfg != nil && e.cfg.Leverage > 0 {
		return e.cfg.Leverage
	}

	// Ultimate default
	if isBTCETH(symbol) {
		return 10
	}
	return 20
}

// getPositionKey generates a unique key for position tracking
func getPositionKey(symbol, side string) string {
	return symbol + "_" + side
}

// =============================================================================
// Risk Control Enforcement Functions
// =============================================================================

// enforcePositionValueRatio caps position size based on equity ratio
// Returns the capped position size and whether it was modified
func (e *Engine) enforcePositionValueRatio(positionSizeUSD, equity float64, symbol string) (float64, bool) {
	if e.strategy == nil {
		return positionSizeUSD, false
	}

	rc := e.strategy.Config.RiskControl
	var maxRatio float64

	if isBTCETH(symbol) {
		maxRatio = rc.BTCETHMaxPositionValueRatio
		if maxRatio <= 0 {
			maxRatio = 5.0 // Default 5x equity for BTC/ETH
		}
	} else {
		maxRatio = rc.AltcoinMaxPositionValueRatio
		if maxRatio <= 0 {
			maxRatio = 1.0 // Default 1x equity for altcoins
		}
	}

	maxPositionValue := equity * maxRatio
	if positionSizeUSD > maxPositionValue {
		log.Printf("[%s] Position size $%.2f exceeds max ratio (%.1fx equity = $%.2f), capping",
			e.name, positionSizeUSD, maxRatio, maxPositionValue)
		return maxPositionValue, true
	}

	return positionSizeUSD, false
}

// enforceMinPositionSize validates minimum position size
func (e *Engine) enforceMinPositionSize(positionSizeUSD float64, symbol string) error {
	if e.strategy == nil {
		return nil
	}

	rc := e.strategy.Config.RiskControl
	var minSize float64

	if isBTCETH(symbol) {
		// Try new field first
		minSize = rc.MinPositionSizeBTCETH
		if minSize <= 0 {
			// Fallback to legacy MinPositionUSD
			minSize = rc.MinPositionUSD
		}
		if minSize <= 0 {
			minSize = 60.0 // Default $60 for BTC/ETH
		}
	} else {
		// Try new field first
		minSize = rc.MinPositionSize
		if minSize <= 0 {
			// Fallback to legacy MinPositionUSD
			minSize = rc.MinPositionUSD
		}
		if minSize <= 0 {
			minSize = 12.0 // Default $12 for altcoins
		}
	}

	if positionSizeUSD < minSize {
		return fmt.Errorf("position size $%.2f below minimum $%.2f for %s", positionSizeUSD, minSize, symbol)
	}

	return nil
}

// enforceMaxPositions checks if we've reached max positions
func (e *Engine) enforceMaxPositions() error {
	if e.strategy == nil {
		return nil
	}

	maxPositions := e.strategy.Config.RiskControl.MaxPositions
	if maxPositions <= 0 {
		maxPositions = 3
	}

	e.mu.RLock()
	currentCount := len(e.positions)
	e.mu.RUnlock()

	if currentCount >= maxPositions {
		return fmt.Errorf("max positions (%d) reached", maxPositions)
	}

	return nil
}

// validateRiskRewardRatio validates TP/SL ratio meets minimum requirement
func (e *Engine) validateRiskRewardRatio(action string, stopLoss, takeProfit, currentPrice float64) error {
	if e.strategy == nil {
		return nil
	}

	minRatio := e.strategy.Config.RiskControl.MinRiskRewardRatio
	if minRatio <= 0 {
		minRatio = 3.0 // Default 3:1
	}

	// Only validate for open actions
	if action != "BUY" && action != "SELL" && action != "open_long" && action != "open_short" {
		return nil
	}

	// Calculate risk and reward based on action type
	var risk, reward float64
	isLong := action == "BUY" || action == "open_long"

	if isLong {
		risk = currentPrice - stopLoss
		reward = takeProfit - currentPrice
	} else {
		risk = stopLoss - currentPrice
		reward = currentPrice - takeProfit
	}

	if risk <= 0 || reward <= 0 {
		return fmt.Errorf("invalid SL/TP: risk=%.2f, reward=%.2f", risk, reward)
	}

	ratio := reward / risk
	if ratio < minRatio {
		return fmt.Errorf("risk-reward ratio %.2f below minimum %.2f", ratio, minRatio)
	}

	return nil
}

// applyMarginBuffer applies safety buffer to position size
func (e *Engine) applyMarginBuffer(positionSizeUSD float64) float64 {
	if e.strategy == nil {
		return positionSizeUSD * 0.98 // Default 98%
	}

	buffer := e.strategy.Config.RiskControl.MarginBuffer
	if buffer <= 0 || buffer > 1 {
		buffer = 0.98
	}

	return positionSizeUSD * buffer
}

// =============================================================================
// Position Management - Peak P&L Tracking
// =============================================================================

// UpdatePeakPnL updates the peak P&L for a position
func (e *Engine) UpdatePeakPnL(symbol, side string, currentPnLPct float64) {
	key := getPositionKey(symbol, side)

	e.peakPnLCacheMutex.Lock()
	defer e.peakPnLCacheMutex.Unlock()

	if current, exists := e.peakPnLCache[key]; !exists || currentPnLPct > current {
		e.peakPnLCache[key] = currentPnLPct
	}
}

// GetPeakPnL returns the peak P&L for a position
func (e *Engine) GetPeakPnL(symbol, side string) float64 {
	key := getPositionKey(symbol, side)

	e.peakPnLCacheMutex.RLock()
	defer e.peakPnLCacheMutex.RUnlock()

	return e.peakPnLCache[key]
}

// ClearPeakPnL clears the peak P&L cache when position closes
func (e *Engine) ClearPeakPnL(symbol, side string) {
	key := getPositionKey(symbol, side)

	e.peakPnLCacheMutex.Lock()
	defer e.peakPnLCacheMutex.Unlock()

	delete(e.peakPnLCache, key)
}

// =============================================================================
// Position Management - Hold Duration Tracking
// =============================================================================

// setPositionFirstSeen records when a position was first observed
func (e *Engine) setPositionFirstSeen(symbol, side string) {
	key := getPositionKey(symbol, side)

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.positionFirstSeenTime[key]; !exists {
		e.positionFirstSeenTime[key] = time.Now().UnixMilli()
	}
}

// GetHoldDuration returns how long a position has been held
func (e *Engine) GetHoldDuration(symbol, side string) time.Duration {
	key := getPositionKey(symbol, side)

	e.mu.RLock()
	defer e.mu.RUnlock()

	if firstSeen, exists := e.positionFirstSeenTime[key]; exists {
		return time.Since(time.UnixMilli(firstSeen))
	}
	return 0
}

// clearPositionTracking clears all tracking data for a closed position
func (e *Engine) clearPositionTracking(symbol, side string) {
	key := getPositionKey(symbol, side)

	e.mu.Lock()
	delete(e.positionFirstSeenTime, key)
	e.mu.Unlock()

	e.ClearPeakPnL(symbol, side)
}

// =============================================================================
// Daily Loss Monitoring
// =============================================================================

// shouldStopTrading checks if trading should be paused due to daily loss
func (e *Engine) shouldStopTrading() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check if we're in a pause period
	if !e.stopUntil.IsZero() && time.Now().Before(e.stopUntil) {
		return true
	}

	return false
}

// checkDailyLoss checks if daily loss limit has been exceeded
func (e *Engine) checkDailyLoss() bool {
	if e.strategy == nil || e.initialBalance <= 0 {
		return false
	}

	maxDailyLossPct := e.strategy.Config.RiskControl.MaxDailyLossPct
	if maxDailyLossPct <= 0 {
		return false // Disabled
	}

	e.mu.RLock()
	currentBalance := 0.0
	if e.account != nil {
		currentBalance = e.account.TotalWalletBalance + e.account.TotalUnrealizedProfit
	}
	initialBalance := e.initialBalance
	e.mu.RUnlock()

	if initialBalance <= 0 {
		return false
	}

	lossPct := ((initialBalance - currentBalance) / initialBalance) * 100

	if lossPct >= maxDailyLossPct {
		log.Printf("[%s] Daily loss limit triggered: %.2f%% >= %.2f%%", e.name, lossPct, maxDailyLossPct)
		return true
	}

	return false
}

// triggerTradingPause pauses trading for the configured duration
func (e *Engine) triggerTradingPause() {
	if e.strategy == nil {
		return
	}

	pauseMins := e.strategy.Config.RiskControl.StopTradingMins
	if pauseMins <= 0 {
		pauseMins = 60 // Default 60 minutes
	}

	e.mu.Lock()
	e.stopUntil = time.Now().Add(time.Duration(pauseMins) * time.Minute)
	e.mu.Unlock()

	log.Printf("[%s] Trading paused until %s", e.name, e.stopUntil.Format(time.RFC3339))
}

// resetDailyPnLIfNeeded resets daily P&L tracking at the start of a new day
func (e *Engine) resetDailyPnLIfNeeded() {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if 24 hours have passed since last reset
	if time.Since(e.lastResetTime) >= 24*time.Hour {
		if e.account != nil {
			e.initialBalance = e.account.TotalWalletBalance
		}
		e.dailyPnL = 0
		e.lastResetTime = time.Now()
		e.stopUntil = time.Time{} // Clear any pause
		log.Printf("[%s] Daily P&L reset. New initial balance: $%.2f", e.name, e.initialBalance)
	}
}

// =============================================================================
// Drawdown Monitor Goroutine
// =============================================================================

// startDrawdownMonitor starts background goroutine for drawdown checks
func (e *Engine) startDrawdownMonitor(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	log.Printf("[%s] Drawdown monitor started", e.name)

	for {
		select {
		case <-e.stopCh:
			log.Printf("[%s] Drawdown monitor stopped", e.name)
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.checkPositionDrawdown(ctx)
		}
	}
}

// checkPositionDrawdown checks if any positions should be closed due to drawdown
func (e *Engine) checkPositionDrawdown(ctx context.Context) {
	if e.strategy == nil {
		return
	}

	rc := e.strategy.Config.RiskControl
	drawdownThreshold := rc.DrawdownCloseThreshold
	if drawdownThreshold <= 0 {
		drawdownThreshold = 40.0 // Default 40%
	}

	minProfitForDrawdown := rc.MinProfitForDrawdown
	if minProfitForDrawdown <= 0 {
		minProfitForDrawdown = 5.0 // Default 5%
	}

	e.mu.RLock()
	positions := make([]*exchange.Position, 0)
	for _, pos := range e.positions {
		positions = append(positions, pos)
	}
	e.mu.RUnlock()

	for _, pos := range positions {
		if pos.PositionAmt == 0 {
			continue
		}

		// Calculate current P&L %
		var pnlPct float64
		if pos.EntryPrice > 0 {
			if pos.PositionAmt > 0 {
				pnlPct = ((pos.MarkPrice - pos.EntryPrice) / pos.EntryPrice) * 100
			} else {
				pnlPct = ((pos.EntryPrice - pos.MarkPrice) / pos.EntryPrice) * 100
			}
		}

		side := "LONG"
		if pos.PositionAmt < 0 {
			side = "SHORT"
		}

		// Update peak P&L
		e.UpdatePeakPnL(pos.Symbol, side, pnlPct)
		peakPnL := e.GetPeakPnL(pos.Symbol, side)

		// Only apply drawdown protection if we were profitable
		if peakPnL < minProfitForDrawdown {
			continue
		}

		// Calculate drawdown from peak
		drawdown := peakPnL - pnlPct

		if drawdown >= drawdownThreshold {
			log.Printf("[%s][%s] Drawdown alert: Peak=%.2f%%, Current=%.2f%%, Drawdown=%.2f%% >= %.2f%%",
				e.name, pos.Symbol, peakPnL, pnlPct, drawdown, drawdownThreshold)

			// Close the position
			log.Printf("[%s][%s] Closing position due to drawdown protection", e.name, pos.Symbol)
			if _, err := e.binance.ClosePosition(ctx, pos.Symbol, pos.PositionAmt); err != nil {
				log.Printf("[%s][%s] Failed to close position: %v", e.name, pos.Symbol, err)
			} else {
				e.clearPositionTracking(pos.Symbol, side)
			}
		}
	}
}

// =============================================================================
// Background Order Sync
// =============================================================================

// startOrderSync starts background goroutine to sync orders from Binance
func (e *Engine) startOrderSync(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.Printf("[%s] Order sync started (30s interval)", e.name)

	for {
		select {
		case <-e.orderSyncStop:
			log.Printf("[%s] Order sync stopped", e.name)
			return
		case <-e.stopCh:
			log.Printf("[%s] Order sync stopped", e.name)
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.syncOrdersFromBinance(ctx)
		}
	}
}

// syncOrdersFromBinance fetches and reconciles positions from Binance
func (e *Engine) syncOrdersFromBinance(ctx context.Context) {
	// Get positions from Binance
	positions, err := e.binance.GetPositions(ctx)
	if err != nil {
		log.Printf("[%s] Order sync failed: %v", e.name, err)
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Track which positions still exist
	currentSymbols := make(map[string]bool)

	// Update positions
	newPositions := make(map[string]*exchange.Position)
	for i := range positions {
		pos := &positions[i]
		newPositions[pos.Symbol] = pos
		currentSymbols[pos.Symbol] = true

		// Track new positions
		if pos.PositionAmt != 0 {
			side := "LONG"
			if pos.PositionAmt < 0 {
				side = "SHORT"
			}
			key := getPositionKey(pos.Symbol, side)
			if _, exists := e.positionFirstSeenTime[key]; !exists {
				e.positionFirstSeenTime[key] = time.Now().UnixMilli()
				log.Printf("[%s] New position detected: %s %s", e.name, pos.Symbol, side)
			}
		}
	}

	// Detect closed positions
	for symbol, oldPos := range e.positions {
		if oldPos.PositionAmt != 0 {
			newPos, exists := newPositions[symbol]
			if !exists || newPos.PositionAmt == 0 {
				side := "LONG"
				if oldPos.PositionAmt < 0 {
					side = "SHORT"
				}
				log.Printf("[%s] Position closed: %s %s", e.name, symbol, side)

				// Clear tracking data (need to release lock temporarily)
				key := getPositionKey(symbol, side)
				delete(e.positionFirstSeenTime, key)

				// Clear peak P&L (handled outside lock)
				go e.ClearPeakPnL(symbol, side)
			}
		}
	}

	e.positions = newPositions

	// Update account info
	account, err := e.binance.GetAccountInfo(ctx)
	if err == nil {
		e.account = account
	}
}
