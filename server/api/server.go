package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"auto-trader-ahh/analysis"
	"auto-trader-ahh/backtest"
	"auto-trader-ahh/config"
	"auto-trader-ahh/debate"
	"auto-trader-ahh/decision"
	"auto-trader-ahh/events"
	"auto-trader-ahh/exchange"
	"auto-trader-ahh/logger"
	"auto-trader-ahh/mcp"
	"auto-trader-ahh/store"
	"auto-trader-ahh/trader"

	"golang.org/x/time/rate"
)

type Server struct {
	port             string
	strategyStore    *store.StrategyStore
	traderStore      *store.TraderStore
	positionStore    *store.PositionStore
	decisionStore    *store.DecisionStore
	equityStore      *store.EquityStore
	tradeStore       *store.TradeStore
	settingsStore    *store.SettingsStore
	attributionStore *store.AttributionStore
	engineManager    *trader.EngineManager
	debateEngine     *debate.Engine
	backtestManager  *backtest.Manager
	aiClient         mcp.AIClient
	binanceClient    exchange.Exchange
	accessPasskey    string
	cfg              *config.Config
	hub              *events.Hub

	// SECURITY: Rate limiting
	rateLimiters map[string]*rate.Limiter
	limiterMu    sync.RWMutex
}

func NewServer(port string, em *trader.EngineManager, cfg *config.Config) *Server {
	// AI client — routed through the local Quota Concierge sidecar (Claude MAX / Codex)
	aiClient := mcp.NewConciergeClient(cfg.LLMModel)

	// Exchange client for backtest + dashboard market endpoints (active venue from EXCHANGE)
	binanceClient, exErr := exchange.NewExchange(cfg)
	if exErr != nil {
		log.Fatalf("exchange init failed: %v", exErr)
	}

	// Create debate engine and register AI client
	debateEng := debate.NewEngine()
	debateEng.RegisterClient("openrouter", aiClient)
	debateEng.RegisterClient("openai", aiClient)    // Use OpenRouter for all providers
	debateEng.RegisterClient("anthropic", aiClient) // OpenRouter supports these models
	debateEng.RegisterClient("deepseek", aiClient)

	equityStore := store.NewEquityStore()

	srv := &Server{
		port:             port,
		strategyStore:    store.NewStrategyStore(),
		traderStore:      store.NewTraderStore(),
		positionStore:    store.NewPositionStore(),
		decisionStore:    store.NewDecisionStore(),
		equityStore:      equityStore,
		tradeStore:       store.NewTradeStore(),
		settingsStore:    store.NewSettingsStore(),
		attributionStore: store.NewAttributionStore(),
		engineManager:    em,
		debateEngine:     debateEng,
		backtestManager:  backtest.NewManager(aiClient, binanceClient),
		aiClient:         aiClient,
		binanceClient:    binanceClient,
		accessPasskey:    cfg.AccessPasskey,
		cfg:              cfg,
		hub:              em.GetHub(),
		rateLimiters:     make(map[string]*rate.Limiter),
	}

	// Wire up debate engine with market context provider and trade executor
	debateEng.SetMarketContextProvider(srv.buildDebateMarketContextForCycle)
	debateEng.SetTradeExecutor(srv.executeDebateDecisions)
	// Council Attribution v0: log every consensus into attribution with participant
	// persona/model provenance (read-only; fires for auto-execute and read-only sessions).
	debateEng.SetConsensusRecorder(srv.recordCouncilConsensus)

	return srv
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Public endpoints (no auth required)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/auth/verify", s.handleAuthVerify)
	mux.HandleFunc("/api/events", s.hub.ServeHTTP) // SSE endpoint

	// Protected endpoints (auth required)
	// Strategy endpoints
	mux.HandleFunc("/api/strategies", s.authMiddleware(s.handleStrategies))
	mux.HandleFunc("/api/strategies/", s.authMiddleware(s.handleStrategy))
	mux.HandleFunc("/api/strategies/active", s.authMiddleware(s.handleActiveStrategy))
	mux.HandleFunc("/api/strategies/default-config", s.authMiddleware(s.handleDefaultConfig))
	mux.HandleFunc("/api/strategies/recommend-pairs", s.authMiddleware(s.handleRecommendPairs))

	// Trader endpoints
	mux.HandleFunc("/api/traders", s.authMiddleware(s.handleTraders))
	mux.HandleFunc("/api/traders/", s.authMiddleware(s.handleTrader))

	// Data endpoints
	mux.HandleFunc("/api/symbols", s.authMiddleware(s.handleSymbols))
	mux.HandleFunc("/api/top-watches", s.authMiddleware(s.handleTopWatches))
	mux.HandleFunc("/api/klines", s.authMiddleware(s.handleKlines))
	mux.HandleFunc("/api/patterns", s.authMiddleware(s.handlePatterns))
	// Live AI trading assistant (reliable non-streaming + token-streaming SSE)
	mux.HandleFunc("/api/assistant/analyze", s.authMiddleware(s.handleAssistantAnalyze))
	mux.HandleFunc("/api/assistant/stream", s.authMiddleware(s.handleAssistantStream))
	// AI decision engine: one verdict per symbol + a management verdict per open position
	mux.HandleFunc("/api/verdict", s.authMiddleware(s.handleVerdict))
	mux.HandleFunc("/api/guardian", s.authMiddleware(s.handleGuardian))
	// Deterministic signal board (fast, no LLM) — the "what the system is seeing" layer
	mux.HandleFunc("/api/signals", s.authMiddleware(s.handleSignals))
	// Read-only Validation Harness v0 — replays the deterministic board over history
	mux.HandleFunc("/api/validation/signals", s.authMiddleware(s.handleValidationSignals))
	// Read-only Attribution Log v0 — the Feedback Loop's evidence floor. Every fresh
	// verdict is logged with the board it weighed; these endpoints inspect that log.
	mux.HandleFunc("/api/attribution/verdicts", s.authMiddleware(s.handleAttributionVerdicts))
	mux.HandleFunc("/api/attribution/verdicts/", s.authMiddleware(s.handleAttributionVerdict))
	mux.HandleFunc("/api/attribution/summary", s.authMiddleware(s.handleAttributionSummary))
	// Outcome Linkage v0 — manual, local-only resolver: replays klines after each pending
	// verdict to populate attribution_outcomes (passive; no trade/exchange writes).
	mux.HandleFunc("/api/attribution/resolve-outcomes", s.authMiddleware(s.handleResolveOutcomes))
	// Per-Signal Outcome Rollups v0 — read-only, stance-aware aggregation of resolved
	// outcomes (evidence before knobs; no weight tuning).
	mux.HandleFunc("/api/attribution/rollups", s.authMiddleware(s.handleAttributionRollups))
	// Realized Trade Rollups v0 — read-only aggregation of explicitly linked action
	// results, kept separate from passive market outcomes.
	mux.HandleFunc("/api/attribution/realized-rollups", s.authMiddleware(s.handleAttributionRealizedRollups))
	// Trade-Fill Linkage v0 — bridge verdicts↔real/paper trades. Read-only candidate
	// detection + explicit local linkage; never places/cancels/modifies orders.
	mux.HandleFunc("/api/attribution/trade-links", s.authMiddleware(s.handleTradeLinks))
	mux.HandleFunc("/api/attribution/trade-links/candidates", s.authMiddleware(s.handleTradeLinkCandidates))
	mux.HandleFunc("/api/attribution/link-trades", s.authMiddleware(s.handleLinkTrades))
	mux.HandleFunc("/api/status", s.authMiddleware(s.handleStatus))
	mux.HandleFunc("/api/account", s.authMiddleware(s.handleAccount))
	mux.HandleFunc("/api/positions", s.authMiddleware(s.handlePositions))
	mux.HandleFunc("/api/decisions", s.authMiddleware(s.handleDecisions))
	mux.HandleFunc("/api/trades", s.authMiddleware(s.handleTrades))
	mux.HandleFunc("/api/trades/", s.authMiddleware(s.handleTradeNarrative))
	mux.HandleFunc("/api/equity-history", s.authMiddleware(s.handleEquityHistory))
	mux.HandleFunc("/api/kill-switch", s.authMiddleware(s.handleKillSwitch))
	mux.HandleFunc("/api/venue-config", s.authMiddleware(s.handleVenueConfig))
	// Named venue connection profiles (save several Phemex connections; switch via dropdown).
	mux.HandleFunc("/api/connections", s.authMiddleware(s.handleConnections))
	mux.HandleFunc("/api/connections/activate", s.authMiddleware(s.handleConnectionActivate))
	mux.HandleFunc("/api/connections/delete", s.authMiddleware(s.handleConnectionDelete))
	mux.HandleFunc("/api/connections/balance", s.authMiddleware(s.handleConnectionBalance))

	// Backtest endpoints
	mux.HandleFunc("/api/backtest", s.authMiddleware(s.handleBacktests))
	mux.HandleFunc("/api/backtest/start", s.authMiddleware(s.handleBacktestStart))
	mux.HandleFunc("/api/backtest/", s.authMiddleware(s.handleBacktest))

	// Debate endpoints
	mux.HandleFunc("/api/debate/sessions", s.authMiddleware(s.handleDebateSessions))
	mux.HandleFunc("/api/debate/sessions/", s.authMiddleware(s.handleDebateSession))

	// Settings endpoints
	mux.HandleFunc("/api/settings", s.authMiddleware(s.handleSettings))
	mux.HandleFunc("/api/favorites", s.authMiddleware(s.handleFavorites))

	// System endpoints
	mux.HandleFunc("/api/logs/stream", s.authMiddleware(s.handleLogStream))

	// Wrap with middleware layers: Rate Limit -> CORS
	handler := s.rateLimitHandler(s.corsMiddleware(mux))

	log.Printf("API server starting at http://localhost:%s", s.port)
	log.Printf("SECURITY: Rate limiting enabled - 50 req/s per IP, burst 100 (loopback exempt)")
	if s.accessPasskey != "" {
		log.Printf("Authentication enabled - passkey required")
	} else {
		log.Printf("WARNING: No ACCESS_PASSKEY set - server is unprotected!")
	}
	return http.ListenAndServe(":"+s.port, handler)
}

