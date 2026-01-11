package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"auto-trader-ahh/ai"
	"auto-trader-ahh/config"
	"auto-trader-ahh/decision"
	"auto-trader-ahh/events"
	"auto-trader-ahh/exchange"
	"auto-trader-ahh/market"
	"auto-trader-ahh/mcp"
	"auto-trader-ahh/store"
)

// Notifier interfaces for broadcasting events
type Notifier interface {
	Broadcast(evt events.Event)
}

type Engine struct {
	id           string
	name         string
	cfg          *config.Config
	strategy     *store.Strategy
	traderConfig *store.TraderConfig // Trader-specific config (for reasoning mode, etc.)
	aiClient     *ai.Client          // Legacy AI client (for backward compatibility)
	binance      *exchange.BinanceClient
	dataProvider *market.DataProvider
	notifier     Notifier

	// Decision Engine (NOFX-style XML parsing with CoT)
	mcpClient      mcp.AIClient
	decisionEngine *decision.Engine
	callCount      int       // Number of AI calls made
	startTime      time.Time // Engine start time

	running bool
	stopCh  chan struct{}
	mu      sync.RWMutex

	// State
	lastDecisions    map[string]*ai.TradingDecision
	lastFullDecision *decision.FullDecision // Latest full decision with CoT
	positions        map[string]*exchange.Position
	account          *exchange.AccountInfo

	// Stores
	decisionStore *store.DecisionStore
	equityStore   *store.EquityStore
	tradeStore    *store.TradeStore

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

	// SL/TP Order Tracking
	bracketOrders      map[string]*BracketOrderIDs // key: symbol -> SL/TP order IDs
	bracketOrdersMutex sync.RWMutex

	// Dynamic Coin Source Cache
	dynamicCoins       []string
	lastDynamicRefresh time.Time
}

// BracketOrderIDs tracks stop-loss and take-profit order IDs for a position
type BracketOrderIDs struct {
	StopLossOrderID   int64
	TakeProfitOrderID int64
	EntryPrice        float64
	StopLossPct       float64
	TakeProfitPct     float64
}

type TradeLog struct {
	Timestamp   time.Time
	Symbol      string
	Action      string
	Decision    *ai.TradingDecision
	RawAI       string
	MarketData  string
	Error       string
	CoTTrace    string  // Chain of thought from AI reasoning
	RealizedPnL float64 // PnL realized when closing a position
}

// NewEngine creates a new trading engine with strategy support
func NewEngine(id, name string, aiClient *ai.Client, binance *exchange.BinanceClient, strategy *store.Strategy, traderCfg *store.TraderConfig, cfg *config.Config, notifier Notifier) *Engine {
	dataProvider := market.NewDataProvider(binance)

	// Determine API Key and Model (Trader config > Global config)
	apiKey := cfg.OpenRouterAPIKey
	model := cfg.OpenRouterModel

	if traderCfg != nil {
		if traderCfg.OpenRouterAPIKey != "" {
			apiKey = traderCfg.OpenRouterAPIKey
		}
		if traderCfg.OpenRouterModel != "" {
			model = traderCfg.OpenRouterModel
		}
	}

	// Create MCP client from config (uses OpenRouter by default)
	mcpClient := mcp.NewOpenRouterClient(apiKey, model)

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
		traderConfig:   traderCfg,
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
		tradeStore:     store.NewTradeStore(),

		// Initialize position management maps
		peakPnLCache:          make(map[string]float64),
		positionFirstSeenTime: make(map[string]int64),

		// Initialize bracket orders tracking
		bracketOrders: make(map[string]*BracketOrderIDs),

		// Initialize daily tracking
		lastResetTime:  time.Now(),
		initialBalance: 0,
		notifier:       notifier,
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
	e.initialBalance = account.TotalMarginBalance // Set initial balance for daily loss tracking (includes unrealized P&L)
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

// SetStrategy updates the engine's strategy configuration at runtime.
// This allows live updates without restarting the engine.
func (e *Engine) SetStrategy(strategy *store.Strategy) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if strategy == nil {
		return
	}

	oldSimpleMode := false
	oldTrailingStop := false
	if e.strategy != nil {
		oldSimpleMode = e.strategy.Config.SimpleMode
		oldTrailingStop = e.strategy.Config.RiskControl.EnableTrailingStop
	}

	e.strategy = strategy

	// Log important changes
	newSimpleMode := strategy.Config.SimpleMode
	newTrailingStop := strategy.Config.RiskControl.EnableTrailingStop

	if oldSimpleMode != newSimpleMode {
		log.Printf("[%s] Strategy updated: SimpleMode changed from %v to %v", e.name, oldSimpleMode, newSimpleMode)
	}
	if oldTrailingStop != newTrailingStop {
		log.Printf("[%s] Strategy updated: TrailingStop changed from %v to %v", e.name, oldTrailingStop, newTrailingStop)
	}

	log.Printf("[%s] Strategy config reloaded successfully", e.name)
}

// GetStrategyID returns the current strategy ID
func (e *Engine) GetStrategyID() string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.strategy != nil {
		return e.strategy.ID
	}
	return ""
}

