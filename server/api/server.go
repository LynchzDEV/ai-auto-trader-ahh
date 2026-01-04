package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
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
	tradeStore      *store.TradeStore
	engineManager   *trader.EngineManager
	debateEngine    *debate.Engine
	backtestManager *backtest.Manager
	aiClient        mcp.AIClient
	binanceClient   *exchange.BinanceClient
	accessPasskey   string
}

func NewServer(port string, em *trader.EngineManager, cfg *config.Config) *Server {
	// Create OpenRouter AI client
	aiClient := mcp.NewOpenRouterClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel)

	// Create Binance client for backtest
	binanceClient := exchange.NewBinanceClient(cfg.BinanceAPIKey, cfg.BinanceSecretKey, cfg.BinanceTestnet)

	// Create debate engine and register AI client
	debateEng := debate.NewEngine()
	debateEng.RegisterClient("openrouter", aiClient)
	debateEng.RegisterClient("openai", aiClient)    // Use OpenRouter for all providers
	debateEng.RegisterClient("anthropic", aiClient) // OpenRouter supports these models
	debateEng.RegisterClient("deepseek", aiClient)

	equityStore := store.NewEquityStore()

	srv := &Server{
		port:            port,
		strategyStore:   store.NewStrategyStore(),
		traderStore:     store.NewTraderStore(),
		decisionStore:   store.NewDecisionStore(),
		equityStore:     equityStore,
		tradeStore:      store.NewTradeStore(),
		engineManager:   em,
		debateEngine:    debateEng,
		backtestManager: backtest.NewManager(aiClient, binanceClient),
		aiClient:        aiClient,
		binanceClient:   binanceClient,
		accessPasskey:   cfg.AccessPasskey,
	}

	// Wire up debate engine with market context provider and trade executor
	debateEng.SetMarketContextProvider(srv.buildDebateMarketContextForCycle)
	debateEng.SetTradeExecutor(srv.executeDebateDecisions)

	return srv
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Public endpoints (no auth required)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/auth/verify", s.handleAuthVerify)

	// Protected endpoints (auth required)
	// Strategy endpoints
	mux.HandleFunc("/api/strategies", s.authMiddleware(s.handleStrategies))
	mux.HandleFunc("/api/strategies/", s.authMiddleware(s.handleStrategy))
	mux.HandleFunc("/api/strategies/active", s.authMiddleware(s.handleActiveStrategy))
	mux.HandleFunc("/api/strategies/default-config", s.authMiddleware(s.handleDefaultConfig))

	// Trader endpoints
	mux.HandleFunc("/api/traders", s.authMiddleware(s.handleTraders))
	mux.HandleFunc("/api/traders/", s.authMiddleware(s.handleTrader))

	// Data endpoints
	mux.HandleFunc("/api/status", s.authMiddleware(s.handleStatus))
	mux.HandleFunc("/api/account", s.authMiddleware(s.handleAccount))
	mux.HandleFunc("/api/positions", s.authMiddleware(s.handlePositions))
	mux.HandleFunc("/api/decisions", s.authMiddleware(s.handleDecisions))
	mux.HandleFunc("/api/trades", s.authMiddleware(s.handleTrades))
	mux.HandleFunc("/api/equity-history", s.authMiddleware(s.handleEquityHistory))

	// Backtest endpoints
	mux.HandleFunc("/api/backtest", s.authMiddleware(s.handleBacktests))
	mux.HandleFunc("/api/backtest/start", s.authMiddleware(s.handleBacktestStart))
	mux.HandleFunc("/api/backtest/", s.authMiddleware(s.handleBacktest))

	// Debate endpoints
	mux.HandleFunc("/api/debate/sessions", s.authMiddleware(s.handleDebateSessions))
	mux.HandleFunc("/api/debate/sessions/", s.authMiddleware(s.handleDebateSession))

	// Wrap with CORS middleware
	handler := corsMiddleware(mux)

	log.Printf("API server starting at http://localhost:%s", s.port)
	if s.accessPasskey != "" {
		log.Printf("Authentication enabled - passkey required")
	} else {
		log.Printf("WARNING: No ACCESS_PASSKEY set - server is unprotected!")
	}
	return http.ListenAndServe(":"+s.port, handler)
}

// authMiddleware checks for valid passkey in X-Access-Key header
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if no passkey is configured
		if s.accessPasskey == "" {
			next(w, r)
			return
		}

		// Check X-Access-Key header
		accessKey := r.Header.Get("X-Access-Key")
		if accessKey == "" {
			s.errorResponse(w, http.StatusUnauthorized, "Access key required")
			return
		}

		// Use constant-time comparison to prevent timing attacks
		if !secureCompare(accessKey, s.accessPasskey) {
			s.errorResponse(w, http.StatusUnauthorized, "Invalid access key")
			return
		}

		next(w, r)
	}
}

