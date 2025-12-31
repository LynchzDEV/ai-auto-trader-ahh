package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"auto-trader-ahh/backtest"
	"auto-trader-ahh/config"
	"auto-trader-ahh/debate"
	"auto-trader-ahh/decision"
	"auto-trader-ahh/exchange"
	"auto-trader-ahh/mcp"
	"auto-trader-ahh/store"
	"auto-trader-ahh/trader"
)

type Server struct {
	port            string
	strategyStore   *store.StrategyStore
	traderStore     *store.TraderStore
	decisionStore   *store.DecisionStore
	equityStore     *store.EquityStore
	engineManager   *trader.EngineManager
	debateEngine    *debate.Engine
	backtestManager *backtest.Manager
	aiClient        mcp.AIClient
	binanceClient   *exchange.BinanceClient
}

func NewServer(port string, em *trader.EngineManager, cfg *config.Config) *Server {
	// Create OpenRouter AI client
	aiClient := mcp.NewOpenRouterClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel)

	// Create Binance client for backtest
	binanceClient := exchange.NewBinanceClient(cfg.BinanceAPIKey, cfg.BinanceSecretKey, cfg.BinanceTestnet)

	// Create debate engine and register AI client
	debateEng := debate.NewEngine()
	debateEng.RegisterClient("openrouter", aiClient)
	debateEng.RegisterClient("openai", aiClient)     // Use OpenRouter for all providers
	debateEng.RegisterClient("anthropic", aiClient)  // OpenRouter supports these models
	debateEng.RegisterClient("deepseek", aiClient)

	return &Server{
		port:            port,
		strategyStore:   store.NewStrategyStore(),
		traderStore:     store.NewTraderStore(),
		decisionStore:   store.NewDecisionStore(),
		equityStore:     store.NewEquityStore(),
		engineManager:   em,
		debateEngine:    debateEng,
		backtestManager: backtest.NewManager(aiClient, binanceClient),
		aiClient:        aiClient,
		binanceClient:   binanceClient,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/api/health", s.handleHealth)

	// Strategy endpoints
	mux.HandleFunc("/api/strategies", s.handleStrategies)
	mux.HandleFunc("/api/strategies/", s.handleStrategy)
	mux.HandleFunc("/api/strategies/active", s.handleActiveStrategy)
	mux.HandleFunc("/api/strategies/default-config", s.handleDefaultConfig)

	// Trader endpoints
	mux.HandleFunc("/api/traders", s.handleTraders)
	mux.HandleFunc("/api/traders/", s.handleTrader)

	// Data endpoints
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/account", s.handleAccount)
	mux.HandleFunc("/api/positions", s.handlePositions)
	mux.HandleFunc("/api/decisions", s.handleDecisions)
	mux.HandleFunc("/api/equity-history", s.handleEquityHistory)

	// Backtest endpoints
	mux.HandleFunc("/api/backtest", s.handleBacktests)
	mux.HandleFunc("/api/backtest/start", s.handleBacktestStart)
	mux.HandleFunc("/api/backtest/", s.handleBacktest)

	// Debate endpoints
	mux.HandleFunc("/api/debate/sessions", s.handleDebateSessions)
	mux.HandleFunc("/api/debate/sessions/", s.handleDebateSession)

	// Wrap with CORS middleware
	handler := corsMiddleware(mux)

	log.Printf("API server starting at http://localhost:%s", s.port)
	return http.ListenAndServe(":"+s.port, handler)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (s *Server) errorResponse(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// Health check
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, map[string]interface{}{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

// ============ STRATEGY ENDPOINTS ============

func (s *Server) handleStrategies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		strategies, err := s.strategyStore.List()
		if err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.jsonResponse(w, map[string]interface{}{"strategies": strategies})

	case "POST":
		var strategy store.Strategy
		if err := json.NewDecoder(r.Body).Decode(&strategy); err != nil {
			s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		if err := s.strategyStore.Create(&strategy); err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.jsonResponse(w, strategy)

	default:
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleStrategy(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: /api/strategies/{id}
	id := r.URL.Path[len("/api/strategies/"):]
	if id == "" {
		s.errorResponse(w, http.StatusBadRequest, "Strategy ID required")
		return
	}

	// Handle activate endpoint
	if len(id) > 9 && id[len(id)-9:] == "/activate" {
		if r.Method == "POST" {
			stratID := id[:len(id)-9]
			if err := s.strategyStore.SetActive(stratID); err != nil {
				s.errorResponse(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.jsonResponse(w, map[string]string{"status": "activated"})
		}
		return
	}

	switch r.Method {
	case "GET":
		strategy, err := s.strategyStore.Get(id)
		if err != nil {
			s.errorResponse(w, http.StatusNotFound, "Strategy not found")
			return
		}
		s.jsonResponse(w, strategy)

	case "PUT":
		var strategy store.Strategy
		if err := json.NewDecoder(r.Body).Decode(&strategy); err != nil {
			s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		strategy.ID = id
		if err := s.strategyStore.Update(&strategy); err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.jsonResponse(w, strategy)

	case "DELETE":
		if err := s.strategyStore.Delete(id); err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.jsonResponse(w, map[string]string{"status": "deleted"})

	default:
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleActiveStrategy(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	strategy, err := s.strategyStore.GetActive()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.jsonResponse(w, strategy)
}

func (s *Server) handleDefaultConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	s.jsonResponse(w, store.DefaultStrategyConfig())
}

// ============ TRADER ENDPOINTS ============

func (s *Server) handleTraders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		traders, err := s.traderStore.List()
		if err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Enhance with runtime status
		result := make([]map[string]interface{}, len(traders))
		for i, t := range traders {
			result[i] = map[string]interface{}{
				"id":              t.ID,
				"name":            t.Name,
				"strategy_id":     t.StrategyID,
				"exchange":        t.Exchange,
				"status":          t.Status,
				"initial_balance": t.InitialBalance,
				"created_at":      t.CreatedAt,
				"is_running":      s.engineManager.IsRunning(t.ID),
			}
		}
		s.jsonResponse(w, map[string]interface{}{"traders": result})

	case "POST":
		var trader store.Trader
		if err := json.NewDecoder(r.Body).Decode(&trader); err != nil {
			s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		if err := s.traderStore.Create(&trader); err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.jsonResponse(w, trader)

	default:
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleTrader(w http.ResponseWriter, r *http.Request) {
	// Extract path parts: /api/traders/{id} or /api/traders/{id}/action
	path := r.URL.Path[len("/api/traders/"):]
	parts := splitPath(path)

	if len(parts) == 0 {
		s.errorResponse(w, http.StatusBadRequest, "Trader ID required")
		return
	}

	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	// Handle actions
	if action != "" && r.Method == "POST" {
		switch action {
		case "start":
			if err := s.engineManager.Start(id); err != nil {
				s.errorResponse(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.traderStore.UpdateStatus(id, "running")
			s.jsonResponse(w, map[string]string{"status": "started"})

		case "stop":
			s.engineManager.Stop(id)
			s.traderStore.UpdateStatus(id, "stopped")
			s.jsonResponse(w, map[string]string{"status": "stopped"})

		default:
			s.errorResponse(w, http.StatusBadRequest, "Unknown action")
		}
		return
	}

	// Standard CRUD
	switch r.Method {
	case "GET":
		trader, err := s.traderStore.Get(id)
		if err != nil {
			s.errorResponse(w, http.StatusNotFound, "Trader not found")
			return
		}
		s.jsonResponse(w, trader)

	case "PUT":
		var trader store.Trader
		if err := json.NewDecoder(r.Body).Decode(&trader); err != nil {
			s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		trader.ID = id
		if err := s.traderStore.Update(&trader); err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.jsonResponse(w, trader)

	case "DELETE":
		s.engineManager.Stop(id) // Stop if running
		if err := s.traderStore.Delete(id); err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.jsonResponse(w, map[string]string{"status": "deleted"})

	default:
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// ============ DATA ENDPOINTS ============

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	traderID := r.URL.Query().Get("trader_id")
	if traderID == "" {
		s.errorResponse(w, http.StatusBadRequest, "trader_id required")
		return
	}

	status := s.engineManager.GetStatus(traderID)
	s.jsonResponse(w, status)
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	traderID := r.URL.Query().Get("trader_id")
	if traderID == "" {
		s.errorResponse(w, http.StatusBadRequest, "trader_id required")
		return
	}

	account := s.engineManager.GetAccount(traderID)
	s.jsonResponse(w, account)
}

func (s *Server) handlePositions(w http.ResponseWriter, r *http.Request) {
	traderID := r.URL.Query().Get("trader_id")
	if traderID == "" {
		s.errorResponse(w, http.StatusBadRequest, "trader_id required")
		return
	}

	positions := s.engineManager.GetPositions(traderID)
	s.jsonResponse(w, map[string]interface{}{"positions": positions})
}

func (s *Server) handleDecisions(w http.ResponseWriter, r *http.Request) {
	traderID := r.URL.Query().Get("trader_id")
	if traderID == "" {
		s.errorResponse(w, http.StatusBadRequest, "trader_id required")
		return
	}

	decisions, err := s.decisionStore.ListByTrader(traderID, 50)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.jsonResponse(w, map[string]interface{}{"decisions": decisions})
}

func (s *Server) handleEquityHistory(w http.ResponseWriter, r *http.Request) {
	traderID := r.URL.Query().Get("trader_id")
	if traderID == "" {
		s.errorResponse(w, http.StatusBadRequest, "trader_id required")
		return
	}

	snapshots, err := s.equityStore.GetLatest(traderID, 1000)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.jsonResponse(w, map[string]interface{}{"history": snapshots})
}

func splitPath(path string) []string {
	var parts []string
	current := ""
	for _, c := range path {
		if c == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// ============ BACKTEST ENDPOINTS ============

func (s *Server) handleBacktests(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	runs := s.backtestManager.ListRuns()
	s.jsonResponse(w, map[string]interface{}{"backtests": runs})
}

func (s *Server) handleBacktestStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var cfg backtest.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := cfg.Validate(); err != nil {
		s.errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	runID, err := s.backtestManager.Start(context.Background(), &cfg)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.jsonResponse(w, map[string]string{"run_id": runID, "status": "started"})
}

func (s *Server) handleBacktest(w http.ResponseWriter, r *http.Request) {
	// Extract path: /api/backtest/{runId} or /api/backtest/{runId}/action
	path := r.URL.Path[len("/api/backtest/"):]
	parts := splitPath(path)

	if len(parts) == 0 {
		s.errorResponse(w, http.StatusBadRequest, "Run ID required")
		return
	}

	runID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch action {
	case "stop":
		if r.Method != "POST" {
			s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		if err := s.backtestManager.Stop(runID); err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.jsonResponse(w, map[string]string{"status": "stopped"})

	case "status":
		if r.Method != "GET" {
			s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		meta, err := s.backtestManager.GetStatus(runID)
		if err != nil {
			s.errorResponse(w, http.StatusNotFound, err.Error())
			return
		}
		s.jsonResponse(w, meta)

	case "metrics":
		if r.Method != "GET" {
			s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		metrics, err := s.backtestManager.GetMetrics(runID)
		if err != nil {
			s.errorResponse(w, http.StatusNotFound, err.Error())
			return
		}
		s.jsonResponse(w, metrics)

	case "equity":
		if r.Method != "GET" {
			s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		curve, err := s.backtestManager.GetEquityCurve(runID)
		if err != nil {
			s.errorResponse(w, http.StatusNotFound, err.Error())
			return
		}
		s.jsonResponse(w, map[string]interface{}{"equity_curve": curve})

	case "trades":
		if r.Method != "GET" {
			s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		trades, err := s.backtestManager.GetTrades(runID)
		if err != nil {
			s.errorResponse(w, http.StatusNotFound, err.Error())
			return
		}
		s.jsonResponse(w, map[string]interface{}{"trades": trades})

	case "":
		// No action - CRUD on run
		switch r.Method {
		case "GET":
			meta, err := s.backtestManager.GetStatus(runID)
			if err != nil {
				s.errorResponse(w, http.StatusNotFound, err.Error())
				return
			}
			s.jsonResponse(w, meta)

		case "DELETE":
			if err := s.backtestManager.Delete(runID); err != nil {
				s.errorResponse(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.jsonResponse(w, map[string]string{"status": "deleted"})

		default:
			s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		}

	default:
		s.errorResponse(w, http.StatusBadRequest, "Unknown action")
	}
}

// ============ DEBATE ENDPOINTS ============

func (s *Server) handleDebateSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		sessions := s.debateEngine.ListSessions()
		s.jsonResponse(w, map[string]interface{}{"sessions": sessions})

	case "POST":
		var req debate.CreateSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		session, err := s.debateEngine.CreateSession(&req)
		if err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}

		s.jsonResponse(w, session)

	default:
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleDebateSession(w http.ResponseWriter, r *http.Request) {
	// Extract path: /api/debate/sessions/{sessionId} or /api/debate/sessions/{sessionId}/action
	path := r.URL.Path[len("/api/debate/sessions/"):]
	parts := splitPath(path)

	if len(parts) == 0 {
		s.errorResponse(w, http.StatusBadRequest, "Session ID required")
		return
	}

	sessionID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch action {
	case "start":
		if r.Method != "POST" {
			s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		// Get session to retrieve symbols
		session, err := s.debateEngine.GetSession(sessionID)
		if err != nil {
			s.errorResponse(w, http.StatusNotFound, err.Error())
			return
		}

		// Build market context with real data
		marketCtx := s.buildDebateMarketContext(session.Symbols)

		if err := s.debateEngine.Start(context.Background(), sessionID, marketCtx); err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.jsonResponse(w, map[string]string{"status": "started"})

	case "stop":
		if r.Method != "POST" {
			s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		if err := s.debateEngine.Stop(sessionID); err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.jsonResponse(w, map[string]string{"status": "stopped"})

	case "":
		// No action - CRUD on session
		switch r.Method {
		case "GET":
			session, err := s.debateEngine.GetSession(sessionID)
			if err != nil {
				s.errorResponse(w, http.StatusNotFound, err.Error())
				return
			}
			s.jsonResponse(w, session)

		case "DELETE":
			// For now, just stop it
			s.debateEngine.Stop(sessionID)
			s.jsonResponse(w, map[string]string{"status": "deleted"})

		default:
			s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		}

	default:
		s.errorResponse(w, http.StatusBadRequest, "Unknown action")
	}
}

// buildDebateMarketContext fetches real market data and creates a simulated account for debate
func (s *Server) buildDebateMarketContext(symbols []string) *debate.MarketContext {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	marketData := make(map[string]*decision.MarketData)

	// Fetch market data for each symbol
	for _, symbol := range symbols {
		// Get ticker for current price
		ticker, err := s.binanceClient.GetTicker(ctx, symbol)
		if err != nil {
			log.Printf("Failed to get ticker for %s: %v", symbol, err)
			continue
		}

		// Get recent klines (5m, last 288 candles = ~24 hours for stats)
		klines, err := s.binanceClient.GetKlines(ctx, symbol, "5m", 288)
		if err != nil {
			log.Printf("Failed to get klines for %s: %v", symbol, err)
		}

		// Calculate 24h stats from klines
		var highPrice, lowPrice, volume24h, openPrice float64
		if len(klines) > 0 {
			openPrice = klines[0].Open
			highPrice = klines[0].High
			lowPrice = klines[0].Low
			for _, k := range klines {
				if k.High > highPrice {
					highPrice = k.High
				}
				if k.Low < lowPrice {
					lowPrice = k.Low
				}
				volume24h += k.Volume
			}
		}

		// Calculate 24h change percentage
		change24h := 0.0
		if openPrice > 0 {
			change24h = ((ticker.Price - openPrice) / openPrice) * 100
		}

		// Convert klines to decision.Kline format
		var decisionKlines []decision.Kline
		for _, k := range klines {
			decisionKlines = append(decisionKlines, decision.Kline{
				OpenTime:  k.OpenTime,
				Open:      k.Open,
				High:      k.High,
				Low:       k.Low,
				Close:     k.Close,
				Volume:    k.Volume,
				CloseTime: k.CloseTime,
			})
		}

		md := &decision.MarketData{
			Symbol:       symbol,
			Price:        ticker.Price,
			Change24h:    change24h,
			Volume24h:    volume24h,
			HighPrice24h: highPrice,
			LowPrice24h:  lowPrice,
			Timestamp:    time.Now(),
			Klines:       decisionKlines,
		}
		marketData[symbol] = md
	}

	// Create simulated account with $10,000 starting balance for debate purposes
	account := decision.AccountInfo{
		TotalEquity:      10000.0,
		AvailableBalance: 10000.0,
		UnrealizedPnL:    0,
		TotalPnL:         0,
		TotalPnLPct:      0,
		MarginUsed:       0,
		MarginUsedPct:    0,
		PositionCount:    0,
	}

	return &debate.MarketContext{
		CurrentTime: time.Now().Format(time.RFC3339),
		Account:     account,
		Positions:   []decision.PositionInfo{}, // No existing positions
		MarketData:  marketData,
	}
}