// SECURITY: Rate limiting middleware

// getLimiter returns a rate limiter for the given IP address
func (s *Server) getLimiter(ip string) *rate.Limiter {
	s.limiterMu.Lock()
	defer s.limiterMu.Unlock()

	limiter, exists := s.rateLimiters[ip]
	if !exists {
		// Create new limiter: 50 req/s, burst 100. Loopback (the local UI) is exempted
		// entirely in the middleware, so this cap only applies to non-local clients, which
		// must also pass the passkey auth gate.
		limiter = rate.NewLimiter(rate.Limit(50), 100)
		s.rateLimiters[ip] = limiter

		// Clean up old limiters periodically (every 100 new IPs)
		if len(s.rateLimiters) > 100 {
			go s.cleanupLimiters()
		}
	}

	return limiter
}

// cleanupLimiters removes limiters that haven't been used recently
func (s *Server) cleanupLimiters() {
	s.limiterMu.Lock()
	defer s.limiterMu.Unlock()

	// Simple cleanup: if we have too many, clear all and start fresh
	// In production, you'd want to track last-used timestamps
	if len(s.rateLimiters) > 1000 {
		s.rateLimiters = make(map[string]*rate.Limiter)
		log.Printf("Rate limiter cache cleared (exceeded 1000 entries)")
	}
}

