package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"auto-trader-ahh/ai"
	"auto-trader-ahh/config"
	"auto-trader-ahh/decision"
	"auto-trader-ahh/events"
	"auto-trader-ahh/exchange"
	"auto-trader-ahh/intel"
	"auto-trader-ahh/market"
	"auto-trader-ahh/mcp"
	"auto-trader-ahh/provider/coinank"
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
	coinank      *coinank.Client
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
	settingsStore *store.SettingsStore // For persisting daily loss state
	positionStore *store.PositionStore // For historical position data

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

	// Smart Find Auto-Refresh
	lastSmartFindRefresh time.Time

	// Market Intelligence Provider (free external data)
	intelProvider *intel.Provider

	// OI Analysis Cache (to avoid rate limits: 1000 req/5min)
	oiCache      map[string]*oiCacheEntry // key: symbol -> cached OI data
	oiCacheMutex sync.RWMutex

	// WebSocket position updates (real-time, <1s latency)
	wsUpdateCh <-chan *exchange.PositionUpdate

	// Risk check optimization (prevent concurrent checks and rate limit close attempts)
	isCheckingDrawdown atomic.Bool          // Atomic flag to prevent concurrent drawdown checks
	lastCloseAttempt   map[string]time.Time // Track last close attempt per symbol
	lastLogTime        map[string]time.Time // Track last log time per symbol (for throttling status logs)
	closeAttemptMu     sync.Mutex           // Mutex for lastCloseAttempt and lastLogTime maps

	// WebSocket mark price logging (for debugging)
	lastMarkPriceLog time.Time // Throttle mark price logs to avoid spam
}