func (e *Engine) getTradingPairs() []string {
	if e.strategy != nil {
		sourceType := e.strategy.Config.CoinSource.SourceType

		// If dynamic source is selected
		if sourceType == "volume_top" || sourceType == "oi_top" || sourceType == "dynamic" {
			// Refresh cache if older than 5 minutes or empty
			if time.Since(e.lastDynamicRefresh) > 5*time.Minute || len(e.dynamicCoins) == 0 {
				log.Printf("[%s] Refreshing top volume coins...", e.name)
				// Fetch top 20 coins
				topCoins, err := e.binance.GetTopVolumeCoins(context.Background(), 20)
				if err != nil {
					log.Printf("[%s] Failed to fetch top coins, using previous list/static fallback: %v", e.name, err)
					// Verify we have something to fall back to
					if len(e.dynamicCoins) == 0 {
						return e.strategy.Config.CoinSource.StaticCoins
					}
				} else {
					e.dynamicCoins = topCoins
					e.lastDynamicRefresh = time.Now()
					log.Printf("[%s] Updated dynamic coin list: %v", e.name, e.dynamicCoins)
				}
			}
			return e.dynamicCoins
		}

		// Fallback to static list
		if len(e.strategy.Config.CoinSource.StaticCoins) > 0 {
			return e.strategy.Config.CoinSource.StaticCoins
		}
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

	// Check Trading Mode: If Copy Trading is enabled, switch to monitoring mode
	if e.strategy != nil && e.strategy.Config.TradingMode == "copy_trade" {
		e.runCopyTradingCycle(ctx)
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

		// SAFETY: Emergency Shutdown Check
		if e.strategy != nil && e.strategy.Config.RiskControl.EnableEmergencyShutdown {
			minBal := e.strategy.Config.RiskControl.EmergencyMinBalance
			if minBal <= 0 {
				minBal = 60.0
			}
			// Check Equity (TotalMarginBalance)
			if account.TotalMarginBalance <= minBal {
				log.Printf("[%s] 🚨 EMERGENCY SHUTDOWN TRIGGERED! Equity $%.2f is below safety limit $%.2f. Stopping trading cycle.",
					e.name, account.TotalMarginBalance, minBal)
				// We return immediately to prevent any further trading actions (opening OR managed closing)
				// Existing positions will rely on their hard SL/TP orders on the exchange.
				return
			}
		}

		// Save equity snapshot
		e.equityStore.Save(&store.EquitySnapshot{
			TraderID:      e.id,
			Timestamp:     time.Now(),
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

	// Determine pairs to analyze (Optimize AI Token Usage)
	var pairsToAnalyze []string

	e.mu.RLock()
	// Get max positions config
	maxPositions := 3
	if e.strategy != nil && e.strategy.Config.RiskControl.MaxPositions > 0 {
		maxPositions = e.strategy.Config.RiskControl.MaxPositions
	}

	// Collect active positions
	activeSymbols := make([]string, 0)
	for _, pos := range e.positions {
		if pos.PositionAmt != 0 {
			activeSymbols = append(activeSymbols, pos.Symbol)
		}
	}
	e.mu.RUnlock()

	// Logic: If max positions reached, ONLY analyze open positions to save tokens
	if len(activeSymbols) >= maxPositions {
		log.Printf("[%s] Max positions reached (%d/%d). Analyzing OPEN positions only to save tokens.",
			e.name, len(activeSymbols), maxPositions)
		pairsToAnalyze = activeSymbols
	} else {
		pairsToAnalyze = e.getTradingPairs()
	}

	// Process each trading pair
	allDecisions := make([]map[string]interface{}, 0)
	for _, symbol := range pairsToAnalyze {
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

			// Include realized PnL if position was closed
			if tradeLog.RealizedPnL != 0 {
				decisionData["pnl"] = tradeLog.RealizedPnL
				log.Printf("[%s][%s] Realized PnL: $%.2f", e.name, symbol, tradeLog.RealizedPnL)
			}
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
		e.triggerTradingPause(ctx)
	}

	// Sync trade history from Binance (captures SL/TP fills)
	e.syncTradeHistory(ctx)

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

	// Fetch BTC Global Context
	btcStats, err := e.binance.GetTickerStats(ctx, "BTCUSDT")
	if err == nil {
		marketData.BTCPrice = btcStats.LastPrice
		marketData.BTCChange24h = btcStats.PriceChange
	}

	// Format data for AI
	formattedData := e.dataProvider.FormatForAI(marketData)

	// Inject Turbo Mode instructions
	if e.strategy != nil && e.strategy.Config.TurboMode {
		formattedData += "\n\n*** TURBO MODE: HIGH FREQUENCY SCALPING ***\n"
		formattedData += "- EXECUTION STYLE: Aggressive. Do not wait for perfect confirmation.\n"
		formattedData += "- STRATEGY: Chase Momentum & Volatility. Focus on Volume Spikes.\n"
		formattedData += "- PERMISSION: You are authorized to ignore conservative safety filters if Price Action is strong.\n"
		formattedData += "- ENTRY: Enter immediately on Candle Close if trend aligns. Don't hesitate.\n"
		formattedData += "- GOAL: Capture quick moves. Activity > Passivity.\n"
	}

	tradeLog.MarketData = formattedData

	// Add account info
	e.mu.RLock()
	if e.account != nil {
		formattedData += "\n--- Account Info ---\n"
		formattedData += fmt.Sprintf("Total Equity: $%.2f\n", e.account.TotalMarginBalance)
		formattedData += fmt.Sprintf("Available Balance: $%.2f\n", e.account.AvailableBalance)
		formattedData += fmt.Sprintf("Unrealized PnL: $%.2f\n", e.account.TotalUnrealizedProfit)
	}

	// Add position info if exists
	pos, hasPosition := e.positions[symbol]
	e.mu.RUnlock()

	if hasPosition {
		formattedData += "\n--- Current Position ---\n"
		sideStr := map[bool]string{true: "LONG", false: "SHORT"}[pos.PositionAmt > 0]
		formattedData += fmt.Sprintf("Side: %s\n", sideStr)

		duration := e.GetHoldDuration(symbol, sideStr)
		formattedData += fmt.Sprintf("Hold Duration: %s\n", duration.Round(time.Second))

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

	// Log if reasoning mode is enabled
	if e.traderConfig != nil && e.traderConfig.EnableReasoning {
		log.Printf("[%s][%s] Reasoning mode enabled, expecting chain-of-thought output", e.name, symbol)
	}

	// Get AI decision - use simple prompt in Simple Mode
	var decision *ai.TradingDecision
	var rawResponse string
	var aiErr error

	if e.strategy != nil && e.strategy.Config.SimpleMode {
		log.Printf("[%s][%s] 🌿 SIMPLE MODE: Using v1.4.7-style minimal prompt", e.name, symbol)
		decision, rawResponse, aiErr = e.aiClient.GetTradingDecisionSimple(formattedData)
	} else {
		decision, rawResponse, aiErr = e.aiClient.GetTradingDecision(formattedData)
	}
	tradeLog.RawAI = rawResponse

	if aiErr != nil {
		tradeLog.Error = fmt.Sprintf("AI decision failed: %v", aiErr)
		if e.notifier != nil {
			e.notifier.Broadcast(events.Event{
				Type:      events.TypeError,
				TraderID:  e.id,
				Symbol:    symbol,
				Message:   tradeLog.Error,
				Timestamp: time.Now().UnixMilli(),
			})
		}
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
		// Multi-Timeframe Confirmation (only for new positions)
		if !hasPosition && (decision.Action == "BUY" || decision.Action == "SELL") {
			if e.strategy != nil && e.strategy.Config.Indicators.EnableMultiTF {
				confirmTF := e.strategy.Config.Indicators.ConfirmationTimeframe
				if confirmTF == "" {
					confirmTF = "15m"
				}

				// Get higher timeframe data
				htfData, err := e.dataProvider.GetMarketDataWithConfig(ctx, symbol, confirmTF, 50)
				if err != nil {
					log.Printf("[%s][%s] Failed to get %s data for MTF confirmation: %v", e.name, symbol, confirmTF, err)
					// Continue without confirmation if we can't get data
				} else {
					// Check if higher timeframe agrees with trade direction
					htfBullish := htfData.EMA9 > htfData.EMA21
					wantLong := decision.Action == "BUY"

					if (wantLong && !htfBullish) || (!wantLong && htfBullish) {
						log.Printf("[%s][%s] ❌ BLOCKED: Multi-TF disagreement. 5m says %s but %s shows %s trend (EMA9: %.2f, EMA21: %.2f)",
							e.name, symbol, decision.Action, confirmTF,
							map[bool]string{true: "BULLISH", false: "BEARISH"}[htfBullish],
							htfData.EMA9, htfData.EMA21)
						tradeLog.Error = fmt.Sprintf("blocked: %s timeframe disagrees (%s vs %s)",
							confirmTF,
							map[bool]string{true: "BULLISH", false: "BEARISH"}[htfBullish],
							decision.Action)
						return tradeLog
					}
					log.Printf("[%s][%s] ✅ Multi-TF confirmed: Both 5m and %s agree on %s",
						e.name, symbol, confirmTF, decision.Action)
				}
			}
		}

		realizedPnL, err := e.executeTrade(ctx, symbol, decision, hasPosition, pos)
		if err != nil {
			tradeLog.Error = fmt.Sprintf("trade execution failed: %v", err)
			if e.notifier != nil {
				e.notifier.Broadcast(events.Event{
					Type:      events.TypeError,
					TraderID:  e.id,
					Symbol:    symbol,
					Message:   tradeLog.Error,
					Timestamp: time.Now().UnixMilli(),
				})
			}
		} else if realizedPnL != 0 {
			tradeLog.RealizedPnL = realizedPnL
		}
	} else {
		log.Printf("[%s][%s] Confidence too low (%.0f%% < %.0f%%), skipping trade",
			e.name, symbol, decision.Confidence, minConfidence)
	}

	return tradeLog
}

// executeTrade executes the trade and returns realized PnL (if closing) and error
func (e *Engine) executeTrade(ctx context.Context, symbol string, decision *ai.TradingDecision, hasPosition bool, currentPos *exchange.Position) (float64, error) {
	// CRITICAL: Reject invalid symbols - "ALL" is only for wait/hold, never for actual trades
	if symbol == "ALL" || symbol == "" {
		return 0, fmt.Errorf("invalid symbol '%s' - cannot execute trade on ALL/empty symbol", symbol)
	}

	// Get account info for position sizing
	account, err := e.binance.GetAccountInfo(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get account info: %w", err)
	}

	// Get current price
	ticker, err := e.binance.GetTicker(ctx, symbol)
	if err != nil {
		return 0, fmt.Errorf("failed to get price: %w", err)
	}

	// For open actions, apply all risk controls
	isOpenAction := decision.Action == "BUY" || decision.Action == "SELL" ||
		decision.Action == "open_long" || decision.Action == "open_short"

	if isOpenAction && !hasPosition {
		// 1. Check max positions
		if err := e.enforceMaxPositions(); err != nil {
			log.Printf("[%s][%s] %v, skipping new position", e.name, symbol, err)
			return 0, fmt.Errorf("skipped: %w", err)
		}

		// 2. Adjust and Validate SL/TP
		// Apply auto-adjustment based on strategy config (fixes R:R mismatches)
		slPct, tpPct := e.getSLTPPercentages(decision)
		decision.StopLossPct = slPct
		decision.TakeProfitPct = tpPct

		if err := e.validateRiskRewardRatioPct(slPct, tpPct); err != nil {
			log.Printf("[%s][%s] %v, skipping trade", e.name, symbol, err)
			return 0, fmt.Errorf("skipped: %w", err)
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

	// Log decision details if reasoning provided
	if decision.Reasoning != "" {
		log.Printf("[%s][%s] %s Reasoning: %s", e.name, symbol, decision.Action, decision.Reasoning)
	}

	// Calculate position size based on balance and strategy config
	// Use available balance to calculate position size
	// Default to 10% of equity if not specified
	// Use strategy config for max position %
	maxPosPct := e.getPositionPercent()
	if e.strategy != nil && e.strategy.Config.RiskControl.MaxPositionPercent > 0 {
		maxPosPct = e.strategy.Config.RiskControl.MaxPositionPercent
	}

	// Base calculation
	positionSizeUSD := (e.account.TotalMarginBalance * maxPosPct) / 100

	// Apply margin safety check (COPIED FROM NOFX)
	// ⚠️ Auto-adjust position size if insufficient margin
	// Formula: totalRequired = positionSize/leverage + positionSize*0.001 + positionSize/leverage*0.01
	//        = positionSize * (1.01/leverage + 0.001)
	marginFactor := 1.01/float64(leverage) + 0.001
	maxAffordablePositionSize := e.account.AvailableBalance / marginFactor

	if positionSizeUSD > maxAffordablePositionSize {
		// Cap at max affordable - margin buffer will be applied later via applyMarginBuffer()
		// NOTE: Do NOT multiply by 0.98 here as that would double-apply the buffer
		log.Printf("[%s][%s] ⚠️ Position size $%.2f exceeds max affordable $%.2f, capping to max",
			e.name, symbol, positionSizeUSD, maxAffordablePositionSize)
		positionSizeUSD = maxAffordablePositionSize
	}

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
			return 0, fmt.Errorf("skipped: %w", err)
		}
	}

	// IMPORTANT: positionSizeUSD is the MARGIN amount, not position value!
	// With leverage, the actual position value = margin × leverage
	actualPositionValue := positionSizeUSD * float64(leverage)
	quantity := actualPositionValue / ticker.Price

	log.Printf("[%s][%s] Position calculation: margin=$%.2f × %dx leverage = $%.2f position value → %.8f %s",
		e.name, symbol, positionSizeUSD, leverage, actualPositionValue, quantity, symbol)

	// VALIDATION: Ensure quantity meets minimum requirements after rounding
	// Binance enforces minimum quantities per symbol (e.g., 0.001 BTC)
	minQuantity := 0.001 // Default for BTC/ETH
	if !isBTCETH(symbol) {
		// For altcoins, calculate based on a reasonable minimum notional ($10)
		minQuantity = 10.0 / ticker.Price
	}

	if quantity < minQuantity {
		return 0, fmt.Errorf("skipped: quantity %.8f below minimum %.8f for %s (position $%.2f too small, increase position size)",
			quantity, minQuantity, symbol, positionSizeUSD)
	}

	// CRITICAL: Before opening any new position, cancel any orphaned SL/TP orders for this symbol
	// This prevents the "-4130: An open stop or take profit order...is existing" error
	if !hasPosition && (decision.Action == "BUY" || decision.Action == "SELL" || decision.Action == "open_long" || decision.Action == "open_short") {
		e.cancelOrphanedOrders(ctx, symbol)
	}

	switch decision.Action {
	case "BUY", "open_long":
		if hasPosition && currentPos.PositionAmt > 0 {
			log.Printf("[%s][%s] Already in LONG position, skipping BUY", e.name, symbol)
			return 0, fmt.Errorf("skipped: already in LONG position")
		}
		// Require explicit close of opposite position (matching NOFX behavior)
		if hasPosition && currentPos.PositionAmt < 0 {
			log.Printf("[%s][%s] Already has SHORT position, close it first before opening LONG", e.name, symbol)
			return 0, fmt.Errorf("skipped: %s already has SHORT position, close it first", symbol)
		}
		log.Printf("[%s][%s] Opening LONG: %.4f @ $%.2f (margin: $%.2f, position: $%.2f, leverage: %dx)",
			e.name, symbol, quantity, ticker.Price, positionSizeUSD, actualPositionValue, leverage)
		openOrder, err := e.binance.PlaceOrder(ctx, symbol, "BUY", "MARKET", quantity, 0, false)
		if err != nil {
			return 0, fmt.Errorf("failed to open long: %w", err)
		}
		e.setPositionFirstSeen(symbol, "LONG")

		// Use actual fill data from order response
		entryPrice := ticker.Price
		filledQty := quantity
		if openOrder != nil {
			// Verify order was filled
			if openOrder.Status != "FILLED" {
				log.Printf("[%s][%s] ⚠️ LONG order status is %s (not FILLED), verifying position...",
					e.name, symbol, openOrder.Status)
				// Query actual position from Binance to get real fill data
				if positions, err := e.binance.GetPositions(ctx); err == nil {
					for _, pos := range positions {
						if pos.Symbol == symbol && pos.PositionAmt > 0 {
							filledQty = pos.PositionAmt
							entryPrice = pos.EntryPrice
							log.Printf("[%s][%s] Position verified from exchange: qty=%.4f, entry=$%.4f",
								e.name, symbol, filledQty, entryPrice)
							break
						}
					}
				}
			} else if openOrder.AvgPrice > 0 {
				entryPrice = openOrder.AvgPrice
				if openOrder.ExecutedQty > 0 {
					filledQty = openOrder.ExecutedQty
				}
				log.Printf("[%s][%s] LONG filled: price=$%.4f, qty=%.4f", e.name, symbol, entryPrice, filledQty)
			}
		} else {
			log.Printf("[%s][%s] ⚠️ No order response, using expected values: price=$%.4f, qty=%.4f",
				e.name, symbol, entryPrice, filledQty)
		}

		// Update positions map with actual fill data
		e.mu.Lock()
		e.positions[symbol] = &exchange.Position{
			Symbol:      symbol,
			PositionAmt: filledQty,
			EntryPrice:  entryPrice,
			MarkPrice:   entryPrice,
			Leverage:    leverage,
		}
		e.mu.Unlock()

		// Place bracket orders (SL/TP) on exchange using actual entry price
		// If trailing stop is enabled, only place SL - let TSL handle profits
		slPct, tpPct := e.getSLTPPercentages(decision)
		if slPct > 0 {
			if e.strategy.Config.RiskControl.EnableTrailingStop {
				// Only place SL, TSL will handle profit-taking
				log.Printf("[%s][%s] Trailing stop enabled - placing SL only, TSL will handle profits", e.name, symbol)
				e.placeStopLossOnly(ctx, symbol, true, entryPrice, slPct)
			} else if tpPct > 0 {
				e.placeBracketOrders(ctx, symbol, true, entryPrice, slPct, tpPct)
			}
		}

	case "SELL", "open_short":
		if hasPosition && currentPos.PositionAmt < 0 {
			log.Printf("[%s][%s] Already in SHORT position, skipping SELL", e.name, symbol)
			return 0, fmt.Errorf("skipped: already in SHORT position")
		}
		// Require explicit close of opposite position (matching NOFX behavior)
		if hasPosition && currentPos.PositionAmt > 0 {
			log.Printf("[%s][%s] Already has LONG position, close it first before opening SHORT", e.name, symbol)
			return 0, fmt.Errorf("skipped: %s already has LONG position, close it first", symbol)
		}
		log.Printf("[%s][%s] Opening SHORT: %.4f @ $%.2f (margin: $%.2f, position: $%.2f, leverage: %dx)",
			e.name, symbol, quantity, ticker.Price, positionSizeUSD, actualPositionValue, leverage)
		openOrder, err := e.binance.PlaceOrder(ctx, symbol, "SELL", "MARKET", quantity, 0, false)
		if err != nil {
			return 0, fmt.Errorf("failed to open short: %w", err)
		}
		e.setPositionFirstSeen(symbol, "SHORT")

		// Use actual fill data from order response
		entryPrice := ticker.Price
		filledQty := quantity
		if openOrder != nil {
			// Verify order was filled
			if openOrder.Status != "FILLED" {
				log.Printf("[%s][%s] ⚠️ SHORT order status is %s (not FILLED), verifying position...",
					e.name, symbol, openOrder.Status)
				// Query actual position from Binance to get real fill data
				if positions, err := e.binance.GetPositions(ctx); err == nil {
					for _, pos := range positions {
						if pos.Symbol == symbol && pos.PositionAmt < 0 {
							filledQty = -pos.PositionAmt // Convert to positive
							entryPrice = pos.EntryPrice
							log.Printf("[%s][%s] Position verified from exchange: qty=%.4f, entry=$%.4f",
								e.name, symbol, filledQty, entryPrice)
							break
						}
					}
				}
			} else if openOrder.AvgPrice > 0 {
				entryPrice = openOrder.AvgPrice
				if openOrder.ExecutedQty > 0 {
					filledQty = openOrder.ExecutedQty
				}
				log.Printf("[%s][%s] SHORT filled: price=$%.4f, qty=%.4f", e.name, symbol, entryPrice, filledQty)
			}
		} else {
			log.Printf("[%s][%s] ⚠️ No order response, using expected values: price=$%.4f, qty=%.4f",
				e.name, symbol, entryPrice, filledQty)
		}

		// Update positions map with actual fill data
		e.mu.Lock()
		e.positions[symbol] = &exchange.Position{
			Symbol:      symbol,
			PositionAmt: -filledQty, // Negative for short
			EntryPrice:  entryPrice,
			MarkPrice:   entryPrice,
			Leverage:    leverage,
		}
		e.mu.Unlock()

		// Place bracket orders (SL/TP) on exchange using actual entry price
		// If trailing stop is enabled, only place SL - let TSL handle profits
		slPct, tpPct := e.getSLTPPercentages(decision)
		if slPct > 0 {
			if e.strategy.Config.RiskControl.EnableTrailingStop {
				// Only place SL, TSL will handle profit-taking
				log.Printf("[%s][%s] Trailing stop enabled - placing SL only, TSL will handle profits", e.name, symbol)
				e.placeStopLossOnly(ctx, symbol, false, entryPrice, slPct)
			} else if tpPct > 0 {
				e.placeBracketOrders(ctx, symbol, false, entryPrice, slPct, tpPct)
			}
		}

	case "CLOSE", "close_long", "close_short":
		if !hasPosition {
			log.Printf("[%s][%s] No position to close", e.name, symbol)
			return 0, fmt.Errorf("skipped: no position to close")
		}
		side := "LONG"
		if currentPos.PositionAmt < 0 {
			side = "SHORT"
		}

		// Calculate PnL percentage
		var pnlPct float64
		if currentPos.EntryPrice > 0 {
			if currentPos.PositionAmt > 0 { // Long position
				pnlPct = ((currentPos.MarkPrice - currentPos.EntryPrice) / currentPos.EntryPrice) * 100
			} else { // Short position
				pnlPct = ((currentPos.EntryPrice - currentPos.MarkPrice) / currentPos.EntryPrice) * 100
			}
		}

		// SMART LOSS MANAGEMENT V3: Stricter protection against premature exits
		// The AI was closing at -0.76% with 90% confidence, losing $298.
		// We need to be MORE protective of positions in the noise zone.
		//
		// Rules (configurable via Strategy > Risk Control > Noise Zone):
		// 1. SIGNIFICANT LOSS (< lower bound): Allow close immediately
		// 2. NOISE ZONE (lower to upper bound): BLOCK most closes
		//    - Only allow if confidence >= threshold (very rare)
		//    - AND position has been open > min hold minutes (not brand new)
		// 3. PROFIT ZONE (> upper bound): Allow close to lock in gains

		// Get noise zone settings from config (with defaults)
		rc := e.strategy.Config.RiskControl
		enableNoiseZone := true // Default enabled
		significantLossThreshold := -1.5
		noiseZoneCeiling := 1.5
		minHoldBeforeClose := 10

		if e.strategy != nil {
			// Check if noise zone protection is disabled
			if !rc.EnableNoiseZoneProtection && rc.NoiseZoneLowerBound == 0 && rc.NoiseZoneUpperBound == 0 {
				// Fields are all zero/false - use defaults (enabled)
			} else if !rc.EnableNoiseZoneProtection {
				// Explicitly disabled
				enableNoiseZone = false
			}

			// Get configurable bounds
			if rc.NoiseZoneLowerBound != 0 {
				significantLossThreshold = rc.NoiseZoneLowerBound
			}
			if rc.NoiseZoneUpperBound != 0 {
				noiseZoneCeiling = rc.NoiseZoneUpperBound
			}
			if rc.MinHoldBeforeClose > 0 {
				minHoldBeforeClose = rc.MinHoldBeforeClose
			}
		}

		// Get high confidence threshold from config (default 95% for noise zone override)
		highConfidenceThreshold := 95.0
		if e.strategy != nil && rc.HighConfidenceCloseThreshold > 0 {
			highConfidenceThreshold = rc.HighConfidenceCloseThreshold
		}

		isHighConfidence := decision.Confidence >= highConfidenceThreshold
		holdDuration := e.GetHoldDuration(symbol, side)
		holdMins := holdDuration.Minutes()
		isNewPosition := holdMins < float64(minHoldBeforeClose)

		// Skip noise zone protection if disabled
		if !enableNoiseZone {
			log.Printf("[%s][%s] ⚙️ Noise zone protection DISABLED - allowing close (PnL: %.2f%%)", e.name, symbol, pnlPct)
			// Continue to close...
		} else if pnlPct < significantLossThreshold {
			// Case 1: Significant loss - ALLOW (but log warning if position is new)
			if isNewPosition {
				log.Printf("[%s][%s] ⚠️ WARNING: Closing NEW position at loss (%.2f%% in %.1f mins). Consider if SL is too tight.",
					e.name, symbol, pnlPct, holdMins)
			}
			log.Printf("[%s][%s] ✅ ALLOWING loss cut: Loss %.2f%% exceeds threshold %.2f%%.",
				e.name, symbol, pnlPct, significantLossThreshold)
			// Continue to close...
		} else if pnlPct >= noiseZoneCeiling {
			// Case 2: Good profit - ALLOW (taking profits)
			log.Printf("[%s][%s] ✅ ALLOWING profit take: Profit %.2f%% exceeds threshold %.2f%%.",
				e.name, symbol, pnlPct, noiseZoneCeiling)
			// Continue to close...
		} else {
			// Case 3: NOISE ZONE (lower to upper bound) - BLOCK unless very high confidence
			if isNewPosition {
				// NEW positions in noise zone: ALWAYS block, no override
				log.Printf("[%s][%s] ❌ BLOCKED: Position too new (%.1f mins) and in noise zone (PnL: %.2f%%). Let it develop.",
					e.name, symbol, holdMins, pnlPct)
				return 0, fmt.Errorf("blocked: position only %.1f mins old, in noise zone (%.2f%%). Wait for development", holdMins, pnlPct)
			} else if isHighConfidence {
				// OLDER positions with very high confidence: Allow override (rare)
				log.Printf("[%s][%s] ⚡ OVERRIDE: High confidence (%.1f%%) allows closing in noise zone (PnL: %.2f%%, held %.1f mins).",
					e.name, symbol, decision.Confidence, pnlPct, holdMins)
				// Continue to close...
			} else {
				// OLDER positions without high confidence: BLOCK
				log.Printf("[%s][%s] ❌ BLOCKED: Cannot close in noise zone (PnL: %.2f%%). Need %.0f%% confidence, got %.1f%%.",
					e.name, symbol, pnlPct, highConfidenceThreshold, decision.Confidence)
				return 0, fmt.Errorf("blocked: PnL %.2f%% in noise zone (need >%.1f%% profit OR >%.0f%% confidence)",
					pnlPct, noiseZoneCeiling, highConfidenceThreshold)
			}
		}

		// Estimate P&L before closing (for logging)
		estimatedPnL := currentPos.UnrealizedProfit

		log.Printf("[%s][%s] Closing %s position: %.4f (held for %v, estimated profit: $%.2f = %.2f%%)", e.name, symbol, side, currentPos.PositionAmt, holdDuration, estimatedPnL, pnlPct)
		closeOrder, err := e.binance.ClosePosition(ctx, symbol, currentPos.PositionAmt)
		if err != nil {
			return 0, fmt.Errorf("failed to close position: %w", err)
		}
		e.clearPositionTracking(symbol, side)
		e.cancelBracketOrders(ctx, symbol)

		// Calculate actual realized P&L from fill price
		realizedPnL := estimatedPnL // Default to estimated if we can't calculate
		if closeOrder != nil && closeOrder.AvgPrice > 0 && closeOrder.ExecutedQty > 0 {
			// For LONG: P&L = (ExitPrice - EntryPrice) * Quantity
			// For SHORT: P&L = (EntryPrice - ExitPrice) * Quantity
			if currentPos.PositionAmt > 0 { // Long position
				realizedPnL = (closeOrder.AvgPrice - currentPos.EntryPrice) * closeOrder.ExecutedQty
			} else { // Short position
				realizedPnL = (currentPos.EntryPrice - closeOrder.AvgPrice) * closeOrder.ExecutedQty
			}
			log.Printf("[%s][%s] Actual realized P&L: $%.2f (fill price: %.4f, entry: %.4f, qty: %.4f)",
				e.name, symbol, realizedPnL, closeOrder.AvgPrice, currentPos.EntryPrice, closeOrder.ExecutedQty)
		}

		// Return the realized PnL
		return realizedPnL, nil

	case "HOLD", "hold", "wait":
		log.Printf("[%s][%s] Holding - no action taken", e.name, symbol)

	default:
		log.Printf("[%s][%s] Unknown action: %s", e.name, symbol, decision.Action)
	}

	return 0, nil
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
		"trader_id":   e.id,
		"trader_name": e.name,
		"running":     e.running,
		"strategy":    strategyName,
		"pairs":       e.getTradingPairs(),
		"positions":   positions,
		"decisions":   decisions,
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
		"total_equity":   e.account.TotalMarginBalance,
		"wallet_balance": e.account.TotalWalletBalance,
		"available":      e.account.AvailableBalance,
		"unrealized_pnl": e.account.TotalUnrealizedProfit,
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
// NOTE: Only applies if the strategy explicitly sets these new ratio fields
func (e *Engine) enforcePositionValueRatio(positionSizeUSD, equity float64, symbol string) (float64, bool) {
	if e.strategy == nil {
		return positionSizeUSD, false
	}

	rc := e.strategy.Config.RiskControl
	var maxRatio float64

	if isBTCETH(symbol) {
		maxRatio = rc.BTCETHMaxPositionValueRatio
		// If not set (0), disable this check for backward compatibility
		if maxRatio <= 0 {
			return positionSizeUSD, false
		}
	} else {
		maxRatio = rc.AltcoinMaxPositionValueRatio
		// If not set (0), disable this check for backward compatibility
		if maxRatio <= 0 {
			return positionSizeUSD, false
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
			minSize = 10.0 // Default $10 for BTC/ETH (adjusted for small account)
		}
	} else {
		// Try new field first
		minSize = rc.MinPositionSize
		if minSize <= 0 {
			// Fallback to legacy MinPositionUSD
			minSize = rc.MinPositionUSD
		}
		if minSize <= 0 {
			minSize = 10.0 // Default $10 for altcoins
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

// validateRiskRewardRatioPct validates TP/SL percentage ratio meets minimum requirement
// Uses simple percentage comparison - TP% should be at least minRatio * SL%
func (e *Engine) validateRiskRewardRatioPct(slPct, tpPct float64) error {
	if e.strategy == nil {
		return nil
	}

	minRatio := e.strategy.Config.RiskControl.MinRiskRewardRatio
	if minRatio <= 0 {
		// Validation disabled - allow trade
		return nil
	}

	// Validate percentages are sensible
	if slPct <= 0 || slPct > 20 {
		log.Printf("[RiskReward] Skipping validation - invalid SL%%: %.2f", slPct)
		return nil
	}
	if tpPct <= 0 || tpPct > 50 {
		log.Printf("[RiskReward] Skipping validation - invalid TP%%: %.2f", tpPct)
		return nil
	}

	// Check ratio: tpPct / slPct should be >= minRatio
	ratio := tpPct / slPct
	if ratio < minRatio {
		return fmt.Errorf("risk-reward ratio %.2f:1 below minimum %.2f:1 (SL=%.1f%%, TP=%.1f%%)",
			ratio, minRatio, slPct, tpPct)
	}

	log.Printf("[RiskReward] Valid ratio %.2f:1 (SL=%.1f%%, TP=%.1f%%)", ratio, slPct, tpPct)
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

// syncTradeHistory fetches recent trades from Binance and saves them to the database
func (e *Engine) syncTradeHistory(ctx context.Context) {
	// Get last synced trade time
	lastTradeTime, err := e.tradeStore.GetLastTradeTime(e.id)
	if err != nil {
		log.Printf("[%s] Failed to get last trade time: %v", e.name, err)
		lastTradeTime = 0
	}

	// If no previous trades, start from 24 hours ago
	if lastTradeTime == 0 {
		lastTradeTime = time.Now().Add(-24 * time.Hour).UnixMilli()
	} else {
		// Add 1ms to avoid duplicates
		lastTradeTime++
	}

	// Get coins from strategy to fetch trades for each symbol
	coins := e.getTradingPairs()
	if len(coins) == 0 {
		return
	}

	var allTrades []*store.Trade
	for _, symbol := range coins {
		trades, err := e.binance.GetTradeHistory(ctx, symbol, lastTradeTime, 100)
		if err != nil {
			log.Printf("[%s] Failed to fetch trades for %s: %v", e.name, symbol, err)
			continue
		}

		for _, t := range trades {
			trade := &store.Trade{
				ID:          t.ID,
				TraderID:    e.id,
				Symbol:      t.Symbol,
				Side:        t.Side,
				Price:       t.Price,
				Quantity:    t.Qty,
				QuoteQty:    t.QuoteQty,
				RealizedPnL: t.RealizedPnL,
				Commission:  t.Commission,
				Timestamp:   time.UnixMilli(t.Time),
				OrderID:     t.OrderID,
			}
			allTrades = append(allTrades, trade)
		}
	}

	if len(allTrades) > 0 {
		if err := e.tradeStore.SaveBatch(allTrades); err != nil {
			log.Printf("[%s] Failed to save trades: %v", e.name, err)
		} else {
			log.Printf("[%s] Synced %d trades from Binance", e.name, len(allTrades))
		}
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
// Bracket Orders (SL/TP) Management
// =============================================================================

// getSLTPPercentages extracts stop-loss and take-profit percentages from decision
// Returns default values if not provided by AI
func (e *Engine) getSLTPPercentages(decision *ai.TradingDecision) (slPct, tpPct float64) {
	// Use new percentage fields
	slPct = decision.StopLossPct
	tpPct = decision.TakeProfitPct

	// Only apply defaults if values are missing or clearly invalid
	// Do NOT silently adjust R:R - let validateRiskRewardRatioPct reject bad ratios
	if slPct <= 0 {
		slPct = 2.0 // Default 2% stop loss only if not provided
		log.Printf("[SL/TP] No SL provided, using default %.1f%%", slPct)
	} else if slPct > 10 {
		log.Printf("[SL/TP] WARNING: SL=%.1f%% exceeds 10%%, trade will be validated", slPct)
	}

	if tpPct <= 0 {
		tpPct = 6.0 // Default 6% take profit only if not provided
		log.Printf("[SL/TP] No TP provided, using default %.1f%%", tpPct)
	} else if tpPct > 30 {
		log.Printf("[SL/TP] WARNING: TP=%.1f%% exceeds 30%%, trade will be validated", tpPct)
	}

	// DO NOT silently adjust R:R - this was hiding bad AI decisions
	// Let validateRiskRewardRatioPct properly REJECT trades with bad R:R
	ratio := tpPct / slPct
	if ratio < 1.0 {
		log.Printf("[SL/TP] WARNING: Poor R:R ratio %.2f:1 (SL=%.1f%%, TP=%.1f%%) - will be rejected by validator", ratio, slPct, tpPct)
	}

	return slPct, tpPct
}

// placeBracketOrders places SL/TP orders on Binance and tracks them
// CRITICAL: If this fails after retries, we close the position to prevent unprotected exposure
func (e *Engine) placeBracketOrders(ctx context.Context, symbol string, isLong bool, entryPrice, slPct, tpPct float64) {
	log.Printf("[%s][%s] Placing bracket orders: SL=%.1f%%, TP=%.1f%%, entry=$%.2f",
		e.name, symbol, slPct, tpPct, entryPrice)

	// CLEANUP: Cancel any existing open orders before placing new ones to avoid "order exists" errors (Code -4130)
	if err := e.binance.CancelAllOrders(ctx, symbol); err != nil {
		log.Printf("[%s][%s] Warning: failed to clear existing orders before brackets: %v", e.name, symbol, err)
	}

	// Retry up to 3 times
	var slOrder, tpOrder *exchange.Order
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		slOrder, tpOrder, err = e.binance.PlaceBracketOrders(ctx, symbol, isLong, entryPrice, slPct, tpPct)
		if err == nil {
			break
		}
		log.Printf("[%s][%s] Bracket order attempt %d failed: %v", e.name, symbol, attempt, err)
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * time.Second) // Exponential backoff
		}
	}

	if err != nil {
		// CRITICAL: Failed to place bracket orders after all retries
		// Instead of closing immediately (which can lock in losses during volatility),
		// try to place an emergency stop-loss with wider parameters
		log.Printf("[%s][%s] ⚠️ Bracket orders failed after 3 attempts. Trying emergency SL...", e.name, symbol)

		// Calculate emergency SL at wider level (1.5x the normal SL distance)
		emergencySLPct := slPct * 1.5
		if emergencySLPct > 10.0 {
			emergencySLPct = 10.0 // Cap at 10% to limit potential loss
		}

		closeSide := "SELL"
		var slPrice float64
		if isLong {
			slPrice = entryPrice * (1 - emergencySLPct/100)
		} else {
			closeSide = "BUY"
			slPrice = entryPrice * (1 + emergencySLPct/100)
		}

		slOrder, slErr := e.binance.PlaceStopLoss(ctx, symbol, closeSide, 0, slPrice)
		if slErr != nil {
			log.Printf("[%s][%s] 🔴 Emergency SL also failed: %v", e.name, symbol, slErr)
			log.Printf("[%s][%s] Position is UNPROTECTED! Software trailing stop will monitor.", e.name, symbol)
			// Don't close immediately - the software trailing stop (checkPositionDrawdown)
			// will monitor this position every 10 seconds and can close it if needed
			// This is safer than closing at potentially bad timing
		} else {
			log.Printf("[%s][%s] 🟡 Emergency SL placed at $%.2f (%.1f%% from entry)", e.name, symbol, slPrice, emergencySLPct)
			// Store the emergency SL for tracking
			e.bracketOrdersMutex.Lock()
			e.bracketOrders[symbol] = &BracketOrderIDs{
				StopLossOrderID:   slOrder.OrderID,
				TakeProfitOrderID: 0, // No TP
				EntryPrice:        entryPrice,
				StopLossPct:       emergencySLPct,
				TakeProfitPct:     0,
			}
			e.bracketOrdersMutex.Unlock()
		}
		return
	}

	// Store order IDs for tracking
	e.bracketOrdersMutex.Lock()
	e.bracketOrders[symbol] = &BracketOrderIDs{
		StopLossOrderID:   slOrder.OrderID,
		TakeProfitOrderID: tpOrder.OrderID,
		EntryPrice:        entryPrice,
		StopLossPct:       slPct,
		TakeProfitPct:     tpPct,
	}
	e.bracketOrdersMutex.Unlock()

	log.Printf("[%s][%s] Bracket orders placed: SL_ID=%d, TP_ID=%d",
		e.name, symbol, slOrder.OrderID, tpOrder.OrderID)
}

// placeStopLossOnly places ONLY a stop-loss order (no take-profit)
// Used when trailing stop is enabled - TSL handles profit-taking, exchange SL protects downside
func (e *Engine) placeStopLossOnly(ctx context.Context, symbol string, isLong bool, entryPrice, slPct float64) {
	log.Printf("[%s][%s] Placing SL only: SL=%.1f%%, entry=$%.2f (TSL will handle profits)",
		e.name, symbol, slPct, entryPrice)

	// CLEANUP: Cancel any existing open orders before placing new ones
	if err := e.binance.CancelAllOrders(ctx, symbol); err != nil {
		log.Printf("[%s][%s] Warning: failed to clear existing orders before SL: %v", e.name, symbol, err)
	}

	// Calculate SL price
	var slPrice float64
	var closeSide string
	if isLong {
		closeSide = "SELL"
		slPrice = entryPrice * (1 - slPct/100) // SL below entry for long
	} else {
		closeSide = "BUY"
		slPrice = entryPrice * (1 + slPct/100) // SL above entry for short
	}

	// Retry up to 3 times
	var slOrder *exchange.Order
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		slOrder, err = e.binance.PlaceStopLoss(ctx, symbol, closeSide, 0, slPrice)
		if err == nil {
			break
		}
		log.Printf("[%s][%s] SL order attempt %d failed: %v", e.name, symbol, attempt, err)
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}

	if err != nil {
		// CRITICAL: Failed to place SL after all retries - close position for safety
		log.Printf("[%s][%s] CRITICAL: Failed to place SL after 3 attempts. Closing position for safety!", e.name, symbol)
		positions, posErr := e.binance.GetPositions(ctx)
		if posErr != nil {
			log.Printf("[%s][%s] ERROR: Cannot get positions to close: %v", e.name, symbol, posErr)
			return
		}
		for _, pos := range positions {
			if pos.Symbol == symbol && pos.PositionAmt != 0 {
				if _, closeErr := e.binance.ClosePosition(ctx, symbol, pos.PositionAmt); closeErr != nil {
					log.Printf("[%s][%s] ERROR: Failed to close unprotected position: %v", e.name, symbol, closeErr)
				} else {
					log.Printf("[%s][%s] Closed unprotected position for safety", e.name, symbol)
				}
				break
			}
		}
		return
	}

	// Store SL order ID for tracking (no TP)
	e.bracketOrdersMutex.Lock()
	e.bracketOrders[symbol] = &BracketOrderIDs{
		StopLossOrderID:   slOrder.OrderID,
		TakeProfitOrderID: 0, // No TP when TSL is enabled
		EntryPrice:        entryPrice,
		StopLossPct:       slPct,
		TakeProfitPct:     0,
	}
	e.bracketOrdersMutex.Unlock()

	log.Printf("[%s][%s] SL order placed: SL_ID=%d (TSL will handle profits)",
		e.name, symbol, slOrder.OrderID)
}

// cancelOrphanedOrders cancels any orphaned SL/TP orders for a symbol before opening a new position
// This prevents the "-4130: An open stop or take profit order...is existing" error
func (e *Engine) cancelOrphanedOrders(ctx context.Context, symbol string) {
	// First, cancel any tracked bracket orders
	e.bracketOrdersMutex.Lock()
	bracket, exists := e.bracketOrders[symbol]
	if exists {
		delete(e.bracketOrders, symbol)
	}
	e.bracketOrdersMutex.Unlock()

	if exists {
		log.Printf("[%s][%s] 🧹 Cleaning up tracked bracket orders before new position", e.name, symbol)
		if bracket.StopLossOrderID > 0 {
			if err := e.binance.CancelOrder(ctx, symbol, bracket.StopLossOrderID); err != nil {
				// Ignore errors - order might already be filled/cancelled
			}
		}
		if bracket.TakeProfitOrderID > 0 {
			if err := e.binance.CancelOrder(ctx, symbol, bracket.TakeProfitOrderID); err != nil {
				// Ignore errors
			}
		}
	}

	// ALSO cancel ALL open algo orders for this symbol from Binance directly
	// This catches any orphaned orders that our tracking missed
	if err := e.binance.CancelAllOrders(ctx, symbol); err != nil {
		// This is expected to fail sometimes (no orders), ignore
		log.Printf("[%s][%s] 🧹 Attempted to cancel all open orders: %v", e.name, symbol, err)
	} else {
		log.Printf("[%s][%s] 🧹 Cancelled all open orders for fresh start", e.name, symbol)
	}
}

// cancelBracketOrders cancels any existing SL/TP orders for a symbol
func (e *Engine) cancelBracketOrders(ctx context.Context, symbol string) {
	e.bracketOrdersMutex.Lock()
	bracket, exists := e.bracketOrders[symbol]
	if exists {
		delete(e.bracketOrders, symbol)
	}
	e.bracketOrdersMutex.Unlock()

	if !exists {
		return
	}

	log.Printf("[%s][%s] Cancelling bracket orders: SL_ID=%d, TP_ID=%d",
		e.name, symbol, bracket.StopLossOrderID, bracket.TakeProfitOrderID)

	slFilled := false
	tpFilled := false

	// Cancel SL order (using CancelAlgoOrder since SL/TP are algo orders)
	if bracket.StopLossOrderID > 0 {
		if err := e.binance.CancelAlgoOrder(ctx, symbol, bracket.StopLossOrderID); err != nil {
			errStr := err.Error()
			// Check for "order not found" or already filled/cancelled
			if strings.Contains(errStr, "Unknown order") || strings.Contains(errStr, "-2011") ||
				strings.Contains(errStr, "Order does not exist") || strings.Contains(errStr, "-20123") {
				slFilled = true
				log.Printf("[%s][%s] 🔴 SL order was ALREADY FILLED/CANCELLED by exchange (Algo ID: %d)",
					e.name, symbol, bracket.StopLossOrderID)
			} else {
				log.Printf("[%s][%s] SL cancel error: %v", e.name, symbol, err)
			}
		} else {
			log.Printf("[%s][%s] ✅ SL algo order cancelled successfully", e.name, symbol)
		}
	}

	// Cancel TP order (using CancelAlgoOrder since SL/TP are algo orders)
	if bracket.TakeProfitOrderID > 0 {
		if err := e.binance.CancelAlgoOrder(ctx, symbol, bracket.TakeProfitOrderID); err != nil {
			errStr := err.Error()
			// Check for "order not found" or already filled/cancelled
			if strings.Contains(errStr, "Unknown order") || strings.Contains(errStr, "-2011") ||
				strings.Contains(errStr, "Order does not exist") || strings.Contains(errStr, "-20123") {
				tpFilled = true
				log.Printf("[%s][%s] 🟢 TP order was ALREADY FILLED/CANCELLED by exchange (Algo ID: %d)",
					e.name, symbol, bracket.TakeProfitOrderID)
			} else {
				log.Printf("[%s][%s] TP cancel error: %v", e.name, symbol, err)
			}
		} else {
			log.Printf("[%s][%s] ✅ TP algo order cancelled successfully", e.name, symbol)
		}
	}

	// Summary
	if slFilled && !tpFilled {
		log.Printf("[%s][%s] ⚠️ STOP LOSS HIT: Position was closed by exchange SL order",
			e.name, symbol)
	} else if tpFilled && !slFilled {
		log.Printf("[%s][%s] 🎯 TAKE PROFIT HIT: Position closed at target profit",
			e.name, symbol)
	} else if slFilled && tpFilled {
		log.Printf("[%s][%s] ⚠️ Both SL and TP orders were filled (unusual - check order history)",
			e.name, symbol)
	}
}

// GetBracketOrders returns the current bracket orders for all symbols
func (e *Engine) GetBracketOrders() map[string]*BracketOrderIDs {
	e.bracketOrdersMutex.RLock()
	defer e.bracketOrdersMutex.RUnlock()

	result := make(map[string]*BracketOrderIDs)
	for k, v := range e.bracketOrders {
		result[k] = v
	}
	return result
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
		currentBalance = e.account.TotalMarginBalance // Use TotalMarginBalance directly (same as wallet + unrealized)
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
func (e *Engine) triggerTradingPause(ctx context.Context) {
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

	log.Printf("[%s] 🛑 Trading paused until %s due to daily loss limit", e.name, e.stopUntil.Format(time.RFC3339))

	// Check if we should close all positions
	if e.strategy.Config.RiskControl.ClosePositionsOnDailyLoss {
		log.Printf("[%s] 🔴 CLOSING ALL POSITIONS due to daily loss limit...", e.name)
		e.closeAllPositions(ctx, "daily loss limit reached")
	}
}

// closeAllPositions closes all open positions
func (e *Engine) closeAllPositions(ctx context.Context, reason string) {
	e.mu.RLock()
	positions := make([]*exchange.Position, 0)
	for _, pos := range e.positions {
		if pos.PositionAmt != 0 {
			positions = append(positions, pos)
		}
	}
	e.mu.RUnlock()

	if len(positions) == 0 {
		log.Printf("[%s] No open positions to close", e.name)
		return
	}

	log.Printf("[%s] Closing %d position(s): %s", e.name, len(positions), reason)

	for _, pos := range positions {
		side := "LONG"
		if pos.PositionAmt < 0 {
			side = "SHORT"
		}

		log.Printf("[%s][%s] Closing %s position: %.4f (reason: %s)",
			e.name, pos.Symbol, side, pos.PositionAmt, reason)

		if _, err := e.binance.ClosePosition(ctx, pos.Symbol, pos.PositionAmt); err != nil {
			log.Printf("[%s][%s] Failed to close position: %v", e.name, pos.Symbol, err)
		} else {
			log.Printf("[%s][%s] ✅ Position closed successfully", e.name, pos.Symbol)
			e.clearPositionTracking(pos.Symbol, side)
			e.cancelBracketOrders(ctx, pos.Symbol)
		}
	}
}

// resetDailyPnLIfNeeded resets daily P&L tracking at the start of a new day
func (e *Engine) resetDailyPnLIfNeeded() {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if 24 hours have passed since last reset
	if time.Since(e.lastResetTime) >= 24*time.Hour {
		if e.account != nil {
			e.initialBalance = e.account.TotalMarginBalance // Use TotalMarginBalance (includes unrealized P&L)
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

// lastRiskSettingsLog tracks when we last logged risk settings (for rate limiting)
var lastRiskSettingsLog = make(map[string]time.Time)
var lastRiskSettingsLogMu sync.Mutex

// logActiveRiskSettings logs which risk management features are active (rate limited to once per minute)
func (e *Engine) logActiveRiskSettings(rc store.RiskControlConfig) {
	lastRiskSettingsLogMu.Lock()
	defer lastRiskSettingsLogMu.Unlock()

	// Only log once per minute per engine
	if lastLog, exists := lastRiskSettingsLog[e.id]; exists && time.Since(lastLog) < time.Minute {
		return
	}
	lastRiskSettingsLog[e.id] = time.Now()

	var features []string
	if rc.EnableTrailingStop {
		features = append(features, fmt.Sprintf("TrailingStop(activate=%.1f%%, dist=%.1f%%)", rc.TrailingStopActivatePct, rc.TrailingStopDistancePct))
	}
	if rc.EnableMaxHoldDuration {
		features = append(features, fmt.Sprintf("MaxHold(%dm)", rc.MaxHoldDurationMins))
	}
	if rc.EnableSmartLossCut {
		features = append(features, fmt.Sprintf("SmartLossCut(%dm, %.1f%%)", rc.SmartLossCutMins, rc.SmartLossCutPct))
	}
	if rc.EnableEmergencyShutdown {
		features = append(features, fmt.Sprintf("EmergencyShutdown($%.0f)", rc.EmergencyMinBalance))
	}

	if len(features) > 0 {
		log.Printf("[%s] ⚙️ Active Risk Features: %s", e.name, strings.Join(features, ", "))
	} else {
		log.Printf("[%s] ⚙️ No advanced risk features enabled (using exchange SL/TP only)", e.name)
	}
}

// checkPositionDrawdown checks if any positions should be closed due to drawdown
func (e *Engine) checkPositionDrawdown(ctx context.Context) {
	if e.strategy == nil {
		return
	}

	rc := e.strategy.Config.RiskControl
	isSimpleMode := e.strategy.Config.SimpleMode

	// Log active features once per minute (avoid spam)
	e.logActiveRiskSettings(rc)

	// SIMPLE MODE affects only automatic drawdown protection
	// Explicitly enabled features (Trailing Stop, Max Hold, Smart Loss Cut) still work
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

		// Calculate current P&L % (raw price movement, NOT leveraged)
		// This matches the CLOSE action calculation and SL/TP percentages
		// Note: The dollar P&L already reflects leverage, but percentage thresholds
		// like noise zone (-2% to +8%) should be based on price movement
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

		holdDuration := e.GetHoldDuration(pos.Symbol, side)

		// =====================================================================
		// 1. TRAILING STOP LOSS - Lock in profits
		// =====================================================================
		if rc.EnableTrailingStop {
			// activatePct: Set to 0 to activate immediately from entry (aggressive)
			// Set to positive value (e.g., 1.0) to only activate after reaching that profit %
			activatePct := rc.TrailingStopActivatePct
			// NOTE: We don't override activatePct <= 0 anymore - 0 means immediate activation
			trailDistPct := rc.TrailingStopDistancePct
			if trailDistPct <= 0 {
				trailDistPct = 0.5
			}

			// Update and get peak P&L
			e.UpdatePeakPnL(pos.Symbol, side, pnlPct)
			peakPnL := e.GetPeakPnL(pos.Symbol, side)

			// Check if trailing stop should activate
			// If activatePct is 0 or negative, activate immediately from entry
			if activatePct <= 0 || peakPnL >= activatePct {
				// Calculate trailing stop level
				trailingStopLevel := peakPnL - trailDistPct

				if pnlPct <= trailingStopLevel {
					log.Printf("[%s][%s] 📉 TRAILING STOP TRIGGERED: Peak=%.2f%%, Current=%.2f%%, TrailStop=%.2f%%",
						e.name, pos.Symbol, peakPnL, pnlPct, trailingStopLevel)

					if _, err := e.binance.ClosePosition(ctx, pos.Symbol, pos.PositionAmt); err != nil {
						log.Printf("[%s][%s] Failed to close position (trailing stop): %v", e.name, pos.Symbol, err)
					} else {
						log.Printf("[%s][%s] ✅ Closed position via trailing stop. Realized profit locked in.", e.name, pos.Symbol)
						e.clearPositionTracking(pos.Symbol, side)
						e.cancelBracketOrders(ctx, pos.Symbol)
					}
					continue // Move to next position
				}
			}
		}

		// =====================================================================
		// 2. MAX HOLD DURATION - Force close positions held too long
		// =====================================================================
		if rc.EnableMaxHoldDuration {
			maxHoldMins := rc.MaxHoldDurationMins
			if maxHoldMins <= 0 {
				maxHoldMins = 240 // Default 4 hours
			}

			maxHoldDuration := time.Duration(maxHoldMins) * time.Minute

			if holdDuration >= maxHoldDuration {
				log.Printf("[%s][%s] ⏰ MAX HOLD DURATION EXCEEDED: Held for %v (limit: %v). Force closing.",
					e.name, pos.Symbol, holdDuration.Round(time.Minute), maxHoldDuration)

				if _, err := e.binance.ClosePosition(ctx, pos.Symbol, pos.PositionAmt); err != nil {
					log.Printf("[%s][%s] Failed to close position (max hold): %v", e.name, pos.Symbol, err)
				} else {
					log.Printf("[%s][%s] ✅ Closed position due to max hold duration. PnL: %.2f%%", e.name, pos.Symbol, pnlPct)
					e.clearPositionTracking(pos.Symbol, side)
					e.cancelBracketOrders(ctx, pos.Symbol)
				}
				continue // Move to next position
			}
		}

		// =====================================================================
		// 3. SMART LOSS CUT - Cut positions underwater too long
		// =====================================================================
		if rc.EnableSmartLossCut {
			smartLossMins := rc.SmartLossCutMins
			if smartLossMins <= 0 {
				smartLossMins = 30 // Default 30 mins
			}
			smartLossPct := rc.SmartLossCutPct
			if smartLossPct >= 0 {
				smartLossPct = -1.0 // Default -1%
			}

			smartLossDuration := time.Duration(smartLossMins) * time.Minute

			// Only trigger if both conditions are met: underwater AND held long enough
			if pnlPct <= smartLossPct && holdDuration >= smartLossDuration {
				log.Printf("[%s][%s] 🔪 SMART LOSS CUT: Position at %.2f%% (threshold: %.2f%%) for %v (threshold: %v). Cutting losses.",
					e.name, pos.Symbol, pnlPct, smartLossPct, holdDuration.Round(time.Minute), smartLossDuration)

				if _, err := e.binance.ClosePosition(ctx, pos.Symbol, pos.PositionAmt); err != nil {
					log.Printf("[%s][%s] Failed to close position (smart loss cut): %v", e.name, pos.Symbol, err)
				} else {
					log.Printf("[%s][%s] ✅ Cut losing position. Loss: %.2f%%", e.name, pos.Symbol, pnlPct)
					e.clearPositionTracking(pos.Symbol, side)
					e.cancelBracketOrders(ctx, pos.Symbol)
				}
				continue // Move to next position
			}
		}

		// =====================================================================
		// 4. DRAWDOWN PROTECTION (Original logic) - SKIPPED in Simple Mode
		// =====================================================================
		// In Simple Mode, we skip ONLY this automatic drawdown protection
		// (features 1-3 above are explicitly enabled by user, so they still run)
		if isSimpleMode {
			continue
		}

		// Update peak P&L
		e.UpdatePeakPnL(pos.Symbol, side, pnlPct)
		peakPnL := e.GetPeakPnL(pos.Symbol, side)

		// Only apply drawdown protection if we were profitable
		if peakPnL < minProfitForDrawdown {
			continue
		}

		// Calculate drawdown from peak (relative percentage, matching NOFX)
		var drawdownPct float64
		if peakPnL > 0 && pnlPct < peakPnL {
			drawdownPct = ((peakPnL - pnlPct) / peakPnL) * 100
		}

		if drawdownPct >= drawdownThreshold {
			log.Printf("[%s][%s] Drawdown alert: Peak=%.2f%%, Current=%.2f%%, Drawdown=%.2f%% >= %.2f%%",
				e.name, pos.Symbol, peakPnL, pnlPct, drawdownPct, drawdownThreshold)

			// Close the position
			log.Printf("[%s][%s] Closing position due to drawdown protection", e.name, pos.Symbol)
			if _, err := e.binance.ClosePosition(ctx, pos.Symbol, pos.PositionAmt); err != nil {
				log.Printf("[%s][%s] Failed to close position: %v", e.name, pos.Symbol, err)
			} else {
				e.clearPositionTracking(pos.Symbol, side)
				e.cancelBracketOrders(ctx, pos.Symbol)
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

				// Clear tracking data
				key := getPositionKey(symbol, side)
				delete(e.positionFirstSeenTime, key)

				// Clear peak P&L synchronously to prevent race condition
				// where a new position could inherit stale peak P&L
				e.ClearPeakPnL(symbol, side)
			}
		}
	}

	// MERGE positions instead of replacing to prevent race conditions:
	// If executeTrade() just opened a position and added it to e.positions,
	// we don't want to lose that update. Binance data is authoritative for
	// existing positions, but we preserve local state for positions not yet
	// visible on exchange (due to API latency).
	for symbol, newPos := range newPositions {
		e.positions[symbol] = newPos
	}
	// NOTE: We don't remove positions that aren't in newPositions here.
	// A locally-opened position might not be visible on Binance yet due to
	// API latency. The trading cycle's fresh fetch at the start handles
	// authoritative position state. This sync is for background updates only.

	// Update account info
	account, err := e.binance.GetAccountInfo(ctx)
	if err == nil {
		e.account = account
	}
}