// isLoopbackAddr reports whether a request originates from localhost (the local UI / sidecar).
// Loopback is exempt from rate limiting: the dashboard polls many endpoints at once, and the
// passkey (authMiddleware) is the real access gate.
func isLoopbackAddr(remoteAddr string) bool {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return host == "localhost"
}

// rateLimitMiddleware enforces rate limits per IP address (for http.HandlerFunc)
func (s *Server) rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Local UI / sidecar traffic is never rate-limited (auth gates it instead).
		if isLoopbackAddr(r.RemoteAddr) {
			next(w, r)
			return
		}
		// Extract IP from request
		ip := r.RemoteAddr
		if forwardedFor := r.Header.Get("X-Forwarded-For"); forwardedFor != "" {
			// Use first IP in X-Forwarded-For chain
			ip = strings.Split(forwardedFor, ",")[0]
			ip = strings.TrimSpace(ip)
		}

		// Get rate limiter for this IP
		limiter := s.getLimiter(ip)

		// Check if request is allowed
		if !limiter.Allow() {
			log.Printf("SECURITY: Rate limit exceeded for IP %s", ip)
			s.errorResponse(w, http.StatusTooManyRequests, "Rate limit exceeded. Please try again later.")
			return
		}

		next(w, r)
	}
}

// rateLimitHandler wraps an http.Handler with rate limiting
func (s *Server) rateLimitHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isLoopbackAddr(r.RemoteAddr) {
			next.ServeHTTP(w, r)
			return
		}
		// Extract IP from request
		ip := r.RemoteAddr
		if forwardedFor := r.Header.Get("X-Forwarded-For"); forwardedFor != "" {
			// Use first IP in X-Forwarded-For chain
			ip = strings.Split(forwardedFor, ",")[0]
			ip = strings.TrimSpace(ip)
		}

		// Get rate limiter for this IP
		limiter := s.getLimiter(ip)

		// Check if request is allowed
		if !limiter.Allow() {
			log.Printf("SECURITY: Rate limit exceeded for IP %s", ip)
			s.errorResponse(w, http.StatusTooManyRequests, "Rate limit exceeded. Please try again later.")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// authMiddleware checks for valid passkey in X-Access-Key header
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if no passkey is configured
		if s.accessPasskey == "" {
			next(w, r)
			return
		}

		// Check X-Access-Key header or query param
		accessKey := r.Header.Get("X-Access-Key")
		if accessKey == "" {
			accessKey = r.URL.Query().Get("access_key")
		}
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

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		allowed := false
		for _, allowedOrigin := range s.cfg.AllowedOrigins {
			if origin == allowedOrigin {
				allowed = true
				break
			}
		}

		// Set CORS headers only for allowed origins
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else if origin != "" {
			// Origin provided but not allowed - log for security monitoring
			log.Printf("SECURITY: Blocked CORS request from unauthorized origin: %s", origin)
		}

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

		// SECURITY: Validate strategy config before creation
		if err := store.ValidateStrategyConfig(&strategy.Config); err != nil {
			s.errorResponse(w, http.StatusBadRequest, fmt.Sprintf("Invalid strategy config: %v", err))
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

		// SECURITY: Validate strategy config before update
		if err := store.ValidateStrategyConfig(&strategy.Config); err != nil {
			s.errorResponse(w, http.StatusBadRequest, fmt.Sprintf("Invalid strategy config: %v", err))
			return
		}

		strategy.ID = id
		if err := s.strategyStore.Update(&strategy); err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Push updated strategy to running engines (live reload)
		if err := s.engineManager.ReloadStrategyForTraders(id); err != nil {
			log.Printf("Warning: failed to reload strategy for running traders: %v", err)
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

func (s *Server) handleRecommendPairs(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// 0. Parse Request Body
	var req struct {
		Count int  `json:"count"`
		Turbo bool `json:"turbo"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}
	targetCount := req.Count
	if targetCount <= 0 {
		targetCount = 7 // Default
	}

	// 1. Get Top Volume Coins (Raw Data)
	tickers, err := s.binanceClient.Get24hTicker(context.Background())
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "Failed to fetch market data: "+err.Error())
		return
	}

	// 2. Get Account Info
	account, err := s.binanceClient.GetAccountInfo(context.Background())
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "Failed to fetch account info: "+err.Error())
		return
	}

	// Filter Tickers
	type MarketCoin struct {
		Symbol      string
		PriceChange float64
		Volume      float64
		QuoteVolume float64
	}
	var candidates []MarketCoin
	for _, t := range tickers {
		// Basic filter: USDT pairs, reasonable volume
		if len(t.Symbol) > 4 && t.Symbol[len(t.Symbol)-4:] == "USDT" {
			// Ensure symbol is actively trading (Futures)
			if !s.binanceClient.IsActiveSymbol(t.Symbol) {
				continue
			}

			// Skip stables
			if t.Symbol == "USDCUSDT" || t.Symbol == "FDUSDUSDT" || t.Symbol == "TUSDUSDT" || t.Symbol == "USDPUSDT" {
				continue
			}
			// Only consider decent volume (>500k) to avoid total dead coins, even in turbo
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

	var prompt string
	if req.Turbo {
		// TURBO MODE: Sort by Volatility (Absolute Price Change)
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
		if len(candidates) > 30 {
			candidates = candidates[:30]
		}

		prompt = fmt.Sprintf(`You are a HIGH RISK crypto degen trader. 
My current balance: $%.2f
Objective: Find the %d MOST EXPLOSIVE trading pairs for aggressive scalping. I am willing to take extreme risks (80-90%% loss) for high rewards.
Criteria: High Volatility, Momentum, Meme Coins, or Breakout candidates. Ignore safety.

Here are the Top 30 pairs by Volatility (Price Change):
`, account.TotalWalletBalance, targetCount)

	} else {
		// STANDARD MODE: Sort by Volume (Safety)
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].QuoteVolume > candidates[j].QuoteVolume })
		if len(candidates) > 30 {
			candidates = candidates[:30]
		}

		prompt = fmt.Sprintf(`You are a crypto trading expert. 
My current balance: $%.2f
Objective: Find the best %d trading pairs for high-probability scalping/day-trading.
Criteria: High liquidity, good volatility (but not insane/manipulated), clear trends.

Here are the Top 30 pairs by 24h Volume:
`, account.TotalWalletBalance, targetCount)
	}

	for _, c := range candidates {
		prompt += fmt.Sprintf("- %s: Vol=$%.0fM, Chg=%.2f%%\n", c.Symbol, c.QuoteVolume/1000000, c.PriceChange)
	}

	prompt += fmt.Sprintf(`
Return ONLY a JSON array of strings with the selected %d symbols. Example: ["BTCUSDT", "ETHUSDT", "SOLUSDT"]
Result:`, targetCount)

	// 4. Call AI
	// Use :online model for Smart Find/Recommend Pairs to get fresh web data
	currentModel := s.aiClient.GetModel()
	onlineModel := currentModel
	if !strings.HasSuffix(onlineModel, ":online") {
		onlineModel += ":online"
	}

	aiReq := &mcp.Request{
		Model: onlineModel,
		Messages: []mcp.Message{
			{Role: "system", Content: "You are a smart crypto trading assistant."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.7,
		MaxTokens:   4096,
	}

	respStruct, err := s.aiClient.CallWithRequest(aiReq)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "AI Request failed: "+err.Error())
		return
	}
	response := respStruct.Content

	// 5. Parse Response (Extract JSON array)
	// Basic cleanup if AI adds markdown
	jsonStr := response
	if idx := strings.Index(jsonStr, "["); idx != -1 {
		jsonStr = jsonStr[idx:]
	}
	if idx := strings.LastIndex(jsonStr, "]"); idx != -1 {
		jsonStr = jsonStr[:idx+1]
	}

	var recommended []string
	if err := json.Unmarshal([]byte(jsonStr), &recommended); err != nil {
		// Fallback: manually split by comma if JSON parse fails (simple robustness)
		// But giving error is safer
		s.errorResponse(w, http.StatusInternalServerError, "Failed to parse AI response: "+err.Error())
		return
	}

	s.jsonResponse(w, map[string]interface{}{"pairs": recommended})
}

// Helper methods for slice sorting and string parsing needed by above handler
// We need to inject these strictly into the store/utils area or define inline if Go allows,
// strictly Go doesn't allow inline func defs for types outside.
// To keep it simple in this file, I'll use inline generic-ish sort or valid helper logic.

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

		case "resume":
			// Cancel trading pause and resume trading
			if err := s.engineManager.CancelTradingPause(id); err != nil {
				s.errorResponse(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.jsonResponse(w, map[string]string{"status": "resumed"})

		case "execute":
			s.handleExecuteTraderPlan(w, r, id)

		default:
			s.errorResponse(w, http.StatusBadRequest, "Unknown action")
		}
		return
	}

	// Handle GET actions (pause-status)
	if action != "" && r.Method == "GET" {
		switch action {
		case "pause-status":
			status := s.engineManager.GetPauseStatus(id)
			s.jsonResponse(w, status)
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

// handleSymbols returns all active futures for the active exchange, enriched
// with last price and 24h percent change where a ticker is available. Only the
// active venue is queried (no Binance hardcoding) — on Phemex the 24h ticker may
// only cover majors, so symbols without ticker data report zeros.
func (s *Server) handleSymbols(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Index 24h tickers by symbol for O(1) lookup of last price + percent change.
	tickerBySymbol := make(map[string]exchange.Ticker24h)
	if tickers, err := s.binanceClient.Get24hTicker(ctx); err != nil {
		// Non-fatal: still return symbol metadata with zeroed price fields.
		log.Printf("[Symbols] Failed to fetch 24h ticker: %v", err)
	} else {
		for _, t := range tickers {
			tickerBySymbol[t.Symbol] = t
		}
	}

	type symbolResponse struct {
		Symbol            string  `json:"symbol"`
		LastPrice         float64 `json:"lastPrice"`
		PriceChangePct    float64 `json:"priceChangePct"`
		MaxLeverage       int     `json:"maxLeverage"`
		QuantityPrecision int     `json:"quantityPrecision"`
		PricePrecision    int     `json:"pricePrecision"`
	}

	var result []symbolResponse
	for _, info := range s.binanceClient.ListSymbols() {
		// Filter to active contracts only (Binance: "TRADING", Phemex: "Listed").
		if !strings.EqualFold(info.Status, "TRADING") && !strings.EqualFold(info.Status, "Listed") {
			continue
		}

		item := symbolResponse{
			Symbol:            info.Symbol,
			MaxLeverage:       info.MaxLeverage,
			QuantityPrecision: info.QuantityPrecision,
			PricePrecision:    info.PricePrecision,
		}
		if t, ok := tickerBySymbol[info.Symbol]; ok {
			item.LastPrice = t.LastPrice
			item.PriceChangePct = t.PriceChange
		}
		result = append(result, item)
	}

	// Sort by symbol for a stable, predictable response order.
	sort.Slice(result, func(i, j int) bool { return result[i].Symbol < result[j].Symbol })

	s.jsonResponse(w, result)
}

// handleKlines returns OHLCV candles for a symbol from the active exchange,
// shaped for lightweight-charts: oldest-first, with time in UNIX SECONDS.
// Query params: symbol (required), interval (default "1h"), limit (default 300,
// clamped to 1..1000). GetKlines already returns candles sorted ascending.
func (s *Server) handleKlines(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		s.errorResponse(w, http.StatusBadRequest, "symbol required")
		return
	}

	interval := r.URL.Query().Get("interval")
	if interval == "" {
		interval = "1h"
	}

	limit := 300
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			limit = parsed
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	klines, err := s.binanceClient.GetKlines(ctx, symbol, interval, limit)
	if err != nil {
		s.errorResponse(w, http.StatusBadGateway, "Failed to fetch klines: "+err.Error())
		return
	}

	type candle struct {
		Time   int64   `json:"time"` // unix SECONDS (lightweight-charts)
		Open   float64 `json:"open"`
		High   float64 `json:"high"`
		Low    float64 `json:"low"`
		Close  float64 `json:"close"`
		Volume float64 `json:"volume"`
	}

	// GetKlines returns ascending (oldest-first) — preserve that order.
	result := make([]candle, 0, len(klines))
	for _, k := range klines {
		result = append(result, candle{
			Time:   k.OpenTime / 1000, // ms -> seconds
			Open:   k.Open,
			High:   k.High,
			Low:    k.Low,
			Close:  k.Close,
			Volume: k.Volume,
		})
	}

	s.jsonResponse(w, result)
}

// handlePatterns runs deterministic, pure-Go chart-pattern detection over recent
// klines from the active exchange and returns the detected patterns as a JSON
// array (sorted by confidence desc; empty array when none found). The chart
// overlay consumes this now; the live AI assistant will consume it later.
// Query params: symbol (required), interval (default "1h"), limit (default 300,
// clamped to 1..1000). Candle.Time carries OpenTime in UNIX MILLISECONDS.
func (s *Server) handlePatterns(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		s.errorResponse(w, http.StatusBadRequest, "symbol required")
		return
	}

	interval := r.URL.Query().Get("interval")
	if interval == "" {
		interval = "1h"
	}

	limit := 300
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			limit = parsed
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	klines, err := s.binanceClient.GetKlines(ctx, symbol, interval, limit)
	if err != nil {
		s.errorResponse(w, http.StatusBadGateway, "Failed to fetch klines: "+err.Error())
		return
	}

	// Map exchange klines onto the dependency-free analysis input. OpenTime is in
	// UNIX MILLISECONDS and stays in ms in Candle.Time (the detectors never do
	// time arithmetic; Time is carried through for context only).
	candles := make([]analysis.Candle, 0, len(klines))
	for _, k := range klines {
		candles = append(candles, analysis.Candle{
			Time:   k.OpenTime,
			Open:   k.Open,
			High:   k.High,
			Low:    k.Low,
			Close:  k.Close,
			Volume: k.Volume,
		})
	}

	patterns := analysis.DetectPatterns(candles)
	s.jsonResponse(w, patterns)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	traderID := r.URL.Query().Get("trader_id")
	if traderID == "" {
		s.errorResponse(w, http.StatusBadRequest, "trader_id required")
		return
	}

	status := s.engineManager.GetStatus(traderID)
	s.jsonResponse(w, status)
}

// handleKillSwitch reads or toggles the global order-placement kill switch.
// GET  -> current {engaged, reason}. POST {engaged, reason} -> sets it and
// returns the new state. When engaged, every order-write method on every venue
// (paper, Phemex, Binance) returns exchange.ErrKillSwitch.
func (s *Server) handleKillSwitch(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		s.jsonResponse(w, map[string]interface{}{
			"engaged": exchange.KillSwitchEngaged(),
			"reason":  exchange.KillSwitchReason(),
		})

	case "POST":
		var req struct {
			Engaged bool   `json:"engaged"`
			Reason  string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		exchange.SetKillSwitch(req.Engaged, req.Reason)
		if req.Engaged {
			log.Printf("[KillSwitch] ENGAGED - all order placement blocked (reason: %q)", req.Reason)
		} else {
			log.Printf("[KillSwitch] DISENGAGED - order placement resumed (reason: %q)", req.Reason)
		}
		s.jsonResponse(w, map[string]interface{}{
			"engaged": exchange.KillSwitchEngaged(),
			"reason":  exchange.KillSwitchReason(),
		})

	default:
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
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
	source := "engine"
	// Fallback: when the live engine reports no positions (trader stopped, just
	// restarted before the boot reconcile, or a transient reconcile that emptied
	// the in-memory map), the dashboard would show nothing even though open
	// positions are persisted in the DB. Surface the DB's open positions so the
	// UI never silently hides a real open position.
	if len(positions) == 0 {
		if dbPos, err := s.positionStore.GetOpenPositions(traderID); err == nil && len(dbPos) > 0 {
			positions = make([]map[string]interface{}, 0, len(dbPos))
			for _, p := range dbPos {
				amt := p.Quantity
				if strings.EqualFold(p.Side, "short") {
					amt = -amt
				}
				positions = append(positions, map[string]interface{}{
					"symbol":      p.Symbol,
					"side":        strings.ToUpper(p.Side),
					"amount":      amt,
					"entry_price": p.EntryPrice,
					"mark_price":  p.EntryPrice, // no live mark when sourced from DB
					"pnl":         0.0,
					"pnl_percent": 0.0,
					"leverage":    p.Leverage,
					"source":      "db",
				})
			}
			source = "db"
		}
	}
	s.jsonResponse(w, map[string]interface{}{"positions": positions, "source": source})
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

	// "all" → aggregate across every trader, including ones whose trader/strategy row
	// was later deleted (trades are never cascade-deleted). Powers the all-time view and
	// keeps orphaned history visible. Read-only; no order activity.
	if traderID == "all" {
		trades, err := s.tradeStore.GetAll(5000)
		if err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		stats, err := s.tradeStore.GetAllStats()
		if err != nil {
			stats = map[string]interface{}{}
		}
		s.jsonResponse(w, map[string]interface{}{
			"trades": trades,
			"stats":  stats,
		})
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
	case "events":
		if r.Method != "GET" {
			s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		s.handleDebateEvents(w, r, sessionID)

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

func (s *Server) handleDebateEvents(w http.ResponseWriter, r *http.Request, sessionID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.errorResponse(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	events, unsubscribe, err := s.debateEngine.SubscribeEvents(sessionID)
	if err != nil {
		s.errorResponse(w, http.StatusNotFound, err.Error())
		return
	}
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	emit := func(eventName string, payload interface{}) bool {
		b, err := json.Marshal(payload)
		if err != nil {
			log.Printf("debate event marshal error: %v", err)
			return true
		}
		if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	session, err := s.debateEngine.GetSession(sessionID)
	if err != nil {
		s.errorResponse(w, http.StatusNotFound, err.Error())
		return
	}
	if !emit("snapshot", session) {
		return
	}

	if session.Status != debate.StatusRunning && session.Status != debate.StatusVoting {
		return
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if !emit("ping", map[string]string{"session_id": sessionID}) {
				return
			}
		case event, ok := <-events:
			if !ok {
				return
			}
			if event == nil {
				continue
			}
			if !emit(event.Type, event) {
				return
			}
		}
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

	// Resolve the execution venue. When the session carries explicit venue creds,
	// build a session-specific client from them; otherwise fall back to the server's
	// ALREADY-ACTIVE exchange (paper-trading by default → real prices + simulated
	// fills + no keys, or a live venue using the keys saved on the Connections page).
	// This is what lets auto-execute actually fire after we removed the redundant
	// credential entry from the New Council Session modal.
	binanceClient := s.binanceClient
	if strings.TrimSpace(session.VenueAPIKey) != "" {
		exCfg := *s.cfg
		exCfg.PhemexAPIKey, exCfg.PhemexSecretKey, exCfg.PhemexTestnet = session.VenueAPIKey, session.VenueSecretKey, session.VenueTestnet
		exCfg.BinanceAPIKey, exCfg.BinanceSecretKey, exCfg.BinanceTestnet = session.VenueAPIKey, session.VenueSecretKey, session.VenueTestnet
		c, exErr := exchange.NewExchange(&exCfg)
		if exErr != nil {
			return fmt.Errorf("debate exchange init: %w", exErr)
		}
		binanceClient = c
	}

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

		order, err := binanceClient.PlaceOrder(ctx, d.Symbol, side, "MARKET", quantity, 0, false)
		if err != nil {
			log.Printf("[Debate] Failed to execute %s on %s: %v", d.Action, d.Symbol, err)
			d.Error = err.Error()
			continue
		}

		d.Executed = true
		d.ExecutedAt = time.Now()
		d.OrderID = fmt.Sprintf("%d", order.OrderID)
		log.Printf("[Debate] Executed order %d: %s %s %.4f @ %.2f", order.OrderID, side, d.Symbol, quantity, ticker.Price)

		// Trade-Fill Linkage: a Council auto-execute is a LIVE verdict→order handoff. Link
		// the council attribution verdict (logged at consensus) to this order. Best-effort,
		// local-only — it never affects execution.
		if s.attributionStore != nil {
			if lerr := s.attributionStore.UpsertTradeLink(&store.AttributionTradeLink{
				VerdictID: councilVerdictID(session.ID, d), LinkedVia: "explicit", LinkConfidence: "high",
				LinkNotes: "Council auto-execute", ActionTaken: true, Actor: "council", Status: "open",
				TraderID: session.TraderID, OrderID: d.OrderID, EntryPrice: ticker.Price, EntrySize: quantity,
				SourceSurface: "council", SourcePersonaName: "Council", SourceProvider: "concierge",
			}); lerr != nil {
				log.Printf("[Attribution] council trade-link failed (%s): %v", d.Symbol, lerr)
			}
		}

		// Place stop-loss and take-profit orders for new positions
		if d.Action == "open_long" || d.Action == "open_short" || d.Action == "BUY" || d.Action == "SELL" {
			isLong := d.Action == "open_long" || d.Action == "BUY"
			closeSide := "SELL"
			if !isLong {
				closeSide = "BUY"
			}

			// SECURITY: Place stop-loss order (CRITICAL for position protection)
			if d.StopLoss > 0 {
				slOrder, err := binanceClient.PlaceStopLoss(ctx, d.Symbol, closeSide, 0, d.StopLoss)
				if err != nil {
					log.Printf("❌ CRITICAL: Failed to place stop-loss for %s: %v", d.Symbol, err)

					// EMERGENCY ROLLBACK: Close the unprotected position immediately
					positionAmt := quantity
					if side == "SELL" {
						positionAmt = -quantity // Short position has negative amount
					}

					closeOrder, closeErr := binanceClient.ClosePosition(ctx, d.Symbol, positionAmt)
					if closeErr != nil {
						// WORST CASE: Can't close the unprotected position
						log.Printf("🚨 EMERGENCY: Failed to close unprotected position %s: %v", d.Symbol, closeErr)
						log.Printf("🚨 MANUAL INTERVENTION REQUIRED: Symbol=%s Side=%s Quantity=%.4f", d.Symbol, side, quantity)

						// Mark decision with critical error
						d.Error = fmt.Sprintf("SL placement failed AND emergency close failed: %v | %v", err, closeErr)
					} else {
						log.Printf("✅ Emergency close successful for %s: OrderID=%d", d.Symbol, closeOrder.OrderID)
						log.Printf("Position closed at market to prevent unlimited loss exposure")
						d.Error = fmt.Sprintf("SL placement failed, position emergency closed: %v", err)
					}
				} else {
					log.Printf("[Debate] Stop-loss placed for %s: ID=%d @ %.2f", d.Symbol, slOrder.OrderID, d.StopLoss)
				}
			}

			// Place take-profit order (less critical - position can trade without TP if SL exists)
			if d.TakeProfit > 0 && d.Error == "" {
				tpOrder, err := binanceClient.PlaceTakeProfit(ctx, d.Symbol, closeSide, 0, d.TakeProfit)
				if err != nil {
					log.Printf("⚠️ WARNING: Failed to place take-profit for %s: %v", d.Symbol, err)
					// TP failure is acceptable - position still has SL protection
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

// ============ SETTINGS ENDPOINTS ============

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		settings, err := s.settingsStore.GetGlobalSettings()
		if err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Include whether settings are configured (for UI to know if setup is needed)
		response := map[string]interface{}{
			"settings": settings,
			"configured": map[string]bool{
				"phemex": settings.PhemexAPIKey != "" || s.cfg.PhemexAPIKey != "",
			},
		}
		s.jsonResponse(w, response)

	case "PUT":
		var req store.GlobalSettings
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		// Get existing settings to preserve masked values
		existing, _ := s.settingsStore.GetGlobalSettings()

		// Only update non-masked values (if masked value sent, keep existing)
		if isMasked(req.PhemexAPIKey) {
			req.PhemexAPIKey = existing.PhemexAPIKey
		}
		if isMasked(req.PhemexSecretKey) {
			req.PhemexSecretKey = existing.PhemexSecretKey
		}

		if err := s.settingsStore.SaveGlobalSettings(&req); err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Reload config to apply new settings
		s.reloadConfig()

		s.jsonResponse(w, map[string]string{"status": "saved"})

	default:
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// isMasked checks if a string contains masked characters
func isMasked(s string) bool {
	return len(s) > 0 && (s == "****" || (len(s) > 8 && s[4:8] == "****"))
}

// handleFavorites persists the user's favorite symbols server-side so they survive any
// client reset (cache clear, dev-port change, a different browser) — fixing favorites
// being lost across restarts. Stored as a JSON string array under the generic settings
// key "ui_favorites"; touches no secrets and places no orders.
func (s *Server) handleFavorites(w http.ResponseWriter, r *http.Request) {
	const key = "ui_favorites"
	switch r.Method {
	case "GET":
		raw, err := s.settingsStore.Get(key)
		if err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		favorites := []string{}
		if raw != "" {
			_ = json.Unmarshal([]byte(raw), &favorites)
		}
		s.jsonResponse(w, map[string]interface{}{"favorites": favorites})

	case "PUT", "POST":
		var req struct {
			Favorites []string `json:"favorites"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.errorResponse(w, http.StatusBadRequest, "invalid request body")
			return
		}
		// De-dup + drop blanks so the stored list stays clean.
		seen := map[string]bool{}
		clean := []string{}
		for _, sym := range req.Favorites {
			sym = strings.TrimSpace(sym)
			if sym == "" || seen[sym] {
				continue
			}
			seen[sym] = true
			clean = append(clean, sym)
		}
		data, _ := json.Marshal(clean)
		if err := s.settingsStore.Set(key, string(data)); err != nil {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.jsonResponse(w, map[string]interface{}{"favorites": clean})

	default:
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// reloadConfig reloads the AI client and Binance client with new settings
func (s *Server) reloadConfig() {
	settings, err := s.settingsStore.GetGlobalSettings()
	if err != nil {
		log.Printf("Failed to reload settings: %v", err)
		return
	}

	// AI client always routes through the Quota Concierge sidecar (no key/model needed).
	s.aiClient = mcp.NewConciergeClient(s.cfg.LLMModel)
	s.debateEngine.RegisterClient("openrouter", s.aiClient)
	s.debateEngine.RegisterClient("openai", s.aiClient)
	s.debateEngine.RegisterClient("anthropic", s.aiClient)
	s.debateEngine.RegisterClient("deepseek", s.aiClient)

	// Exchange client: prefer Phemex keys from settings, else fall back to .env config.
	exCfg := *s.cfg
	if settings.PhemexAPIKey != "" {
		exCfg.PhemexAPIKey = settings.PhemexAPIKey
		exCfg.PhemexSecretKey = settings.PhemexSecretKey
		exCfg.PhemexTestnet = settings.PhemexTestnet
	}
	if exch, err := exchange.NewExchange(&exCfg); err == nil {
		s.binanceClient = exch
		s.backtestManager = backtest.NewManager(s.aiClient, s.binanceClient)
	} else {
		log.Printf("Config reload: exchange init failed: %v", err)
	}

	log.Printf("Config reloaded: exchange=%s, AI=Quota Concierge sidecar", s.cfg.Exchange)
}

// ============ SYSTEM ENDPOINTS ============

func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// SECURITY: Set CORS only for allowed origins
	origin := r.Header.Get("Origin")
	for _, allowedOrigin := range s.cfg.AllowedOrigins {
		if origin == allowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			break
		}
	}

	// Flush now to send headers
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}
	flusher.Flush()

	// Get broadcaster and subscribe
	broadcaster := logger.GetBroadcaster()
	ch, history := broadcaster.Subscribe()
	defer broadcaster.Unsubscribe(ch)

	// Send history
	for _, msg := range history {
		data, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	flusher.Flush()

	// Stream new logs
	// Create a context that cancels when the client disconnects
	ctx := r.Context()

	for {
		select {
		case msg := <-ch:
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

		case <-ctx.Done():
			return
		}
	}
}