// oiCacheEntry stores cached OI data with expiry
type oiCacheEntry struct {
	Analysis      *exchange.OIAnalysis
	SentimentData *exchange.LongShortAnalysis
	ExpiresAt     time.Time
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

// EntryWarning represents a non-fatal safety check warning with a confidence penalty
// Instead of hard-blocking trades, warnings reduce confidence. Trade executes if final confidence >= minConfidence
type EntryWarning struct {
	Filter   string  // Name of the filter that triggered this warning
	Message  string  // Human-readable warning message
	Penalty  float64 // Confidence deduction (e.g., 10 means -10% confidence)
	Severity string  // "low", "medium", "high" for logging
}

// EntryCheckResult contains all warnings and any critical errors from entry safety checks
type EntryCheckResult struct {
	Warnings     []EntryWarning
	CriticalErr  error   // Non-nil only for truly dangerous situations (e.g., liquidation cascade)
	TotalPenalty float64 // Sum of all warning penalties
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

	// Initialize market intelligence provider with caching
	intelCfg := intel.DefaultConfig()
	// Enable LunarCrush if API key is set
	if lunarCrushKey := os.Getenv("LUNARCRUSH_API_KEY"); lunarCrushKey != "" {
		intelCfg.LunarCrushAPIKey = lunarCrushKey
		intelCfg.EnableLunarCrush = true
		log.Printf("[Intel] LunarCrush social sentiment enabled")
	}
	intelProvider := intel.NewProvider(intelCfg)

	// Initialize CoinAnk client
	var coinAnkClient *coinank.Client
	if apiKey := cfg.CoinAnkAPIKey; apiKey != "" {
		coinAnkClient = coinank.NewClient(apiKey)
		log.Printf("[CoinAnk] Client initialized with API key")
	} else {
		log.Printf("[CoinAnk] Warning: COINANK_API_KEY not set. OI-Ranking features will be disabled.")
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
		coinank:        coinAnkClient,
		mcpClient:      mcpClient,
		decisionEngine: decisionEngine,
		startTime:      time.Now(),
		stopCh:         make(chan struct{}),
		lastDecisions:  make(map[string]*ai.TradingDecision),
		positions:      make(map[string]*exchange.Position),
		decisionStore:  store.NewDecisionStore(),
		equityStore:    store.NewEquityStore(),
		tradeStore:     store.NewTradeStore(),
		settingsStore:  store.NewSettingsStore(),
		positionStore:  store.NewPositionStore(),

		// Initialize position management maps
		peakPnLCache:          make(map[string]float64),
		positionFirstSeenTime: make(map[string]int64),

		// Initialize bracket orders tracking
		bracketOrders: make(map[string]*BracketOrderIDs),

		// Initialize daily tracking
		lastResetTime:  time.Now(),
		initialBalance: 0,
		notifier:       notifier,

		// Market Intelligence
		intelProvider: intelProvider,

		// OI Cache (1 minute TTL to stay well under rate limits)
		oiCache: make(map[string]*oiCacheEntry),

		// Risk check optimization
		// Risk check optimization
		lastCloseAttempt: make(map[string]time.Time),
		lastLogTime:      make(map[string]time.Time),
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

	// Load persisted daily loss state (critical for surviving restarts)
	savedState, err := e.settingsStore.GetDailyLossState(e.id)
	if err != nil {
		log.Printf("[%s] Warning: failed to load daily loss state: %v", e.name, err)
	}

	if savedState != nil && time.Since(savedState.LastResetTime) < 24*time.Hour {
		// Restore saved state (within 24h window)
		e.initialBalance = savedState.InitialBalance
		e.lastResetTime = savedState.LastResetTime
		e.stopUntil = savedState.StopUntil

		if !e.stopUntil.IsZero() && time.Now().Before(e.stopUntil) {
			log.Printf("[%s] 🛑 Restored daily loss pause until %s (surviving restart)", e.name, e.stopUntil.Format(time.RFC3339))
		}
		log.Printf("[%s] Restored daily loss tracking: initial=$%.2f, last_reset=%s",
			e.name, e.initialBalance, e.lastResetTime.Format(time.RFC3339))
	} else {
		// No saved state or 24h passed - start fresh
		e.initialBalance = account.TotalMarginBalance
		e.lastResetTime = time.Now()
		e.stopUntil = time.Time{}

		// Persist the new state
		e.saveDailyLossState()
		log.Printf("[%s] Starting fresh daily loss tracking: initial=$%.2f", e.name, e.initialBalance)
	}

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

	// Start WebSocket User Data Stream for real-time position updates
	// IMPORTANT: Must start BEFORE tradingLoop to ensure mark price subscriptions work
	if err := e.binance.StartUserDataStream(ctx); err != nil {
		log.Printf("[%s] Warning: Failed to start WebSocket, using REST fallback: %v", e.name, err)
	} else {
		e.wsUpdateCh = e.binance.WsUpdateCh
		go e.handleWebSocketUpdates(ctx)
		log.Printf("[%s] WebSocket position updates enabled (<1s latency)", e.name)

		// Start Market Data Stream for real-time price ticks
		// This feeds handleWebSocketUpdates with MARK_PRICE ticks for active positions
		if err := e.binance.StartMarketDataStream(ctx); err != nil {
			log.Printf("[%s] Warning: Failed to start Market Data Stream: %v", e.name, err)
		} else {
			log.Printf("[%s] Market Data Stream enabled (real-time PnL tracking)", e.name)
		}
	}

	// Start background goroutines AFTER WebSocket is connected
	// This ensures mark price subscriptions work on first trading cycle
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

	// Close Binance client to stop WebSocket streams
	if e.binance != nil {
		e.binance.Close()
	}
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
	newSmartFind := strategy.Config.SmartFindAutoRefresh

	if oldSimpleMode != newSimpleMode {
		log.Printf("[%s] Strategy updated: SimpleMode changed from %v to %v", e.name, oldSimpleMode, newSimpleMode)
	}
	if oldTrailingStop != newTrailingStop {
		log.Printf("[%s] Strategy updated: TrailingStop changed from %v to %v", e.name, oldTrailingStop, newTrailingStop)
	}
	// Always log Smart Find status on reload to be explicit
	log.Printf("[%s] Strategy updated: Smart Find Auto-Refresh is now %v", e.name, newSmartFind)

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
		if sourceType == "volume_top" || sourceType == "top_volume" || sourceType == "oi_top" || sourceType == "dynamic" {
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

// maybeRefreshSmartFind checks if it's time to auto-refresh Smart Find coins
// and updates the strategy's static_coins if needed.
// This analyzes open positions first, then finds new risky symbols.
func (e *Engine) maybeRefreshSmartFind(ctx context.Context) {
	if e.strategy == nil {
		return
	}

	// Check if Smart Find auto-refresh is enabled
	if !e.strategy.Config.SmartFindAutoRefresh {
		return
	}

	// Get refresh interval (default 60 mins)
	refreshMins := e.strategy.Config.SmartFindRefreshMins
	if refreshMins <= 0 {
		refreshMins = 60
	}

	// Check if enough time has passed
	if time.Since(e.lastSmartFindRefresh) < time.Duration(refreshMins)*time.Minute {
		return
	}

	log.Printf("[%s] 🔍 Smart Find Auto-Refresh triggered (interval: %d mins)", e.name, refreshMins)

	// Get max positions to calculate target count (2x max positions)
	maxPositions := 3
	if e.strategy.Config.RiskControl.MaxPositions > 0 {
		maxPositions = e.strategy.Config.RiskControl.MaxPositions
	}
	targetCount := maxPositions * 2

	// Perform Smart Find
	newCoins, err := e.runSmartFind(ctx, targetCount)
	if err != nil {
		log.Printf("[%s] ⚠️ Smart Find Auto-Refresh failed: %v", e.name, err)
		return
	}

	// Update strategy's static coins
	e.mu.Lock()
	e.strategy.Config.CoinSource.StaticCoins = newCoins
	e.strategy.Config.CoinSource.SourceType = "static"
	e.lastSmartFindRefresh = time.Now()
	e.mu.Unlock()

	log.Printf("[%s] ✅ Smart Find Auto-Refresh complete. New coins: %v", e.name, newCoins)
}

// runSmartFind finds risky symbols using AI analysis
func (e *Engine) runSmartFind(ctx context.Context, targetCount int) ([]string, error) {
	// Prepare auto-avoid list
	avoidSet := make(map[string]bool)
	if e.strategy != nil && e.strategy.Config.RiskControl.EnableAutoAvoidWorstSymbols {
		minLoss := e.strategy.Config.RiskControl.AutoAvoidMinLoss24h
		if minLoss <= 0 {
			minLoss = 5.0
		}
		worst, _ := e.positionStore.GetWorstSymbols24h(e.id, minLoss)
		minTrades := e.strategy.Config.RiskControl.AutoAvoidMinTrades24h
		if minTrades <= 0 {
			minTrades = 2
		}
		for _, w := range worst {
			if w.TradeCount >= minTrades {
				avoidSet[w.Symbol] = true
				log.Printf("[%s] SmartFind avoiding %s (loss > $%.2f)", e.name, w.Symbol, minLoss)
			}
		}
	}

	type MarketCoin struct {
		Symbol       string
		PriceChange  float64
		Volume       float64
		QuoteVolume  float64
		OIChange     float64 // Smart Find OI feature
		OpenInterest float64
	}

	var candidates []MarketCoin
	var sourceDesc string

	// Check if using OI-based discovery
	useOI := e.strategy != nil && e.strategy.Config.SmartFindUseOI
	foundOI := false

	if useOI && e.coinank != nil {
		// --- OI Discovery Mode (CoinAnk) ---
		filter := e.strategy.Config.SmartFindFilter
		if filter == "" {
			filter = "volatility"
		}
		sourceDesc = fmt.Sprintf("Top 30 pairs by %s", filter)

		limit := 40
		var items []coinank.OIRankItem
		var err error

		switch filter {
		case "volume":
			items, err = e.coinank.VolumeRank(ctx, limit)
		case "price", "volatility":
			items, err = e.coinank.PriceRank(ctx, limit)
		case "oi_change":
			items, err = e.coinank.GetOIRanking(ctx, limit)
		default:
			items, err = e.coinank.PriceRank(ctx, limit)
		}

		if err != nil {
			log.Printf("[%s] CoinAnk ranking failed, trying Binance fallback: %v", e.name, err)
		} else {
			for _, item := range items {
				// Filter avoided symbols
				if avoidSet[item.Symbol] {
					continue
				}

				candidates = append(candidates, MarketCoin{
					Symbol:       item.Symbol,
					PriceChange:  item.PriceChange24H,
					Volume:       item.Volume24H, // Note: response turnover24h is QuoteVolume
					QuoteVolume:  item.Volume24H,
					OIChange:     item.OIChange24H,
					OpenInterest: item.OpenInterest,
				})
			}
			foundOI = true
		}
	}

	if useOI && !foundOI {
		// --- OI Discovery Mode (Binance Free Fallback) ---
		sourceDesc = "Top OI Change pairs (Binance Scan)"
		log.Printf("[%s] Using Binance OI Scan (Free Fallback)", e.name)

		// 1. Get Tickers just to find detailed candidates
		tickers, err := e.binance.Get24hTicker(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch market data: %w", err)
		}

		// 2. Filter Top 50 by Liquidity (QuoteVolume) to find relevant OI
		// We can't scan all 300 symbols efficiently without hitting limits, so we check top liquid ones.
		var potential []exchange.Ticker24h
		for _, t := range tickers {
			if len(t.Symbol) > 4 && t.Symbol[len(t.Symbol)-4:] == "USDT" {
				if !e.binance.IsActiveSymbol(t.Symbol) {
					continue
				}
				if avoidSet[t.Symbol] {
					continue
				}
				if t.Symbol == "USDCUSDT" || t.Symbol == "FDUSDUSDT" || t.Symbol == "TUSDUSDT" {
					continue
				}
				if t.QuoteVolume > 1000000 { // >1M volume
					potential = append(potential, t)
				}
			}
		}

		// Sort by Volume DESC
		sort.Slice(potential, func(i, j int) bool {
			return potential[i].QuoteVolume > potential[j].QuoteVolume
		})
		// Take top 50
		if len(potential) > 50 {
			potential = potential[:50]
		}

		// 3. Fetch OI Hist for each (Concurrency controlled)
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 5) // Limit concurrency to 5

		for _, p := range potential {
			wg.Add(1)
			go func(sym string, priceChange, vol float64) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				// Get 4h OI Change using Binance Hist (Top-tier only)
				// Weight: 0 (from docs above?) No, usually weighted. Using openInterestHist endpoint.
				// Requesting 1h period, limit 4 (Last 4 hours)
				hist, err := e.binance.GetOpenInterestHist(ctx, sym, "1h", 4)
				if err == nil && len(hist) > 0 {
					latest := hist[len(hist)-1].SumOpenInterestValue
					oldest := hist[0].SumOpenInterestValue
					if oldest > 0 {
						change := ((latest - oldest) / oldest) * 100

						mu.Lock()
						candidates = append(candidates, MarketCoin{
							Symbol:       sym,
							PriceChange:  priceChange,
							Volume:       vol,
							QuoteVolume:  vol,
							OIChange:     change,
							OpenInterest: latest,
						})
						mu.Unlock()
					}
				}
			}(p.Symbol, p.PriceChange, p.QuoteVolume)
		}
		wg.Wait()

		// 4. Sort by OI Change Magnitude (find big moves)
		sort.Slice(candidates, func(i, j int) bool {
			valI := candidates[i].OIChange
			if valI < 0 {
				valI = -valI
			}
			valJ := candidates[j].OIChange
			if valJ < 0 {
				valJ = -valJ
			}
			return valI > valJ
		})

		foundOI = true
	}

	if !useOI && !foundOI {
		// --- Standard Binance Mode ---
		sourceDesc = "Top 30 pairs by Volatility (Price Change)"
		tickers, err := e.binance.Get24hTicker(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch market data: %w", err)
		}

		for _, t := range tickers {
			// Basic filter: USDT pairs, reasonable volume
			if len(t.Symbol) > 4 && t.Symbol[len(t.Symbol)-4:] == "USDT" {
				if !e.binance.IsActiveSymbol(t.Symbol) {
					continue
				}
				if avoidSet[t.Symbol] {
					continue
				}
				if t.Symbol == "USDCUSDT" || t.Symbol == "FDUSDUSDT" || t.Symbol == "TUSDUSDT" || t.Symbol == "USDPUSDT" {
					continue
				}
				if t.QuoteVolume > 500000 {
					candidates = append(candidates, MarketCoin{
						Symbol:      t.Symbol,
						PriceChange: t.PriceChange,
						Volume:      t.Volume,
						QuoteVolume: t.QuoteVolume,
					})
				}
			}
		}

		// Sort by Volatility
		sort.Slice(candidates, func(i, j int) bool {
			absI := candidates[i].PriceChange
			if absI < 0 {
				absI = -absI
			}
			absJ := candidates[j].PriceChange
			if absJ < 0 {
				absJ = -absJ
			}
			return absI > absJ
		})
	}

	if len(candidates) > 30 {
		candidates = candidates[:30]
	}

	// Get account info for balance context
	account, err := e.binance.GetAccountInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch account info: %w", err)
	}

	var prompt string
	isTurbo := e.strategy != nil && e.strategy.Config.TurboMode

	if isTurbo {
		// TURBO MODE
		prompt = fmt.Sprintf(`You are a HIGH RISK crypto degen trader.
My current balance: $%.2f
Objective: Find the %d MOST EXPLOSIVE trading pairs for aggressive scalping.
Criteria: High Volatility, Momentum, Meme Coins. Ignore safety.

Here are the %s:
`, account.TotalWalletBalance, targetCount, sourceDesc)
	} else {
		// STANDARD MODE
		prompt = fmt.Sprintf(`You are a crypto trading expert.
My current balance: $%.2f
Objective: Find the %d best trading pairs with good momentum for scalping/day-trading.
Criteria: Significant price movement, clear trends, and reasonable volume.

Here are the %s:
`, account.TotalWalletBalance, targetCount, sourceDesc)
	}

	for _, c := range candidates {
		if c.OIChange != 0 {
			prompt += fmt.Sprintf("- %s: Vol=$%.0fM, Chg=%.2f%%, OI Chg=%.2f%%\n", c.Symbol, c.QuoteVolume/1000000, c.PriceChange, c.OIChange)
		} else {
			prompt += fmt.Sprintf("- %s: Vol=$%.0fM, Chg=%.2f%%\n", c.Symbol, c.QuoteVolume/1000000, c.PriceChange)
		}
	}

	prompt += fmt.Sprintf(`
RESPONSE FORMAT: Return ONLY a raw JSON array of %d symbol strings. No markdown, no code blocks, no explanation.
Example: ["BTCUSDT","ETHUSDT","SOLUSDT"]
Your response:`, targetCount)

	// 5. Call AI
	// Use :online model for Smart Find to get fresh web data
	currentModel := e.mcpClient.GetModel()
	onlineModel := currentModel
	if !strings.HasSuffix(onlineModel, ":online") {
		onlineModel += ":online"
	}

	req := &mcp.Request{
		Model: onlineModel,
		Messages: []mcp.Message{
			{Role: "system", Content: "You are a crypto trading assistant. Always respond with raw JSON only - no markdown, no code fences, no extra text."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.7,
		MaxTokens:   4096,
	}

	respStruct, err := e.mcpClient.CallWithRequest(req)
	if err != nil {
		return nil, fmt.Errorf("AI request failed: %w", err)
	}
	response := respStruct.Content

	// 6. Parse Response (Extract JSON array)
	jsonStr := response

	// Strip markdown code blocks if present (handles ```json\n...\n``` format)
	jsonStr = strings.TrimSpace(jsonStr)

	// Remove opening code fence (with optional language identifier)
	if strings.HasPrefix(jsonStr, "```") {
		// Find the end of the first line (after ```json or just ```)
		if idx := strings.Index(jsonStr, "\n"); idx != -1 {
			jsonStr = jsonStr[idx+1:]
		} else {
			// No newline found, just strip the prefix
			jsonStr = strings.TrimPrefix(jsonStr, "```json")
			jsonStr = strings.TrimPrefix(jsonStr, "```")
		}
	}

	// Remove closing code fence (TrimSuffix is safe even if suffix doesn't exist)
	jsonStr = strings.TrimSuffix(jsonStr, "```")
	jsonStr = strings.TrimSpace(jsonStr)

	// Extract JSON array
	if idx := strings.Index(jsonStr, "["); idx != -1 {
		jsonStr = jsonStr[idx:]
	}
	if idx := strings.LastIndex(jsonStr, "]"); idx != -1 {
		jsonStr = jsonStr[:idx+1]
	}

	var recommended []string
	if err := json.Unmarshal([]byte(jsonStr), &recommended); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w (raw: %s)", err, response[:min(len(response), 200)])
	}

	// Final check: Filter out any avoided symbols that AI might have hallucinated
	var safeRecommended []string
	for _, sym := range recommended {
		if !avoidSet[sym] {
			safeRecommended = append(safeRecommended, sym)
		} else {
			log.Printf("[%s] 🛡️ Removed %s from AI recommendations (Auto-Avoid)", e.name, sym)
		}
	}

	return safeRecommended, nil
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

	// Update positions (merge with existing to preserve WebSocket-updated mark prices)
	positions, err := e.binance.GetPositions(ctx)
	if err != nil {
		log.Printf("[%s] Error getting positions: %v", e.name, err)
	} else {
		e.mu.Lock()
		// Build map of new positions from REST API
		newPositions := make(map[string]*exchange.Position)
		var symbolsToSubscribe []string
		for i := range positions {
			newPositions[positions[i].Symbol] = &positions[i]
			// Track symbols to subscribe for real-time mark price updates
			if positions[i].PositionAmt != 0 {
				symbolsToSubscribe = append(symbolsToSubscribe, positions[i].Symbol)
			}
		}

		// Subscribe to mark price streams for all positions
		if len(symbolsToSubscribe) > 0 {
			go e.binance.SubscribeToMarkPrices(symbolsToSubscribe)
		}

		// Use Binance REST data directly - it's authoritative and matches Binance UI.
		// WebSocket mark price updates will provide real-time PnL updates BETWEEN REST fetches.
		// This ensures displayed PnL matches what user sees on Binance.
		for symbol, newPos := range newPositions {
			e.positions[symbol] = newPos
		}

		// Remove closed positions (positions in e.positions but not in newPositions)
		for symbol, oldPos := range e.positions {
			if _, exists := newPositions[symbol]; !exists {
				if oldPos.PositionAmt != 0 {
					// Position was closed
					side := "LONG"
					if oldPos.PositionAmt < 0 {
						side = "SHORT"
					}
					log.Printf("[%s] Position closed (detected in trading cycle): %s %s", e.name, symbol, side)
				}
				delete(e.positions, symbol)
			}
		}
		e.mu.Unlock()
	}

	// Smart Find Auto-Refresh: Check if it's time to find new symbols
	// This runs AFTER positions are updated so we know our current state
	e.maybeRefreshSmartFind(ctx)

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
		log.Printf("[%s] Active positions: %d/%d. Analyzing %d trading pairs: %v",
			e.name, len(activeSymbols), maxPositions, len(pairsToAnalyze), pairsToAnalyze)
	}

	// AUTO-AVOID WORST SYMBOLS: Filter out symbols that have been losing in the last 24h
	// This only applies to NEW trades (not existing positions which must still be analyzed)
	if e.strategy != nil && e.strategy.Config.RiskControl.EnableAutoAvoidWorstSymbols {
		minLoss := e.strategy.Config.RiskControl.AutoAvoidMinLoss24h
		if minLoss <= 0 {
			minLoss = 5.0 // Default: 5 USDT loss
		}

		worstSymbols, err := e.positionStore.GetWorstSymbols24h(e.id, minLoss)
		if err != nil {
			log.Printf("[%s] ⚠️ Failed to get worst symbols for auto-avoid: %v", e.name, err)
		} else if len(worstSymbols) > 0 {
			// Build a set of symbols to avoid (only if they have enough trades)
			minTrades := e.strategy.Config.RiskControl.AutoAvoidMinTrades24h
			if minTrades <= 0 {
				minTrades = 2
			}

			avoidSet := make(map[string]bool)
			for _, ws := range worstSymbols {
				if ws.TradeCount >= minTrades {
					avoidSet[ws.Symbol] = true
					log.Printf("[%s] 🚫 Auto-avoiding %s (24h: %d trades, $%.2f PnL, %.1f%% win rate)",
						e.name, ws.Symbol, ws.TradeCount, ws.TotalPnL, ws.WinRate)
				}
			}

			// Filter pairs but KEEP any symbol that has an open position
			activeSet := make(map[string]bool)
			for _, sym := range activeSymbols {
				activeSet[sym] = true
			}

			var filteredPairs []string
			for _, symbol := range pairsToAnalyze {
				if avoidSet[symbol] && !activeSet[symbol] {
					// Skip this symbol - it's a loser and no open position
					continue
				}
				filteredPairs = append(filteredPairs, symbol)
			}

			if len(filteredPairs) < len(pairsToAnalyze) {
				log.Printf("[%s] 🛡️ Auto-avoid filtered out %d losing symbols. Trading: %v",
					e.name, len(pairsToAnalyze)-len(filteredPairs), filteredPairs)
				pairsToAnalyze = filteredPairs
			}
		}
	}

	// CRITICAL: Check if we have any pairs to analyze
	if len(pairsToAnalyze) == 0 {
		log.Printf("[%s] ⚠️ NO TRADING PAIRS TO ANALYZE! Check your coin source config (Static Coins, Smart Find, or Dynamic).", e.name)
		log.Printf("[%s] Coin Source Type: %s, Static Coins: %v",
			e.name,
			e.strategy.Config.CoinSource.SourceType,
			e.strategy.Config.CoinSource.StaticCoins)
		return // Exit early - nothing to analyze
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

	// Fetch Open Interest data from Binance (FREE - no API key needed!)
	// Uses caching to stay well under rate limits (1000 req/5min)
	// Cache TTL: 1 minute per symbol
	{
		const oiCacheTTL = 1 * time.Minute

		// Check cache first
		e.oiCacheMutex.RLock()
		cached, hasCached := e.oiCache[symbol]
		e.oiCacheMutex.RUnlock()

		if hasCached && time.Now().Before(cached.ExpiresAt) {
			// Use cached data
			if cached.Analysis != nil {
				marketData.OIValue = cached.Analysis.CurrentOI
				marketData.OIChange1H = cached.Analysis.OIChange1H
				marketData.OIChange4H = cached.Analysis.OIChange4H
				marketData.OIChange24H = cached.Analysis.OIChange24H
				marketData.OISignal = cached.Analysis.OISignal

				switch cached.Analysis.OISignal {
				case "BULLISH":
					marketData.OIDescription = "New longs opening - capital flowing into long positions"
				case "BEARISH":
					marketData.OIDescription = "New shorts opening - capital flowing into short positions"
				case "REVERSAL_UP":
					marketData.OIDescription = "Shorts covering - potential short squeeze, NOT new buying"
				case "REVERSAL_DOWN":
					marketData.OIDescription = "Longs capitulating - potential bounce, NOT new shorting"
				default:
					marketData.OIDescription = "No significant OI movement - market indecision"
				}

				// Populate Liquidation Data
				marketData.LiquidationPressure = cached.Analysis.LiquidationPressure
				marketData.LiquidationSeverity = cached.Analysis.LiquidationSeverity
				marketData.LiquidationMessage = cached.Analysis.LiquidationMessage
			}
			if cached.SentimentData != nil {
				marketData.LongRatio = cached.SentimentData.LongAccount * 100
				marketData.ShortRatio = cached.SentimentData.ShortAccount * 100
				marketData.SentimentTrend = cached.SentimentData.SentimentTrend
				marketData.SentimentMessage = cached.SentimentData.SentimentMessage
			}
		} else {
			// Fetch fresh data from Binance
			oiCtx, oiCancel := context.WithTimeout(ctx, 5*time.Second)

			newCacheEntry := &oiCacheEntry{
				ExpiresAt: time.Now().Add(oiCacheTTL),
			}

			// Fetch OI analysis from Binance (FREE)
			oiAnalysis, oiErr := e.binance.GetOIAnalysis(oiCtx, symbol, marketData.PriceChange24h)
			if oiErr != nil {
				log.Printf("[%s][OI] Failed to fetch OI data for %s: %v", e.name, symbol, oiErr)
			} else if oiAnalysis != nil {
				marketData.OIValue = oiAnalysis.CurrentOI
				marketData.OIChange1H = oiAnalysis.OIChange1H
				marketData.OIChange4H = oiAnalysis.OIChange4H
				marketData.OIChange24H = oiAnalysis.OIChange24H
				marketData.OISignal = oiAnalysis.OISignal
				newCacheEntry.Analysis = oiAnalysis

				switch oiAnalysis.OISignal {
				case "BULLISH":
					marketData.OIDescription = "New longs opening - capital flowing into long positions"
				case "BEARISH":
					marketData.OIDescription = "New shorts opening - capital flowing into short positions"
				case "REVERSAL_UP":
					marketData.OIDescription = "Shorts covering - potential short squeeze, NOT new buying"
				case "REVERSAL_DOWN":
					marketData.OIDescription = "Longs capitulating - potential bounce, NOT new shorting"
				default:
					marketData.OIDescription = "No significant OI movement - market indecision"
				}

				// Populate Liquidation Data
				marketData.LiquidationPressure = oiAnalysis.LiquidationPressure
				marketData.LiquidationSeverity = oiAnalysis.LiquidationSeverity
				marketData.LiquidationMessage = oiAnalysis.LiquidationMessage

				log.Printf("[%s][OI] %s: OI Change 1H: %+.2f%%, Signal: %s (%s)",
					e.name, symbol, oiAnalysis.OIChange1H, oiAnalysis.OISignal, oiAnalysis.OIConfidence)
			}

			// Fetch Long/Short Sentiment Analysis from Binance (FREE)
			sentimentData, lsErr := e.binance.GetLongShortAnalysis(oiCtx, symbol)
			if lsErr != nil {
				log.Printf("[%s][OI] Failed to fetch Sentiment data for %s: %v", e.name, symbol, lsErr)
			} else if sentimentData != nil {
				marketData.LongRatio = sentimentData.LongAccount * 100
				marketData.ShortRatio = sentimentData.ShortAccount * 100
				marketData.SentimentTrend = sentimentData.SentimentTrend
				marketData.SentimentMessage = sentimentData.SentimentMessage

				newCacheEntry.SentimentData = sentimentData

				if marketData.LongRatio > 70 {
					log.Printf("[%s][OI] ⚠️ %s: CROWDED LONG (%.1f%%) - reversal risk!", e.name, symbol, marketData.LongRatio)
				} else if marketData.ShortRatio > 70 {
					log.Printf("[%s][OI] ⚠️ %s: CROWDED SHORT (%.1f%%) - squeeze risk!", e.name, symbol, marketData.ShortRatio)
				}
				if sentimentData.SentimentTrend != "STABLE" {
					log.Printf("[%s][Sentiment] %s: %s", e.name, symbol, sentimentData.SentimentMessage)
				}
			}

			oiCancel()

			// Store in cache
			e.oiCacheMutex.Lock()
			e.oiCache[symbol] = newCacheEntry
			e.oiCacheMutex.Unlock()
		}
	}

	// Format data for AI
	// Format data for AI
	enableHighWick := true
	aiPromptCfg := market.DefaultAIPromptConfig()
	if e.strategy != nil {
		enableHighWick = e.strategy.Config.RiskControl.EnableHighWickWarning
		// Toggle entry warnings based on config
		aiPromptCfg.EnableEntryWarnings = e.strategy.Config.RiskControl.EnableEntrySafetyChecks
		// Use configured thresholds if set
		if e.strategy.Config.RiskControl.MinEMASpreadPct > 0 {
			aiPromptCfg.MinEMASpreadPct = e.strategy.Config.RiskControl.MinEMASpreadPct
		}
		if e.strategy.Config.RiskControl.MinVolumeRatioPct > 0 {
			aiPromptCfg.MinVolumeRatioPct = e.strategy.Config.RiskControl.MinVolumeRatioPct
		}
		if e.strategy.Config.RiskControl.ResistanceSupportPct > 0 {
			aiPromptCfg.ResistanceSupportPct = e.strategy.Config.RiskControl.ResistanceSupportPct
		}
	}
	formattedData := e.dataProvider.FormatForAI(marketData, enableHighWick, aiPromptCfg)

	// Fetch and inject market intelligence (uses caching, won't hit APIs every call)
	// Only fetch intel if enabled in strategy settings
	if e.intelProvider != nil && e.strategy != nil && e.strategy.Config.EnableMarketIntel {
		log.Printf("[%s] 🌐 Searching web for real-time market news & sentiment...", e.name)
		intelCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		marketIntel, err := e.intelProvider.GetMarketIntel(intelCtx, []string{symbol})
		cancel()
		if err != nil {
			log.Printf("[%s][Intel] Failed to fetch intel: %v", e.name, err)
		} else if marketIntel != nil {
			intelFormatted := intel.FormatForAI(marketIntel, []string{symbol}, 5)
			if intelFormatted != "" {
				formattedData = intelFormatted + formattedData
				log.Printf("[%s][Intel] Injected market intelligence data:\n%s", e.name, intelFormatted)
			} else {
				log.Printf("[%s][Intel] Market intelligence data fetched but formatted output is empty", e.name)
			}
		}
	}

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

	// Add Account-Wide Worst Performers Ranking (Context)
	// We calculate this early to inject CRITICAL WARNINGS at the top
	worstSymbols, err := e.positionStore.GetWorstSymbols24h(e.id, 0) // Get all losers
	var criticalContext string

	if err == nil && len(worstSymbols) > 0 {
		var worstList string
		isCurrentSymbolWorst := false

		// Show top 5 worst
		limit := 5
		if len(worstSymbols) < limit {
			limit = len(worstSymbols)
		}

		for i := 0; i < limit; i++ {
			ws := worstSymbols[i]
			worstList += fmt.Sprintf("%d. %s ($%.2f)\n", i+1, ws.Symbol, ws.TotalPnL)
			if ws.Symbol == symbol {
				isCurrentSymbolWorst = true
			}
		}

		// If this is a bad symbol, put it at the VERY TOP of the prompt
		if isCurrentSymbolWorst {
			criticalContext += fmt.Sprintf("\n🚨🚨🚨 CRITICAL WARNING: THIS IS A LOSING SYMBOL (%s) 🚨🚨🚨\n", symbol)
			criticalContext += "You have consistently LOST money on this symbol in the last 24h.\n"
			criticalContext += "Account-wide stats show it is one of your WORST performers.\n"
			criticalContext += "Unless the setup is PERFECT (A+), you should REJECT this trade.\n"
			criticalContext += "Do not try to 'make back' losses. Protect capital.\n\n"
		}

		// Append the list to the end of context for reference
		formattedData += "\n--- Account Worst Performers (24h) ---\n"
		formattedData += worstList
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

	// Add Global Safety Rules (Always Active)
	formattedData += "\n--- CRITICAL ENTRY RULES ---\n"
	formattedData += "1. DO NOT FOMO: If price is at 'Recent High' or 'Resistance', YOU MUST WAIT for a breakout + retest.\n"
	formattedData += "2. NO WICK ENTRIES: If the last candle has a long upper wick (rejection), DO NOT BUY.\n"
	formattedData += "3. PULLBACKS ONLY: Prefer entering on pullbacks to EMA, not when extended far above it.\n"
	formattedData += "4. TREND ALIGNMENT: If Price < EMA9 but EMA9 > EMA21, this is a pullback. Verify support before buying.\n"

	// Add 24h trading history for this symbol (with reasons and P&L)
	// This helps AI learn from recent trades and avoid repeating mistakes
	symbolHistory, err := e.positionStore.GetSymbolHistory24h(e.id, symbol)
	if err == nil && len(symbolHistory) > 0 {
		formattedData += "\n--- Recent 24h Trading History on This Symbol ---\n"
		formattedData += fmt.Sprintf("(Past %d trades. Learn from these results!)\n", len(symbolHistory))

		var totalPnL float64
		for i, trade := range symbolHistory {
			pnl, _ := trade["realized_pnl"].(float64)
			totalPnL += pnl

			side, _ := trade["side"].(string)
			entryPrice, _ := trade["entry_price"].(float64)
			exitPrice, _ := trade["exit_price"].(float64)
			entryReason, _ := trade["entry_reason"].(string)
			closeReason, _ := trade["close_reason"].(string)

			// Format: Trade #1: LONG @ 81.50 → 80.20 (-$1.30)
			// Entry: Strong momentum, RSI 55
			// Close: SL_HIT
			pnlStr := fmt.Sprintf("$%.2f", pnl)
			if pnl > 0 {
				pnlStr = "+" + pnlStr
			}

			// Show only last 5 trades for brevity
			if i < 5 {
				formattedData += fmt.Sprintf("  #%d: %s @ $%.2f → $%.2f (%s)\n", i+1, side, entryPrice, exitPrice, pnlStr)
				if entryReason != "" {
					// Truncate long reasons to 80 chars
					displayReason := entryReason
					if len(displayReason) > 80 {
						displayReason = displayReason[:77] + "..."
					}
					formattedData += fmt.Sprintf("      Why opened: %s\n", displayReason)
				}
				if closeReason != "" {
					formattedData += fmt.Sprintf("      Why closed: %s\n", closeReason)
				}
			}
		}

		if len(symbolHistory) > 5 {
			formattedData += fmt.Sprintf("  ... and %d more trades\n", len(symbolHistory)-5)
		}

		// Summary
		winCount := 0
		for _, trade := range symbolHistory {
			pnl, _ := trade["realized_pnl"].(float64)
			if pnl > 0 {
				winCount++
			}
		}
		winRate := float64(winCount) / float64(len(symbolHistory)) * 100

		totalPnLStr := fmt.Sprintf("$%.2f", totalPnL)
		if totalPnL > 0 {
			totalPnLStr = "+" + totalPnLStr
		}

		formattedData += fmt.Sprintf("  SUMMARY: %d trades, %s total, %.0f%% win rate\n", len(symbolHistory), totalPnLStr, winRate)

		if totalPnL < -2.0 { // Tighter threshold for warning
			msg := fmt.Sprintf("🚨 You are DOWN %s on this symbol today. Be CAREFUL. 🚨\n", totalPnLStr)
			formattedData += "  ⚠️ " + msg
			criticalContext += msg // Add to top as well
		} else if totalPnL > 10 {
			formattedData += "  ✅ This symbol has been profitable. Current strategy may be working.\n"
		}
	}

	// Prepend critical warnings to the very top
	if criticalContext != "" {
		formattedData = criticalContext + "\n" + formattedData
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

	// 🚨 ENTRY SAFETY CHECKS (Weighted Scoring) 🚨
	// Instead of hard-blocking, warnings reduce confidence. Trade proceeds if adjusted confidence >= minConfidence.
	// Only critical errors (liquidation cascades) hard-block.
	if !hasPosition && (decision.Action == "BUY" || decision.Action == "SELL" || decision.Action == "open_long" || decision.Action == "open_short") {
		safetyResult := e.checkEntrySafety(symbol, decision, marketData)

		// Critical errors are hard blocks (e.g., liquidation cascades)
		if safetyResult.CriticalErr != nil {
			log.Printf("[%s][%s] ❌ CRITICAL BLOCK: %v", e.name, symbol, safetyResult.CriticalErr)
			tradeLog.Error = fmt.Sprintf("critical block: %v", safetyResult.CriticalErr)
			return tradeLog
		}

		// Apply confidence penalties from warnings
		if len(safetyResult.Warnings) > 0 {
			originalConfidence := decision.Confidence
			adjustedConfidence := originalConfidence - safetyResult.TotalPenalty

			// Log each warning
			for _, w := range safetyResult.Warnings {
				log.Printf("[%s][%s] ⚠️ WARNING [%s] -%0.f%%: %s", e.name, symbol, w.Filter, w.Penalty, w.Message)
			}

			log.Printf("[%s][%s] 📊 CONFIDENCE ADJUSTED: %.0f%% -> %.0f%% (-%0.f%% total penalty from %d warnings)",
				e.name, symbol, originalConfidence, adjustedConfidence, safetyResult.TotalPenalty, len(safetyResult.Warnings))

			// Check if adjusted confidence still meets minimum
			minConf := float64(e.getMinConfidence())
			if adjustedConfidence < minConf {
				log.Printf("[%s][%s] ❌ BLOCKED: Adjusted confidence %.0f%% < min %.0f%% (warnings caused block)",
					e.name, symbol, adjustedConfidence, minConf)
				tradeLog.Error = fmt.Sprintf("blocked: adjusted confidence %.0f%% < min %.0f%% (penalties: %s)",
					adjustedConfidence, minConf, formatWarnings(safetyResult.Warnings))
				return tradeLog
			}

			// Update decision confidence with adjusted value
			decision.Confidence = adjustedConfidence
			log.Printf("[%s][%s] ✅ PROCEEDING: Adjusted confidence %.0f%% >= min %.0f%%",
				e.name, symbol, adjustedConfidence, minConf)
		}
	}

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

					// ENHANCED CHECK: Ensure Price is also respecting the trend
					// If buying, price should be above 15m EMA21 (not crashing through it)
					// If selling, price should be below 15m EMA21 (not pumping through it)
					priceRespectsTrend := false
					if htfBullish {
						// Bullish trend: Price > EMA21
						priceRespectsTrend = htfData.CurrentPrice >= htfData.EMA21
					} else {
						// Bearish trend: Price < EMA21
						priceRespectsTrend = htfData.CurrentPrice <= htfData.EMA21
					}

					wantLong := decision.Action == "BUY"

					// Validation:
					// 1. Trend Direction must match (EMA structure)
					// 2. Price Action must match (Price vs EMA relation)

					isTrendAligned := (wantLong && htfBullish) || (!wantLong && !htfBullish)

					if !isTrendAligned {
						log.Printf("[%s][%s] ❌ BLOCKED: Multi-TF Trend Disagreement. 5m want %s but %s is %s (EMA9: %.2f, EMA21: %.2f)",
							e.name, symbol, decision.Action, confirmTF,
							map[bool]string{true: "BULLISH", false: "BEARISH"}[htfBullish],
							htfData.EMA9, htfData.EMA21)
						tradeLog.Error = fmt.Sprintf("blocked: %s trend disagrees", confirmTF)
						return tradeLog
					}

					if !priceRespectsTrend {
						log.Printf("[%s][%s] ❌ BLOCKED: Multi-TF Price Action Warning. Trend is correct but Price is countering it (Breaking EMA21). Price: $%.4f, EMA21: $%.4f",
							e.name, symbol, htfData.CurrentPrice, htfData.EMA21)
						tradeLog.Error = fmt.Sprintf("blocked: %s price action failure (breaking trend support/resistance)", confirmTF)
						return tradeLog
					}

					// If we get here, both Trend and Price Action are aligned
					log.Printf("[%s][%s] ✅ Multi-TF confirmed: %s trend agrees on %s AND Price is respecting EMA21",
						e.name, symbol, confirmTF, decision.Action)
				}
			}

			// SIGNAL CONFIRMATION - Verify medium-confidence signals before executing
			// High confidence (90%+) executes immediately, medium confidence (75-89%) waits and re-verifies
			if e.strategy != nil && e.strategy.Config.RiskControl.EnableSignalConfirmation {
				rc := e.strategy.Config.RiskControl
				highConfThreshold := rc.HighConfidenceThreshold
				if highConfThreshold <= 0 {
					highConfThreshold = 90.0
				}

				// Only require confirmation for medium-confidence trades
				if decision.Confidence < highConfThreshold {
					confirmDelaySec := rc.SignalConfirmationDelaySec
					if confirmDelaySec <= 0 {
						confirmDelaySec = 60
					}
					priceStabilityPct := rc.PriceStabilityCheckPct
					if priceStabilityPct <= 0 {
						priceStabilityPct = 0.5
					}

					// Record price before waiting
					priceBeforeWait := marketData.CurrentPrice

					log.Printf("[%s][%s] ⏳ SIGNAL CONFIRMATION: Confidence %.0f%% < %.0f%% threshold. Waiting %ds to re-verify...",
						e.name, symbol, decision.Confidence, highConfThreshold, confirmDelaySec)

					// Wait for confirmation delay
					time.Sleep(time.Duration(confirmDelaySec) * time.Second)

					// Re-fetch fresh market data
					freshMarketData, err := e.dataProvider.GetMarketDataWithConfig(ctx, symbol, timeframe, klineCount)
					if err != nil {
						log.Printf("[%s][%s] ❌ BLOCKED: Failed to re-fetch market data for confirmation: %v", e.name, symbol, err)
						tradeLog.Error = fmt.Sprintf("signal confirmation failed: could not refresh data: %v", err)
						return tradeLog
					}

					// Price stability check
					priceAfterWait := freshMarketData.CurrentPrice
					priceDiffPct := ((priceAfterWait - priceBeforeWait) / priceBeforeWait) * 100
					if priceDiffPct < 0 {
						priceDiffPct = -priceDiffPct // Absolute value
					}

					if priceDiffPct > priceStabilityPct {
						log.Printf("[%s][%s] ❌ BLOCKED: Price moved too much during confirmation (%.2f%% > %.2f%%). Before: $%.4f, After: $%.4f",
							e.name, symbol, priceDiffPct, priceStabilityPct, priceBeforeWait, priceAfterWait)
						tradeLog.Error = fmt.Sprintf("signal confirmation failed: price unstable (moved %.2f%%)", priceDiffPct)
						return tradeLog
					}

					// Format fresh data for AI
					// Format fresh data for AI
					enableHighWick := true
					freshAIPromptCfg := market.DefaultAIPromptConfig()
					if e.strategy != nil {
						enableHighWick = e.strategy.Config.RiskControl.EnableHighWickWarning
						freshAIPromptCfg.EnableEntryWarnings = e.strategy.Config.RiskControl.EnableEntrySafetyChecks
						if e.strategy.Config.RiskControl.MinEMASpreadPct > 0 {
							freshAIPromptCfg.MinEMASpreadPct = e.strategy.Config.RiskControl.MinEMASpreadPct
						}
						if e.strategy.Config.RiskControl.MinVolumeRatioPct > 0 {
							freshAIPromptCfg.MinVolumeRatioPct = e.strategy.Config.RiskControl.MinVolumeRatioPct
						}
						if e.strategy.Config.RiskControl.ResistanceSupportPct > 0 {
							freshAIPromptCfg.ResistanceSupportPct = e.strategy.Config.RiskControl.ResistanceSupportPct
						}
					}
					freshFormattedData := e.dataProvider.FormatForAI(freshMarketData, enableHighWick, freshAIPromptCfg)

					// Add account and position info
					e.mu.RLock()
					if e.account != nil {
						freshFormattedData += "\n--- Account Info ---\n"
						freshFormattedData += fmt.Sprintf("Total Equity: $%.2f\n", e.account.TotalMarginBalance)
						freshFormattedData += fmt.Sprintf("Available Balance: $%.2f\n", e.account.AvailableBalance)
					}
					freshFormattedData += "\n--- No Current Position ---\n"
					e.mu.RUnlock()

					// Add strategy rules
					if e.strategy != nil && e.strategy.Config.CustomPrompt != "" {
						freshFormattedData += fmt.Sprintf("\n--- Strategy Rules ---\n%s\n", e.strategy.Config.CustomPrompt)
					}

					// Re-ask AI with fresh data
					log.Printf("[%s][%s] 🔄 Re-verifying signal with fresh data...", e.name, symbol)
					var confirmDecision *ai.TradingDecision
					var confirmRaw string
					var confirmErr error

					if e.strategy != nil && e.strategy.Config.SimpleMode {
						confirmDecision, confirmRaw, confirmErr = e.aiClient.GetTradingDecisionSimple(freshFormattedData)
					} else {
						confirmDecision, confirmRaw, confirmErr = e.aiClient.GetTradingDecision(freshFormattedData)
					}

					if confirmErr != nil {
						log.Printf("[%s][%s] ❌ BLOCKED: AI confirmation call failed: %v", e.name, symbol, confirmErr)
						tradeLog.Error = fmt.Sprintf("signal confirmation failed: AI error: %v", confirmErr)
						return tradeLog
					}

					// Check if AI still agrees with original decision
					// Normalize actions: BUY/LONG/open_long are equivalent, SELL/SHORT/open_short are equivalent
					originalDirection := normalizeActionDirection(decision.Action)
					confirmDirection := normalizeActionDirection(confirmDecision.Action)
					if confirmDirection != originalDirection {
						log.Printf("[%s][%s] ❌ BLOCKED: Signal NOT confirmed. Original: %s (%.0f%%), Recheck: %s (%.0f%%)",
							e.name, symbol, decision.Action, decision.Confidence, confirmDecision.Action, confirmDecision.Confidence)
						tradeLog.Error = fmt.Sprintf("signal confirmation failed: AI changed mind from %s to %s", decision.Action, confirmDecision.Action)
						tradeLog.RawAI = confirmRaw
						return tradeLog
					}

					// Signal confirmed!
					log.Printf("[%s][%s] ✅ SIGNAL CONFIRMED: AI still says %s (Original: %.0f%%, Recheck: %.0f%%). Price stable (moved %.2f%%). Executing...",
						e.name, symbol, decision.Action, decision.Confidence, confirmDecision.Confidence, priceDiffPct)

					// Use the confirmed decision (might have updated confidence/SL/TP)
					decision = confirmDecision
					tradeLog.Decision = confirmDecision
					tradeLog.RawAI = confirmRaw

					// 🚨 RE-VERIFY Entry Safety with fresh data (Weighted Scoring)
					if !hasPosition {
						safetyResult := e.checkEntrySafety(symbol, decision, freshMarketData)

						// Critical errors are hard blocks
						if safetyResult.CriticalErr != nil {
							log.Printf("[%s][%s] ❌ CRITICAL BLOCK (Post-Confirmation): %v", e.name, symbol, safetyResult.CriticalErr)
							tradeLog.Error = fmt.Sprintf("critical block after confirmation: %v", safetyResult.CriticalErr)
							return tradeLog
						}

						// Apply confidence penalties from warnings
						if len(safetyResult.Warnings) > 0 {
							originalConfidence := decision.Confidence
							adjustedConfidence := originalConfidence - safetyResult.TotalPenalty

							for _, w := range safetyResult.Warnings {
								log.Printf("[%s][%s] ⚠️ WARNING (Post-Confirm) [%s] -%0.f%%: %s", e.name, symbol, w.Filter, w.Penalty, w.Message)
							}

							log.Printf("[%s][%s] 📊 POST-CONFIRM CONFIDENCE: %.0f%% -> %.0f%% (-%0.f%% penalty)",
								e.name, symbol, originalConfidence, adjustedConfidence, safetyResult.TotalPenalty)

							minConf := float64(e.getMinConfidence())
							if adjustedConfidence < minConf {
								log.Printf("[%s][%s] ❌ BLOCKED (Post-Confirmation): Adjusted confidence %.0f%% < min %.0f%%",
									e.name, symbol, adjustedConfidence, minConf)
								tradeLog.Error = fmt.Sprintf("blocked after confirmation: adjusted confidence %.0f%% < min %.0f%% (penalties: %s)",
									adjustedConfidence, minConf, formatWarnings(safetyResult.Warnings))
								return tradeLog
							}

							decision.Confidence = adjustedConfidence
						}
					}
				} else {
					log.Printf("[%s][%s] ⚡ HIGH CONFIDENCE (%.0f%% >= %.0f%%): Executing immediately, no confirmation needed",
						e.name, symbol, decision.Confidence, highConfThreshold)
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

// checkEntrySafety enforces rules to prevent bad entries using WEIGHTED SCORING
// Instead of hard-blocking, each check returns a warning with a confidence penalty.
// Trade proceeds if (originalConfidence - totalPenalties) >= minConfidence.
// Only truly dangerous situations (liquidation cascades) return critical errors.
func (e *Engine) checkEntrySafety(symbol string, decision *ai.TradingDecision, marketData *market.MarketData) *EntryCheckResult {
	result := &EntryCheckResult{
		Warnings:     []EntryWarning{},
		CriticalErr:  nil,
		TotalPenalty: 0,
	}

	if decision.Action != "BUY" && decision.Action != "SELL" &&
		decision.Action != "open_long" && decision.Action != "open_short" {
		return result
	}

	isLong := decision.Action == "BUY" || decision.Action == "open_long"
	ema9 := marketData.EMA9
	ema21 := marketData.EMA21
	currentPrice := marketData.CurrentPrice

	// Helper to add warnings
	addWarning := func(filter, message string, penalty float64, severity string) {
		result.Warnings = append(result.Warnings, EntryWarning{
			Filter:   filter,
			Message:  message,
			Penalty:  penalty,
			Severity: severity,
		})
		result.TotalPenalty += penalty
	}

	// Load configurable thresholds (with safe defaults)
	minEmaSpread := 0.15        // default (reduced from 0.3 - was too strict for volatile altcoins)
	minVolumeRatio := 0.4       // default (40%)
	maxWickRejections := 4      // default
	resistanceSupportPct := 1.0 // default (1%)
	if e.strategy != nil {
		if e.strategy.Config.RiskControl.MinEMASpreadPct > 0 {
			minEmaSpread = e.strategy.Config.RiskControl.MinEMASpreadPct
		}
		if e.strategy.Config.RiskControl.MinVolumeRatioPct > 0 {
			minVolumeRatio = e.strategy.Config.RiskControl.MinVolumeRatioPct / 100.0
		}
		if e.strategy.Config.RiskControl.MaxWickRejectionCount > 0 {
			maxWickRejections = e.strategy.Config.RiskControl.MaxWickRejectionCount
		}
		if e.strategy.Config.RiskControl.ResistanceSupportPct > 0 {
			resistanceSupportPct = e.strategy.Config.RiskControl.ResistanceSupportPct
		}
	}

	// 1. Late Entry Detection - Penalty: 25% (CRITICAL)
	// Penalize entries when price is EXTENDED from EMA9 (chasing the move)
	// This is the OPPOSITE of the old "counter-trend" check which was backwards
	// Professional traders want to enter on PULLBACKS to EMA, not when extended
	// Reference: https://highstrike.com/pullback-trading/
	if ema9 > 0 {
		if isLong {
			// For LONG: penalize if price is too far ABOVE EMA9 (chasing pump)
			extension := ((currentPrice - ema9) / ema9) * 100
			if extension > 1.5 {
				addWarning("late-entry", fmt.Sprintf("price extended %.2f%% ABOVE EMA9 - chasing the move, wait for pullback", extension), 25, "critical")
			}
		} else {
			// For SHORT: penalize if price is too far BELOW EMA9 (chasing dump)
			extension := ((ema9 - currentPrice) / ema9) * 100
			if extension > 1.5 {
				addWarning("late-entry", fmt.Sprintf("price extended %.2f%% BELOW EMA9 - chasing the dump, wait for bounce", extension), 25, "critical")
			}
		}
	}

	// 2. EMA Spread Strength Gate - Penalty: 10% for weak, 15% for mismatch
	emaSpread := 0.0
	if ema21 > 0 {
		emaSpread = ((ema9 - ema21) / ema21) * 100
	}
	absEmaSpread := emaSpread
	if absEmaSpread < 0 {
		absEmaSpread = -absEmaSpread
	}

	if absEmaSpread < minEmaSpread {
		addWarning("weak-trend", fmt.Sprintf("EMA spread %.2f%% is too weak (need >= %.1f%%). Choppy market", absEmaSpread, minEmaSpread), 10, "medium")
	}

	// Verify spread direction matches trade direction
	if isLong && emaSpread < 0 {
		addWarning("trend-mismatch", "trying to LONG but EMA9 < EMA21 (bearish structure)", 15, "high")
	}
	if !isLong && emaSpread > 0 {
		addWarning("trend-mismatch", "trying to SHORT but EMA9 > EMA21 (bullish structure)", 15, "high")
	}

	// 3. Momentum Exhaustion Detection - Penalty: 15%
	if ema9 > 0 {
		priceExtension := ((currentPrice - ema9) / ema9) * 100

		if isLong && priceExtension > 1.0 && marketData.MACDHist < 0 {
			addWarning("momentum-exhaustion", fmt.Sprintf("price extended %.2f%% above EMA9 with negative MACD histogram (%.4f) - likely reversal",
				priceExtension, marketData.MACDHist), 15, "high")
		}

		if !isLong && priceExtension < -1.0 && marketData.MACDHist > 0 {
			addWarning("momentum-exhaustion", fmt.Sprintf("price extended %.2f%% below EMA9 with positive MACD histogram (%.4f) - likely reversal",
				priceExtension, marketData.MACDHist), 15, "high")
		}
	}

	// 4. Wick Rejection Pattern Detection - Penalty: 10%
	if len(marketData.Klines) >= 5 {
		recentCandles := marketData.Klines[len(marketData.Klines)-5:]
		upperWickRejections := 0
		lowerWickRejections := 0

		for _, k := range recentCandles {
			bodySize := k.Close - k.Open
			if bodySize < 0 {
				bodySize = -bodySize
			}
			totalRange := k.High - k.Low
			if totalRange <= 0 {
				continue
			}

			maxOC := k.Open
			if k.Close > k.Open {
				maxOC = k.Close
			}
			minOC := k.Open
			if k.Close < k.Open {
				minOC = k.Close
			}

			upperWick := k.High - maxOC
			lowerWick := minOC - k.Low

			if bodySize > 0 && upperWick > bodySize*0.5 {
				upperWickRejections++
			}
			if bodySize > 0 && lowerWick > bodySize*0.5 {
				lowerWickRejections++
			}
		}

		if isLong && upperWickRejections >= maxWickRejections {
			addWarning("wick-rejection", fmt.Sprintf("%d of last 5 candles show upper wick rejection - sellers defending highs", upperWickRejections), 10, "medium")
		}

		if !isLong && lowerWickRejections >= maxWickRejections {
			addWarning("wick-rejection", fmt.Sprintf("%d of last 5 candles show lower wick rejection - buyers defending lows", lowerWickRejections), 10, "medium")
		}
	}

	// 5. Volume Decline Detection - Penalty: 5% (lower because volume can be noisy)
	if len(marketData.Klines) >= 7 {
		recentVol := marketData.Klines[len(marketData.Klines)-2].Volume

		volumes := make([]float64, 5)
		for i := 0; i < 5; i++ {
			volumes[i] = marketData.Klines[len(marketData.Klines)-7+i].Volume
		}

		sort.Float64s(volumes)
		medianVol := volumes[2]

		if medianVol > 0 && recentVol < medianVol*minVolumeRatio {
			addWarning("weak-volume", fmt.Sprintf("last candle volume %.0f is %.0f%% below median %.0f - weak conviction move",
				recentVol, (1-(recentVol/medianVol))*100, medianVol), 5, "low")
		}
	}

	// 6. Resistance/Support FOMO Check - Penalty: 15%
	if len(marketData.Klines) >= 40 {
		recentCandles := marketData.Klines[len(marketData.Klines)-40:]
		var recentHigh, recentLow float64
		recentHigh = recentCandles[0].High
		recentLow = recentCandles[0].Low
		for _, c := range recentCandles {
			if c.High > recentHigh {
				recentHigh = c.High
			}
			if c.Low < recentLow {
				recentLow = c.Low
			}
		}

		resistanceThreshold := 1.0 - (resistanceSupportPct / 100.0)
		if isLong {
			if currentPrice < recentHigh && currentPrice >= recentHigh*resistanceThreshold {
				addWarning("resistance-fomo", fmt.Sprintf("price $%.4f is within %.1f%% of 40-candle high $%.4f (BUY THE TOP)", currentPrice, resistanceSupportPct, recentHigh), 15, "high")
			}
		}

		supportThreshold := 1.0 + (resistanceSupportPct / 100.0)
		if !isLong {
			if currentPrice > recentLow && currentPrice <= recentLow*supportThreshold {
				addWarning("support-fomo", fmt.Sprintf("price $%.4f is within %.1f%% of 40-candle low $%.4f (SELL THE BOTTOM)", currentPrice, resistanceSupportPct, recentLow), 15, "high")
			}
		}
	}

	// 7. RSI Extreme Check - Penalty: 10%
	if isLong && marketData.RSI > 75 {
		addWarning("rsi-overbought", fmt.Sprintf("RSI %.1f > 75 - risky for LONG entry", marketData.RSI), 10, "medium")
	}
	if !isLong && marketData.RSI < 25 {
		addWarning("rsi-oversold", fmt.Sprintf("RSI %.1f < 25 - risky for SHORT entry", marketData.RSI), 10, "medium")
	}

	// 8. OI-based Safety Check - Penalty: 10%
	if marketData.OISignal != "" {
		if isLong && marketData.OISignal == "REVERSAL_UP" {
			if marketData.OIChange1H < -2 {
				addWarning("oi-reversal", fmt.Sprintf("price up but OI down %.2f%% (shorts covering, not new longs) - wait for OI to turn positive", marketData.OIChange1H), 10, "medium")
			}
		}
		if !isLong && marketData.OISignal == "REVERSAL_DOWN" {
			if marketData.OIChange1H < -2 {
				addWarning("oi-reversal", fmt.Sprintf("price down but OI down %.2f%% (longs capitulating, not new shorts) - wait for OI to turn positive", marketData.OIChange1H), 10, "medium")
			}
		}

		// OI Crowding - Penalty: 10%
		if isLong && marketData.LongRatio > 75 {
			addWarning("oi-crowding", fmt.Sprintf("%.1f%% of traders are already LONG - contrarian reversal risk", marketData.LongRatio), 10, "medium")
		}
		if !isLong && marketData.ShortRatio > 75 {
			addWarning("oi-crowding", fmt.Sprintf("%.1f%% of traders are already SHORT - short squeeze risk", marketData.ShortRatio), 10, "medium")
		}
	}

	// 9. Market Regime-Based Rules (Task 4.2)
	// Reference: https://arxiv.org/html/2510.15949v2 (ATLAS Framework)
	if marketData.MarketRegime != "" {
		switch marketData.MarketRegime {
		case "EXHAUSTED":
			// In EXHAUSTED regime: Only allow pullback entries, heavy penalty for chasing
			// Price must be near EMA for entry
			if marketData.DistanceFromEMA9Pct > 0.5 && isLong {
				addWarning("regime-exhausted", fmt.Sprintf("EXHAUSTED regime + price %.2f%% above EMA9 - wait for pullback to EMA", marketData.DistanceFromEMA9Pct), 20, "critical")
			}
			if marketData.DistanceFromEMA9Pct < -0.5 && !isLong {
				addWarning("regime-exhausted", fmt.Sprintf("EXHAUSTED regime + price %.2f%% below EMA9 - wait for bounce to EMA", -marketData.DistanceFromEMA9Pct), 20, "critical")
			}

		case "VOLATILE":
			// In VOLATILE regime: Warn about high volatility, suggest reduced size
			addWarning("regime-volatile", fmt.Sprintf("VOLATILE regime (ATR %.1fx avg) - higher risk, consider smaller position size", marketData.ATRRatio), 10, "medium")

		case "RANGING":
			// In RANGING regime: Momentum trades are risky, warn about choppy conditions
			if marketData.BollingerSqueeze {
				// Squeeze = potential breakout, be cautious
				addWarning("regime-squeeze", "Bollinger squeeze detected - breakout imminent but direction uncertain", 5, "low")
			} else {
				// Normal ranging - momentum trades tend to fail
				addWarning("regime-ranging", fmt.Sprintf("RANGING regime (ADX %.1f) - momentum trades have lower win rate in choppy markets", marketData.ADX), 10, "medium")
			}
		}
	}

	// 10. Fibonacci Level Bonus/Penalty (Task 3.2)
	// Entries at Fib levels have better risk/reward
	if marketData.AtFibSupport && isLong {
		// Being at Fib support for a LONG is good - reduce penalty slightly
		addWarning("fib-support", fmt.Sprintf("Price at Fib %s%% support ($%.2f) - favorable entry level", marketData.NearestFibLevel, marketData.FibLevel382), -5, "bonus")
	}
	if marketData.AtFibResistance && !isLong {
		// Being at Fib resistance for a SHORT is good - reduce penalty slightly
		addWarning("fib-resistance", fmt.Sprintf("Price at Fib %s%% resistance ($%.2f) - favorable entry level", marketData.NearestFibLevel, marketData.FibLevel382), -5, "bonus")
	}

	// 11. Volume Profile Confirmation (Task 3.3)
	if marketData.PullbackVolumeSignal == "UNHEALTHY" {
		addWarning("volume-unhealthy", fmt.Sprintf("Pullback volume %.1fx trend volume - may continue, not healthy pullback", marketData.PullbackVolumeRatio), 10, "medium")
	}
	if marketData.VolumeConfirmation {
		// Healthy volume pattern - slight bonus
		addWarning("volume-confirmed", "Healthy pullback volume pattern - confirms entry", -3, "bonus")
	}

	// 12. Liquidation Pressure Check - CRITICAL ERROR (hard block)
	// This is truly dangerous and should NOT be bypassed by high confidence
	if marketData.LiquidationPressure != "" && marketData.LiquidationPressure != "NONE" {
		if isLong && marketData.LiquidationPressure == "LONG_LIQUIDATION" {
			result.CriticalErr = fmt.Errorf("LIQUIDATION BLOCK: Longs are being liquidated (Severity: %s) - catching a falling knife. Wait for liquidations to settle.", marketData.LiquidationSeverity)
			return result
		}
		if !isLong && marketData.LiquidationPressure == "SHORT_LIQUIDATION" {
			result.CriticalErr = fmt.Errorf("LIQUIDATION BLOCK: Shorts are being squeezed (Severity: %s) - shorting into a rocket. Wait for squeeze to exhaust.", marketData.LiquidationSeverity)
			return result
		}
	}

	return result
}

// formatWarnings creates a concise string summary of entry warnings for logging
func formatWarnings(warnings []EntryWarning) string {
	if len(warnings) == 0 {
		return "none"
	}
	parts := make([]string, len(warnings))
	for i, w := range warnings {
		parts[i] = fmt.Sprintf("%s(-%.0f%%)", w.Filter, w.Penalty)
	}
	return strings.Join(parts, ", ")
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
	maxPosPct := e.getPositionPercent()
	if e.strategy != nil && e.strategy.Config.RiskControl.MaxPositionPercent > 0 {
		maxPosPct = e.strategy.Config.RiskControl.MaxPositionPercent
	}

	// Log balance info for debugging
	log.Printf("[%s][%s] Balance: equity=$%.2f, available=$%.2f, leverage=%dx, positionPct=%.1f%%",
		e.name, symbol, equity, account.AvailableBalance, leverage, maxPosPct)

	// Log decision details if reasoning provided
	if decision.Reasoning != "" {
		log.Printf("[%s][%s] %s Reasoning: %s", e.name, symbol, decision.Action, decision.Reasoning)
	}

	// Calculate position size based on FRESH balance and strategy config
	// CRITICAL: Use `account` (fresh) not `e.account` (cached) to prevent over-leveraging
	positionSizeUSD := (account.TotalMarginBalance * maxPosPct) / 100

	// Apply margin safety check (COPIED FROM NOFX)
	// ⚠️ Auto-adjust position size if insufficient margin
	// Formula: totalRequired = positionSize/leverage + positionSize*0.001 + positionSize/leverage*0.01
	//        = positionSize * (1.01/leverage + 0.001)
	marginFactor := 1.01/float64(leverage) + 0.001
	// CRITICAL: Use fresh account.AvailableBalance, not cached e.account
	maxAffordablePositionSize := account.AvailableBalance / marginFactor

	if positionSizeUSD > maxAffordablePositionSize {
		// Cap at max affordable - margin buffer will be applied later via applyMarginBuffer()
		// NOTE: Do NOT multiply by 0.98 here as that would double-apply the buffer
		log.Printf("[%s][%s] ⚠️ Position size $%.2f exceeds max affordable $%.2f, capping to max",
			e.name, symbol, positionSizeUSD, maxAffordablePositionSize)
		positionSizeUSD = maxAffordablePositionSize

		// CRITICAL: Reject immediately if capped position is below minimum
		// This prevents opening tiny unprofitable positions
		if isOpenAction && !hasPosition {
			minRequired := e.getMinPositionSize(symbol)
			if positionSizeUSD < minRequired {
				log.Printf("[%s][%s] ❌ REJECTED: Affordable margin $%.2f is below minimum $%.2f. Increase balance or reduce other positions.",
					e.name, symbol, positionSizeUSD, minRequired)
				return 0, fmt.Errorf("skipped: insufficient margin ($%.2f available, need $%.2f minimum for %s)",
					positionSizeUSD, minRequired, symbol)
			}
		}
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

	// VALIDATION: Ensure notional value is large enough to be profitable after fees
	// Binance fees are ~0.04% maker / 0.05% taker, so we need at least $5-10 notional
	// to have any chance of profit after entry + exit fees
	minNotionalValue := 10.0 // Minimum $10 notional for profitable trading
	if isBTCETH(symbol) {
		minNotionalValue = 50.0 // BTC/ETH require $50 minimum on Binance
	}
	if actualPositionValue < minNotionalValue {
		return 0, fmt.Errorf("skipped: position notional $%.2f below minimum $%.2f for %s (margin $%.2f × %dx = $%.2f). Increase balance or position %%",
			actualPositionValue, minNotionalValue, symbol, positionSizeUSD, leverage, actualPositionValue)
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

		// Save position to database with entry reason (for 24h history)
		e.positionStore.Create(&store.TraderPosition{
			TraderID:      e.id,
			Symbol:        symbol,
			Side:          "long",
			EntryQuantity: filledQty,
			Quantity:      filledQty,
			EntryPrice:    entryPrice,
			EntryTime:     time.Now(),
			Leverage:      leverage,
			EntryReason:   decision.Reasoning, // Save AI's reasoning for opening
			Source:        "system",
		})

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

		// Save position to database with entry reason (for 24h history)
		e.positionStore.Create(&store.TraderPosition{
			TraderID:      e.id,
			Symbol:        symbol,
			Side:          "short",
			EntryQuantity: filledQty,
			Quantity:      filledQty,
			EntryPrice:    entryPrice,
			EntryTime:     time.Now(),
			Leverage:      leverage,
			EntryReason:   decision.Reasoning, // Save AI's reasoning for opening
			Source:        "system",
		})

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
		// We calculate BOTH Raw (Price) and ROE (Equity) percentages for different uses:
		// 1. Raw %: Used for Noise Zone (prevent instant triggers on tiny moves)
		// 2. ROE %: Used for logging/user display (matches Binance UI)
		var pnlPct float64    // Will hold Raw % for Noise Zone logic
		var roePnlPct float64 // Will hold ROE % for display

		if currentPos.EntryPrice > 0 {
			if currentPos.PositionAmt > 0 { // Long position
				pnlPct = ((currentPos.MarkPrice - currentPos.EntryPrice) / currentPos.EntryPrice) * 100
			} else { // Short position
				pnlPct = ((currentPos.EntryPrice - currentPos.MarkPrice) / currentPos.EntryPrice) * 100
			}

			// Calculate ROE for display purposes
			leverage := float64(currentPos.Leverage)
			if leverage < 1 {
				leverage = 1
			}
			roePnlPct = pnlPct * leverage
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
			if isNewPosition && !isHighConfidence {
				// NEW positions in noise zone: BLOCK unless AI is very certain (HighConfidenceCloseThreshold)
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

		log.Printf("[%s][%s] Closing %s position: %.4f (held for %v, estimated profit: $%.2f = %.2f%% ROE)",
			e.name, symbol, side, currentPos.PositionAmt, holdDuration, estimatedPnL, roePnlPct)
		closeOrder, err := e.binance.ClosePosition(ctx, symbol, currentPos.PositionAmt)
		if err != nil {
			return 0, fmt.Errorf("failed to close position: %w", err)
		}
		e.clearPositionTracking(symbol, side)
		e.cancelBracketOrders(ctx, symbol)

		// Immediately update local position state so UI reflects the close
		e.mu.Lock()
		if pos, exists := e.positions[symbol]; exists {
			pos.PositionAmt = 0
		}
		e.mu.Unlock()

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

// GetPositions returns current positions (filters out dust/closed positions)
func (e *Engine) GetPositions() []map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	positions := make([]map[string]interface{}, 0)
	for _, pos := range e.positions {
		// Skip zero positions
		if pos.PositionAmt == 0 {
			continue
		}

		// Calculate notional value to filter dust
		amt := pos.PositionAmt
		if amt < 0 {
			amt = -amt
		}
		notionalValue := amt * pos.MarkPrice

		// Skip dust positions (< $1 notional)
		if notionalValue < 1.0 {
			continue
		}

		// Calculate PnL % (raw price change)
		pnlPct := 0.0
		if pos.EntryPrice > 0 {
			if pos.PositionAmt > 0 {
				pnlPct = ((pos.MarkPrice - pos.EntryPrice) / pos.EntryPrice) * 100
			} else {
				pnlPct = ((pos.EntryPrice - pos.MarkPrice) / pos.EntryPrice) * 100
			}
		}

		// Calculate ROE % (Return on Equity = raw % × leverage)
		// This matches what Binance shows in their UI
		roePct := pnlPct
		if pos.Leverage > 0 {
			roePct = pnlPct * float64(pos.Leverage)
		}

		positions = append(positions, map[string]interface{}{
			"symbol":      pos.Symbol,
			"side":        map[bool]string{true: "LONG", false: "SHORT"}[pos.PositionAmt > 0],
			"amount":      pos.PositionAmt,
			"entry_price": pos.EntryPrice,
			"mark_price":  pos.MarkPrice,
			"pnl":         pos.UnrealizedProfit,
			"pnl_percent": roePct, // ROE % (with leverage) to match Binance
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

	// Get noise zone config from strategy
	noiseZoneLower := -1.5 // default
	noiseZoneUpper := 1.5  // default
	if e.strategy != nil {
		rc := e.strategy.Config.RiskControl
		if rc.NoiseZoneLowerBound != 0 {
			noiseZoneLower = rc.NoiseZoneLowerBound
		}
		if rc.NoiseZoneUpperBound != 0 {
			noiseZoneUpper = rc.NoiseZoneUpperBound
		}
	}

	// Fetch market intelligence (uses caching, won't hit APIs on every call)
	// Only fetch if enabled in strategy settings
	var intelFormatted string
	if e.intelProvider != nil && e.strategy != nil && e.strategy.Config.EnableMarketIntel {
		// Get trading symbols for intel fetching
		symbols := e.getTradingPairs()

		// Fetch intel with timeout
		intelCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		marketIntel, err := e.intelProvider.GetMarketIntel(intelCtx, symbols)
		if err != nil {
			log.Printf("[Intel] Failed to fetch market intelligence: %v", err)
		} else if marketIntel != nil {
			intelFormatted = intel.FormatForAI(marketIntel, symbols, 5)
		}
	}

	return &decision.Context{
		CurrentTime:          time.Now().Format(time.RFC3339),
		RuntimeMinutes:       int(time.Since(e.startTime).Minutes()),
		CallCount:            e.callCount,
		Account:              accountInfo,
		Positions:            positions,
		CandidateCoins:       candidateCoins,
		BTCETHLeverage:       btcEthLeverage,
		AltcoinLeverage:      altcoinLeverage,
		BTCETHPosRatio:       btcEthPosRatio,
		AltcoinPosRatio:      altcoinPosRatio,
		NoiseZoneLowerBound:  noiseZoneLower,
		NoiseZoneUpperBound:  noiseZoneUpper,
		MarketIntelFormatted: intelFormatted,
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

// normalizeActionDirection converts various action strings to a normalized direction
// This allows comparing actions like "BUY" and "LONG" as equivalent (both are bullish)
func normalizeActionDirection(action string) string {
	switch action {
	case "BUY", "LONG", "open_long":
		return "bullish"
	case "SELL", "SHORT", "open_short":
		return "bearish"
	case "CLOSE", "close_long", "close_short":
		return "close"
	default:
		return "hold" // HOLD, WAIT, or unknown actions
	}
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
	// For altcoins, default to 10x (20x is too high and often invalid)
	return 10
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

	minSize := e.getMinPositionSize(symbol)

	log.Printf("[%s] enforceMinPositionSize: positionSize=$%.2f, minRequired=$%.2f for %s",
		e.name, positionSizeUSD, minSize, symbol)

	if positionSizeUSD < minSize {
		return fmt.Errorf("position size $%.2f below minimum $%.2f for %s", positionSizeUSD, minSize, symbol)
	}

	return nil
}

// getMinPositionSize returns the minimum position size for a symbol
func (e *Engine) getMinPositionSize(symbol string) float64 {
	if e.strategy == nil {
		return 10.0 // Default fallback
	}

	rc := e.strategy.Config.RiskControl
	var minSize float64

	if isBTCETH(symbol) {
		minSize = rc.MinPositionSizeBTCETH
		if minSize <= 0 {
			minSize = rc.MinPositionUSD
		}
		if minSize <= 0 {
			minSize = 50.0 // BTC/ETH default
		}
	} else {
		minSize = rc.MinPositionSize
		if minSize <= 0 {
			minSize = rc.MinPositionUSD
		}
		if minSize <= 0 {
			minSize = 12.0 // Altcoin default
		}
	}

	log.Printf("[DEBUG] getMinPositionSize(%s): MinPositionSize=%.2f, MinPositionSizeBTCETH=%.2f, MinPositionUSD=%.2f → result=%.2f",
		symbol, rc.MinPositionSize, rc.MinPositionSizeBTCETH, rc.MinPositionUSD, minSize)

	return minSize
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
// When TrustAIForTPSL is enabled, AI suggestions are respected with only min/max floors
func (e *Engine) getSLTPPercentages(decision *ai.TradingDecision) (slPct, tpPct float64) {
	// Use AI-suggested values
	slPct = decision.StopLossPct
	tpPct = decision.TakeProfitPct

	// Get config values (with sensible defaults)
	var minTP, minSL, maxSL float64 = 3.0, 2.0, 5.0
	var trustAI bool = false
	var enforceRR bool = true

	if e.strategy != nil {
		rc := e.strategy.Config.RiskControl
		trustAI = rc.TrustAIForTPSL

		if rc.MinTPPercent > 0 {
			minTP = rc.MinTPPercent
		}
		if rc.MinSLPercent > 0 {
			minSL = rc.MinSLPercent
		}
		if rc.MaxSLPercent > 0 {
			maxSL = rc.MaxSLPercent
		}
		// R:R enforcement can be disabled by setting to 0
		if rc.MinRiskRewardRatio <= 0 {
			enforceRR = false
		}
	}

	// HYBRID MODE: Trust AI suggestions with minimal constraints
	if trustAI {
		log.Printf("[SL/TP] 🤖 HYBRID MODE: Trusting AI suggestions with floors (minTP=%.1f%%, minSL=%.1f%%, maxSL=%.1f%%)",
			minTP, minSL, maxSL)

		// Apply minimum TP floor
		if tpPct <= 0 {
			tpPct = minTP
			log.Printf("[SL/TP] AI didn't suggest TP, using minimum floor %.1f%%", minTP)
		} else if tpPct < minTP {
			log.Printf("[SL/TP] AI suggested TP=%.1f%% below floor, raising to %.1f%%", tpPct, minTP)
			tpPct = minTP
		} else {
			log.Printf("[SL/TP] ✅ Using AI's TP suggestion: %.1f%%", tpPct)
		}

		// Apply SL floors and ceiling
		if slPct <= 0 {
			slPct = minSL
			log.Printf("[SL/TP] AI didn't suggest SL, using minimum floor %.1f%%", minSL)
		} else if slPct < minSL {
			log.Printf("[SL/TP] AI suggested SL=%.1f%% below floor, raising to %.1f%%", slPct, minSL)
			slPct = minSL
		} else if slPct > maxSL {
			log.Printf("[SL/TP] AI suggested SL=%.1f%% above ceiling, capping to %.1f%%", slPct, maxSL)
			slPct = maxSL
		} else {
			log.Printf("[SL/TP] ✅ Using AI's SL suggestion: %.1f%%", slPct)
		}

		// Log final R:R for info (but don't enforce in hybrid mode)
		ratio := tpPct / slPct
		log.Printf("[SL/TP] 📊 Final: SL=%.1f%%, TP=%.1f%%, R:R=%.2f:1 (AI-driven)", slPct, tpPct, ratio)

		return slPct, tpPct
	}

	// LEGACY MODE: Strict enforcement with auto-adjustments
	// Only apply defaults if values are missing or clearly invalid
	if slPct <= 0 {
		// Default SL widened from 2% to 3.5% for volatile coins
		slPct = 3.5
		log.Printf("[SL/TP] No SL provided, using default %.1f%% (widened for volatility)", slPct)
	} else if slPct > 10 {
		log.Printf("[SL/TP] WARNING: SL=%.1f%% exceeds 10%%, trade will be validated", slPct)
	} else if slPct < 3.0 {
		// Floor at 3% minimum for volatile altcoins - 2% gets stopped by noise too often
		log.Printf("[SL/TP] ⚠️ AI set SL=%.1f%% which is too tight for volatile coins, raising to 3%% minimum", slPct)
		slPct = 3.0
	}

	if tpPct <= 0 {
		// Default TP of 10.5% maintains 3:1 R:R with 3.5% SL
		tpPct = 10.5
		log.Printf("[SL/TP] No TP provided, using default %.1f%% (3:1 R:R)", tpPct)
	} else if tpPct > 30 {
		log.Printf("[SL/TP] WARNING: TP=%.1f%% exceeds 30%%, trade will be validated", tpPct)
	}

	// Ensure R:R is maintained when we override SL (only if R:R enforcement is enabled)
	if enforceRR {
		minRatio := e.strategy.Config.RiskControl.MinRiskRewardRatio
		if minRatio <= 0 {
			minRatio = 3.0
		}

		ratio := tpPct / slPct
		if ratio < minRatio && slPct >= 3.0 {
			// If we raised SL, also raise TP to maintain R:R
			newTP := slPct * minRatio
			if newTP > tpPct {
				log.Printf("[SL/TP] Adjusting TP from %.1f%% to %.1f%% to maintain %.1f:1 R:R with SL=%.1f%%", tpPct, newTP, minRatio, slPct)
				tpPct = newTP
			}
		}

		if ratio < 1.0 {
			log.Printf("[SL/TP] WARNING: Poor R:R ratio %.2f:1 (SL=%.1f%%, TP=%.1f%%) - will be rejected by validator", ratio, slPct, tpPct)
		}
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
		// Check if position still exists before attempting SL placement
		// This prevents -4509 errors when position was closed between attempts
		positions, posErr := e.binance.GetPositions(ctx)
		if posErr == nil {
			positionExists := false
			for _, pos := range positions {
				if pos.Symbol == symbol && pos.PositionAmt != 0 {
					positionExists = true
					break
				}
			}
			if !positionExists {
				log.Printf("[%s][%s] Position no longer exists, skipping SL placement", e.name, symbol)
				return
			}
		}

		slOrder, err = e.binance.PlaceStopLoss(ctx, symbol, closeSide, 0, slPrice)
		if err == nil {
			break
		}

		// Check if error is specifically about position not existing
		if strings.Contains(err.Error(), "-4509") || strings.Contains(err.Error(), "open positions") {
			log.Printf("[%s][%s] Position was closed, stopping SL retry (error: %v)", e.name, symbol, err)
			return
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

// CancelTradingPause clears the trading pause, allowing trading to resume immediately
// IMPORTANT: This also resets the daily loss counter (sets new initial balance)
// to prevent immediate re-triggering of the pause if the balance is still low.
func (e *Engine) CancelTradingPause() {
	e.mu.Lock()
	waspaused := !e.stopUntil.IsZero() && time.Now().Before(e.stopUntil)
	e.stopUntil = time.Time{}

	// Reset daily loss baseline if we were paused
	// This effectively "acknowledges" the loss and starts fresh from current balance
	if waspaused && e.account != nil && e.account.TotalMarginBalance > 0 {
		oldInitial := e.initialBalance
		e.initialBalance = e.account.TotalMarginBalance
		e.lastResetTime = time.Now()
		log.Printf("[%s] 🔄 Daily loss reset on resume: Base balance updated $%.2f → $%.2f",
			e.name, oldInitial, e.initialBalance)
	}
	e.mu.Unlock()

	// Clear persisted state
	e.saveDailyLossState()

	if waspaused {
		log.Printf("[%s] ✅ Trading pause cancelled - resuming trading", e.name)
	}
}

// GetPauseStatus returns current pause status
func (e *Engine) GetPauseStatus() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	isPaused := !e.stopUntil.IsZero() && time.Now().Before(e.stopUntil)
	result := map[string]interface{}{
		"is_paused": isPaused,
	}

	if isPaused {
		result["pause_until"] = e.stopUntil.Format(time.RFC3339)
		result["remaining_seconds"] = int(time.Until(e.stopUntil).Seconds())
	}

	return result
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

	// Persist the pause state so it survives restarts
	e.saveDailyLossState()

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

		// Persist the reset state
		go e.saveDailyLossState()

		log.Printf("[%s] Daily P&L reset. New initial balance: $%.2f", e.name, e.initialBalance)
	}
}

// saveDailyLossState persists the daily loss tracking state to database
// This allows the state to survive bot restarts
func (e *Engine) saveDailyLossState() {
	e.mu.RLock()
	state := &store.DailyLossState{
		TraderID:       e.id,
		StopUntil:      e.stopUntil,
		InitialBalance: e.initialBalance,
		LastResetTime:  e.lastResetTime,
	}
	e.mu.RUnlock()

	if err := e.settingsStore.SaveDailyLossState(state); err != nil {
		log.Printf("[%s] Warning: failed to persist daily loss state: %v", e.name, err)
	}
}

// =============================================================================
// Drawdown Monitor Goroutine
// =============================================================================

// startDrawdownMonitor starts background goroutine for drawdown checks
// Runs every 5 seconds (was 10) to catch fast profit spikes on volatile altcoins
func (e *Engine) startDrawdownMonitor(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	log.Printf("[%s] Drawdown monitor started (5s interval)", e.name)

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
	if rc.EnableGuaranteedProfit {
		features = append(features, fmt.Sprintf("GuaranteedProfit(activate=%.1f%%, min=%.1f%%)", rc.GuaranteedProfitActivatePct, rc.GuaranteedMinProfitPct))
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

// shouldAttemptClose checks if we should attempt to close a position
// Returns false if we recently attempted to close this symbol (within 2 seconds)
// This prevents rate limiting errors from multiple WebSocket updates
func (e *Engine) shouldAttemptClose(symbol string) bool {
	e.closeAttemptMu.Lock()
	defer e.closeAttemptMu.Unlock()

	last, exists := e.lastCloseAttempt[symbol]
	if exists && time.Since(last) < 2*time.Second {
		return false // Recently attempted, skip to avoid rate limit
	}
	e.lastCloseAttempt[symbol] = time.Now()
	return true
}

// checkPositionDrawdown checks if any positions should be closed due to drawdown
func (e *Engine) checkPositionDrawdown(ctx context.Context) {
	// Skip if already checking (prevents duplicate work from concurrent calls)
	if !e.isCheckingDrawdown.CompareAndSwap(false, true) {
		return
	}
	defer e.isCheckingDrawdown.Store(false)

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

		// Skip positions with invalid mark price - this can happen when WebSocket
		// sends ACCOUNT_UPDATE without mark price, causing false 100% PnL calculation
		if pos.MarkPrice <= 0 || pos.EntryPrice <= 0 {
			log.Printf("[%s][%s] Skipping risk check: invalid prices (entry=$%.4f, mark=$%.4f)",
				e.name, pos.Symbol, pos.EntryPrice, pos.MarkPrice)
			continue
		}

		// Calculate P&L percentages
		// OPTION B: Split Logic
		// 1. rawPnlPct (Price Move): Used for Smart Loss (don't cut on noise)
		// 2. roePnlPct (Equity Move): Used for Trailing Stop & Drawdown (protect actual equity)
		var rawPnlPct float64
		var roePnlPct float64

		if pos.EntryPrice > 0 {
			if pos.PositionAmt > 0 {
				rawPnlPct = ((pos.MarkPrice - pos.EntryPrice) / pos.EntryPrice) * 100
			} else {
				rawPnlPct = ((pos.EntryPrice - pos.MarkPrice) / pos.EntryPrice) * 100
			}

			// Apply leverage to get ROE
			leverage := float64(pos.Leverage)
			if leverage < 1 {
				leverage = 1
			}
			roePnlPct = rawPnlPct * leverage
		}

		side := "LONG"
		if pos.PositionAmt < 0 {
			side = "SHORT"
		}

		holdDuration := e.GetHoldDuration(pos.Symbol, side)

		// =====================================================================
		// 1. TRAILING STOP LOSS - Lock in profits (Uses Raw % - same scale as Noise Zone)
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

			// Update and get peak P&L (Using Raw % - consistent with Noise Zone)
			e.UpdatePeakPnL(pos.Symbol, side, rawPnlPct)
			peakPnL := e.GetPeakPnL(pos.Symbol, side)

			// Check if trailing stop should activate
			// If activatePct is 0 or negative, activate immediately from entry
			if activatePct <= 0 || peakPnL >= activatePct {
				// Calculate trailing stop level
				trailingStopLevel := peakPnL - trailDistPct

				if rawPnlPct <= trailingStopLevel {
					log.Printf("[%s][%s] 📉 TRAILING STOP TRIGGERED: Peak=%.2f%%, Current=%.2f%%, TrailStop=%.2f%% (Raw)",
						e.name, pos.Symbol, peakPnL, rawPnlPct, trailingStopLevel)

					// Rate limit: only attempt close if we haven't tried in last 2 seconds
					if !e.shouldAttemptClose(pos.Symbol) {
						log.Printf("[%s][%s] Skipping close attempt (rate limited)", e.name, pos.Symbol)
						continue
					}

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
		// 1.5. GUARANTEED MINIMUM PROFIT - Lock in minimum profit once threshold reached
		// =====================================================================
		if rc.EnableGuaranteedProfit {
			activatePct := rc.GuaranteedProfitActivatePct
			if activatePct <= 0 {
				activatePct = 0.3 // Default: activate when position reaches 0.3% profit
			}
			minProfitPct := rc.GuaranteedMinProfitPct
			if minProfitPct < 0 {
				minProfitPct = 0.1 // Default: guarantee at least 0.1% profit
			}

			// Always update peak P&L when Guaranteed Profit is enabled
			e.UpdatePeakPnL(pos.Symbol, side, rawPnlPct)
			peakPnL := e.GetPeakPnL(pos.Symbol, side)

			// Debug logging for profit tracking (log every 30 seconds to avoid spam)
			// Throttled to prevent log flooding when WebSocket updates trigger frequent checks
			if rawPnlPct > 0.1 || peakPnL > activatePct*0.5 {
				e.closeAttemptMu.Lock()
				// Lazy init for tests that might construct Engine manually
				if e.lastLogTime == nil {
					e.lastLogTime = make(map[string]time.Time)
				}
				lastLog, exists := e.lastLogTime[pos.Symbol]
				shouldLog := !exists || time.Since(lastLog) > 30*time.Second
				if shouldLog {
					e.lastLogTime[pos.Symbol] = time.Now()
				}
				e.closeAttemptMu.Unlock()

				if shouldLog {
					log.Printf("[%s][%s] 📊 Guaranteed Profit Status: Current=%.2f%%, Peak=%.2f%%, ActivateAt=%.2f%%, MinLock=%.2f%%",
						e.name, pos.Symbol, rawPnlPct, peakPnL, activatePct, minProfitPct)
				}
			}

			// Check if position ever reached activation threshold
			if peakPnL >= activatePct {
				// Position qualified for guaranteed profit - close if dropping to minimum
				if rawPnlPct <= minProfitPct {
					log.Printf("[%s][%s] 🔒 GUARANTEED PROFIT TRIGGERED: Peak=%.2f%%, Current=%.2f%%, MinGuarantee=%.2f%% (Raw)",
						e.name, pos.Symbol, peakPnL, rawPnlPct, minProfitPct)

					// Rate limit: only attempt close if we haven't tried in last 2 seconds
					if !e.shouldAttemptClose(pos.Symbol) {
						log.Printf("[%s][%s] Skipping close attempt (rate limited)", e.name, pos.Symbol)
						continue
					}

					if _, err := e.binance.ClosePosition(ctx, pos.Symbol, pos.PositionAmt); err != nil {
						log.Printf("[%s][%s] Failed to close position (guaranteed profit): %v", e.name, pos.Symbol, err)
					} else {
						log.Printf("[%s][%s] ✅ Closed position via guaranteed profit. Locked in %.2f%% profit.", e.name, pos.Symbol, rawPnlPct)
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

				// Rate limit: only attempt close if we haven't tried in last 2 seconds
				if !e.shouldAttemptClose(pos.Symbol) {
					log.Printf("[%s][%s] Skipping close attempt (rate limited)", e.name, pos.Symbol)
					continue
				}

				if _, err := e.binance.ClosePosition(ctx, pos.Symbol, pos.PositionAmt); err != nil {
					log.Printf("[%s][%s] Failed to close position (max hold): %v", e.name, pos.Symbol, err)
				} else {
					log.Printf("[%s][%s] ✅ Closed position due to max hold duration. PnL: %.2f%% (Raw)", e.name, pos.Symbol, rawPnlPct)
					e.clearPositionTracking(pos.Symbol, side)
					e.cancelBracketOrders(ctx, pos.Symbol)
				}
				continue // Move to next position
			}
		}

		// =====================================================================
		// 3. SMART LOSS CUT - Cut positions underwater too long (Uses RAW %)
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
			// USES RAW P&L: We want to allow -1% Raw (approx -20% ROE) before giving up
			// If we used ROE, -1% would trigger almost instantly on any noise
			if rawPnlPct <= smartLossPct && holdDuration >= smartLossDuration {
				log.Printf("[%s][%s] 🔪 SMART LOSS CUT: Position at %.2f%% Raw (ROE: %.2f%%) < %.2f%% for %v. Cutting losses.",
					e.name, pos.Symbol, rawPnlPct, roePnlPct, smartLossPct, holdDuration.Round(time.Minute))

				// Rate limit: only attempt close if we haven't tried in last 2 seconds
				if !e.shouldAttemptClose(pos.Symbol) {
					log.Printf("[%s][%s] Skipping close attempt (rate limited)", e.name, pos.Symbol)
					continue
				}

				if _, err := e.binance.ClosePosition(ctx, pos.Symbol, pos.PositionAmt); err != nil {
					log.Printf("[%s][%s] Failed to close position (smart loss cut): %v", e.name, pos.Symbol, err)
				} else {
					log.Printf("[%s][%s] ✅ Cut losing position. Loss: %.2f%% (Raw)", e.name, pos.Symbol, rawPnlPct)
					e.clearPositionTracking(pos.Symbol, side)
					e.cancelBracketOrders(ctx, pos.Symbol)
				}
				continue // Move to next position
			}
		}

		// =====================================================================
		// 4. DRAWDOWN PROTECTION (Uses Raw % - same scale as Noise Zone & Trailing Stop)
		// =====================================================================
		// In Simple Mode, we skip ONLY this automatic drawdown protection
		// (features 1-3 above are explicitly enabled by user, so they still run)
		if isSimpleMode {
			continue
		}

		// Update peak P&L (Using Raw % - consistent with other features)
		e.UpdatePeakPnL(pos.Symbol, side, rawPnlPct)
		peakPnL := e.GetPeakPnL(pos.Symbol, side)

		// Only apply drawdown protection if we were profitable (in Raw % terms)
		if peakPnL < minProfitForDrawdown {
			continue
		}

		// Calculate drawdown from peak (relative percentage, matching NOFX)
		// Using Raw % values
		var drawdownPct float64
		if peakPnL > 0 && rawPnlPct < peakPnL {
			drawdownPct = ((peakPnL - rawPnlPct) / peakPnL) * 100
		}

		if drawdownPct >= drawdownThreshold {
			log.Printf("[%s][%s] Drawdown alert: Peak=%.2f%%, Current=%.2f%%, Drawdown=%.2f%% >= %.2f%% (Raw)",
				e.name, pos.Symbol, peakPnL, rawPnlPct, drawdownPct, drawdownThreshold)

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
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	log.Printf("[%s] Position sync started (5s interval)", e.name)

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

// handleWebSocketUpdates processes real-time position updates from WebSocket
// This enables <1s latency for risk checks vs 30-60s with REST polling
func (e *Engine) handleWebSocketUpdates(ctx context.Context) {
	log.Printf("[%s] WebSocket update handler started", e.name)

	for {
		select {
		case <-e.stopCh:
			log.Printf("[%s] WebSocket update handler stopped", e.name)
			return
		case <-ctx.Done():
			return
		case update := <-e.wsUpdateCh:
			if update == nil {
				continue
			}

			// Update position map with mutex lock
			e.mu.Lock()
			triggerRiskCheck := false

			// CASE 1: MARKET PRICE TICK (Real-time mark price for risk checks)
			if update.Type == "MARK_PRICE" {
				if pos, exists := e.positions[update.Symbol]; exists {
					// Update MarkPrice only - PnL comes from Binance REST API (5s polling)
					// This ensures displayed PnL matches Binance UI exactly
					pos.MarkPrice = update.MarkPrice

					// Log mark price updates periodically (every 30s) for debugging
					if time.Since(e.lastMarkPriceLog) > 30*time.Second {
						e.lastMarkPriceLog = time.Now()
						log.Printf("[%s] 📡 WebSocket mark price: %s @ $%.8f | Entry: $%.8f | PnL: $%.2f",
							e.name, update.Symbol, update.MarkPrice, pos.EntryPrice, pos.UnrealizedProfit)
					}

					// Trigger risk check (uses real-time mark price for SL/TP)
					triggerRiskCheck = true
				}
			} else {
				// CASE 2: POSITION UPDATE (Trade/Order Fill)
				if update.PositionAmt == 0 {
					// Position closed
					delete(e.positions, update.Symbol)

					// Unsubscribe from market data to save bandwidth
					go e.binance.UnsubscribeFromMarkPrice(update.Symbol)
				} else {
					// New or Updated Position
					existingPos := e.positions[update.Symbol]
					isNew := existingPos == nil

					// Determine MarkPrice to use
					markPrice := update.MarkPrice

					// 1. Try to back-calculate from Unrealized PnL if MarkPrice is missing
					if markPrice == 0 && update.PositionAmt != 0 && update.EntryPrice > 0 {
						markPrice = (update.UnrealizedPnL / update.PositionAmt) + update.EntryPrice
						update.MarkPrice = markPrice
					}

					// 2. Fallback to existing
					if markPrice == 0 && existingPos != nil && existingPos.MarkPrice > 0 {
						markPrice = existingPos.MarkPrice
					}

					// Determine Leverage to use
					// WebSocket ACCOUNT_UPDATE doesn't include leverage, so we need fallbacks
					leverage := update.Leverage
					if leverage == 0 && existingPos != nil && existingPos.Leverage > 0 {
						leverage = existingPos.Leverage
					}
					if leverage == 0 {
						// Ultimate fallback: use engine's configured leverage for this symbol
						leverage = e.getLeverageLimit(update.Symbol)
					}

					// Update position
					e.positions[update.Symbol] = &exchange.Position{
						Symbol:           update.Symbol,
						PositionSide:     update.PositionSide,
						PositionAmt:      update.PositionAmt,
						EntryPrice:       update.EntryPrice,
						MarkPrice:        markPrice,
						UnrealizedProfit: update.UnrealizedPnL,
						Leverage:         leverage,
					}

					// If new position, subscribe to real-time market data
					if isNew {
						go e.binance.SubscribeToMarkPrice(update.Symbol)
					}

					triggerRiskCheck = true
				}
			}
			e.mu.Unlock()

			// Trigger risk check if needed
			if triggerRiskCheck {
				go e.checkPositionDrawdown(ctx)
			}

			// Broadcast to UI via event hub for real-time updates
			e.notifier.Broadcast(events.Event{
				Type:    events.TypeInfo,
				Message: fmt.Sprintf("Position updated: %s", update.Symbol),
			})
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
	var symbolsToSubscribe []string
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

			// Ensure we are subscribed to real-time mark price
			symbolsToSubscribe = append(symbolsToSubscribe, pos.Symbol)
		}
	}

	// Batch subscribe to avoid rate limits
	if len(symbolsToSubscribe) > 0 {
		go e.binance.SubscribeToMarkPrices(symbolsToSubscribe)
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

				// Mark position as closed in local state so UI reflects the change
				oldPos.PositionAmt = 0

				// Unsubscribe from market data
				go e.binance.UnsubscribeFromMarkPrice(symbol)
			}
		}
	}

	// MERGE positions instead of replacing to prevent race conditions:
	// If executeTrade() just opened a position and added it to e.positions,
	// we don't want to lose that update. Binance data is authoritative for
	// existing positions, but we preserve local state for positions not yet
	// visible on exchange (due to API latency).
	//
	// Use Binance data directly - it's authoritative and matches Binance UI.
	// WebSocket mark price updates will provide real-time updates BETWEEN REST fetches.
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