// handleAuthVerify verifies the passkey and returns success/failure
func (s *Server) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// If no passkey is configured, always allow
	if s.accessPasskey == "" {
		s.jsonResponse(w, map[string]interface{}{
			"valid":    true,
			"message":  "No authentication required",
			"required": false,
		})
		return
	}

	var req struct {
		Passkey string `json:"passkey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Use constant-time comparison to prevent timing attacks
	if secureCompare(req.Passkey, s.accessPasskey) {
		s.jsonResponse(w, map[string]interface{}{
			"valid":    true,
			"message":  "Access granted",
			"required": true,
		})
	} else {
		s.jsonResponse(w, map[string]interface{}{
			"valid":    false,
			"message":  "Invalid passkey",
			"required": true,
		})
	}
}

// secureCompare performs constant-time comparison to prevent timing attacks
func secureCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Access-Key")

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
				"config":          t.Config, // Include config for editing
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

func (s *Server) handleTrades(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	traderID := r.URL.Query().Get("trader_id")
	if traderID == "" {
		s.errorResponse(w, http.StatusBadRequest, "trader_id required")
		return
	}

	trades, err := s.tradeStore.GetByTrader(traderID, 500)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	stats, err := s.tradeStore.GetTradeStats(traderID)
	if err != nil {
		stats = map[string]interface{}{}
	}

	s.jsonResponse(w, map[string]interface{}{
		"trades": trades,
		"stats":  stats,
	})
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

// buildDebateMarketContextForCycle is used by auto-cycle to get fresh market data
func (s *Server) buildDebateMarketContextForCycle(symbols []string) (*debate.MarketContext, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	marketData := make(map[string]*decision.MarketData)

	// Fetch market data for each symbol
	for _, symbol := range symbols {
		ticker, err := s.binanceClient.GetTicker(ctx, symbol)
		if err != nil {
			log.Printf("[Debate] Failed to get ticker for %s: %v", symbol, err)
			continue
		}

		klines, err := s.binanceClient.GetKlines(ctx, symbol, "5m", 288)
		if err != nil {
			log.Printf("[Debate] Failed to get klines for %s: %v", symbol, err)
		}

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

		change24h := 0.0
		if openPrice > 0 {
			change24h = ((ticker.Price - openPrice) / openPrice) * 100
		}

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

	// Get real account info from Binance
	account, err := s.binanceClient.GetAccountInfo(ctx)
	if err != nil {
		log.Printf("[Debate] Failed to get account info, using simulated: %v", err)
		account = &exchange.AccountInfo{
			TotalWalletBalance:    10000.0,
			TotalMarginBalance:    10000.0,
			AvailableBalance:      10000.0,
			TotalUnrealizedProfit: 0,
		}
	}

	// Get real positions
	positions, err := s.binanceClient.GetPositions(ctx)
	if err != nil {
		log.Printf("[Debate] Failed to get positions: %v", err)
		positions = []exchange.Position{}
	}

	// Convert to decision types
	decisionAccount := decision.AccountInfo{
		TotalEquity:      account.TotalMarginBalance,
		AvailableBalance: account.AvailableBalance,
		UnrealizedPnL:    account.TotalUnrealizedProfit,
		TotalPnL:         account.TotalMarginBalance - account.TotalWalletBalance,
		MarginUsed:       account.TotalMarginBalance - account.AvailableBalance,
	}
	if account.TotalMarginBalance > 0 {
		decisionAccount.MarginUsedPct = (decisionAccount.MarginUsed / account.TotalMarginBalance) * 100
	}

	var decisionPositions []decision.PositionInfo
	for _, p := range positions {
		if p.PositionAmt != 0 {
			decisionPositions = append(decisionPositions, decision.PositionInfo{
				Symbol:        p.Symbol,
				Side:          p.PositionSide,
				Quantity:      p.PositionAmt,
				EntryPrice:    p.EntryPrice,
				MarkPrice:     p.MarkPrice,
				UnrealizedPnL: p.UnrealizedProfit,
				Leverage:      p.Leverage,
			})
		}
	}

	// Save equity snapshot for the debate trader
	s.equityStore.Save(&store.EquitySnapshot{
		TraderID:      "debate_auto",
		Timestamp:     time.Now(),
		TotalEquity:   account.TotalMarginBalance,
		Balance:       account.TotalWalletBalance,
		UnrealizedPnL: account.TotalUnrealizedProfit,
		PositionCount: len(decisionPositions),
	})

	return &debate.MarketContext{
		CurrentTime: time.Now().Format(time.RFC3339),
		Account:     decisionAccount,
		Positions:   decisionPositions,
		MarketData:  marketData,
	}, nil
}

// executeDebateDecisions executes the consensus decisions from a debate
// Uses session-specific Binance credentials for trade execution
func (s *Server) executeDebateDecisions(session *debate.Session, decisions []*debate.Decision) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create session-specific Binance client using session's credentials
	binanceClient := exchange.NewBinanceClient(
		session.BinanceAPIKey,
		session.BinanceSecretKey,
		session.BinanceTestnet,
	)

	for _, d := range decisions {
		if d.Action == "wait" || d.Action == "hold" {
			log.Printf("[Debate] Skipping %s for %s", d.Action, d.Symbol)
			continue
		}

		if d.Confidence < 60 {
			log.Printf("[Debate] Skipping %s - low confidence: %d%%", d.Symbol, d.Confidence)
			continue
		}

		log.Printf("[Debate] Executing %s on %s (confidence: %d%%)", d.Action, d.Symbol, d.Confidence)

		// Get current price
		ticker, err := binanceClient.GetTicker(ctx, d.Symbol)
		if err != nil {
			log.Printf("[Debate] Failed to get price for %s: %v", d.Symbol, err)
			d.Error = err.Error()
			continue
		}

		// Calculate position size
		positionSizeUSD := d.PositionSizeUSD
		if positionSizeUSD <= 0 {
			// Use position percentage of account
			account, err := binanceClient.GetAccountInfo(ctx)
			if err == nil && d.PositionPct > 0 {
				positionSizeUSD = account.AvailableBalance * d.PositionPct
			} else {
				positionSizeUSD = 100.0 // Default $100
			}
		}

		quantity := positionSizeUSD / ticker.Price

		// Execute the trade based on action
		var side string
		switch d.Action {
		case "open_long", "BUY":
			side = "BUY"
		case "open_short", "SELL":
			side = "SELL"
		case "close_long":
			side = "SELL"
		case "close_short":
			side = "BUY"
		default:
			log.Printf("[Debate] Unknown action: %s", d.Action)
			continue
		}

		order, err := binanceClient.PlaceOrder(ctx, d.Symbol, side, "MARKET", quantity, 0)
		if err != nil {
			log.Printf("[Debate] Failed to execute %s on %s: %v", d.Action, d.Symbol, err)
			d.Error = err.Error()
			continue
		}

		d.Executed = true
		d.ExecutedAt = time.Now()
		d.OrderID = fmt.Sprintf("%d", order.OrderID)
		log.Printf("[Debate] Executed order %d: %s %s %.4f @ %.2f", order.OrderID, side, d.Symbol, quantity, ticker.Price)

		// Place stop-loss and take-profit orders for new positions
		if d.Action == "open_long" || d.Action == "open_short" || d.Action == "BUY" || d.Action == "SELL" {
			isLong := d.Action == "open_long" || d.Action == "BUY"
			closeSide := "SELL"
			if !isLong {
				closeSide = "BUY"
			}

			// Place stop-loss order
			if d.StopLoss > 0 {
				slOrder, err := binanceClient.PlaceStopLoss(ctx, d.Symbol, closeSide, 0, d.StopLoss)
				if err != nil {
					log.Printf("[Debate] Failed to place stop-loss for %s: %v", d.Symbol, err)
				} else {
					log.Printf("[Debate] Stop-loss placed for %s: ID=%d @ %.2f", d.Symbol, slOrder.OrderID, d.StopLoss)
				}
			}

			// Place take-profit order
			if d.TakeProfit > 0 {
				tpOrder, err := binanceClient.PlaceTakeProfit(ctx, d.Symbol, closeSide, 0, d.TakeProfit)
				if err != nil {
					log.Printf("[Debate] Failed to place take-profit for %s: %v", d.Symbol, err)
				} else {
					log.Printf("[Debate] Take-profit placed for %s: ID=%d @ %.2f", d.Symbol, tpOrder.OrderID, d.TakeProfit)
				}
			}
		}
	}

	// Save equity snapshot after execution
	account, err := binanceClient.GetAccountInfo(ctx)
	if err == nil {
		s.equityStore.Save(&store.EquitySnapshot{
			TraderID:      "debate_" + session.ID,
			Timestamp:     time.Now(),
			TotalEquity:   account.TotalMarginBalance,
			Balance:       account.TotalWalletBalance,
			UnrealizedPnL: account.TotalUnrealizedProfit,
		})
	}

	return nil
}
