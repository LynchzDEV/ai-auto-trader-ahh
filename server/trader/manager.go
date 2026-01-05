package trader

import (
	"context"
	"fmt"
	"log"
	"sync"

	"auto-trader-ahh/ai"
	"auto-trader-ahh/config"
	"auto-trader-ahh/exchange"
	"auto-trader-ahh/store"
)

// EngineManager manages multiple trading engine instances
type EngineManager struct {
	cfg           *config.Config
	engines       map[string]*Engine
	traderStore   *store.TraderStore
	strategyStore *store.StrategyStore
	mu            sync.RWMutex
}

func NewEngineManager(cfg *config.Config) *EngineManager {
	return &EngineManager{
		cfg:           cfg,
		engines:       make(map[string]*Engine),
		traderStore:   store.NewTraderStore(),
		strategyStore: store.NewStrategyStore(),
	}
}

// Start starts a trader by ID
func (m *EngineManager) Start(traderID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already running
	if engine, exists := m.engines[traderID]; exists && engine.IsRunning() {
		return fmt.Errorf("trader %s is already running", traderID)
	}

	// Load trader from database
	trader, err := m.traderStore.Get(traderID)
	if err != nil {
		return fmt.Errorf("failed to load trader: %w", err)
	}

	// Load strategy
	var strategy *store.Strategy
	if trader.StrategyID != "" {
		strategy, err = m.strategyStore.Get(trader.StrategyID)
		if err != nil {
			log.Printf("Warning: failed to load strategy %s, using default", trader.StrategyID)
			strategy, _ = m.strategyStore.GetActive()
		}
	} else {
		strategy, _ = m.strategyStore.GetActive()
	}

	// Create AI client (using trader-specific settings or fallback to global)
	apiKey := trader.Config.OpenRouterAPIKey
	if apiKey == "" {
		apiKey = m.cfg.OpenRouterAPIKey
	}
	model := trader.Config.OpenRouterModel
	if model == "" {
		model = trader.Config.AIModel // Legacy field
	}
	if model == "" {
		model = m.cfg.OpenRouterModel
	}
	aiClient := ai.NewClient(apiKey, model)

	// Create exchange client
	binanceKey := trader.Config.APIKey
	binanceSecret := trader.Config.SecretKey
	testnet := trader.Config.Testnet
	if binanceKey == "" {
		binanceKey = m.cfg.BinanceAPIKey
		binanceSecret = m.cfg.BinanceSecretKey
		testnet = m.cfg.BinanceTestnet
	}
	binanceClient := exchange.NewBinanceClient(binanceKey, binanceSecret, testnet)

	// Create engine
	engine := NewEngine(traderID, trader.Name, aiClient, binanceClient, strategy, &trader.Config, m.cfg)

	// Start engine
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start engine: %w", err)
	}

	m.engines[traderID] = engine
	log.Printf("Started trader: %s (%s)", trader.Name, traderID)
	return nil
}

// Stop stops a trader by ID
func (m *EngineManager) Stop(traderID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if engine, exists := m.engines[traderID]; exists {
		engine.Stop()
		delete(m.engines, traderID)
		log.Printf("Stopped trader: %s", traderID)
	}
}

// StopAll stops all running traders
func (m *EngineManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, engine := range m.engines {
		engine.Stop()
		log.Printf("Stopped trader: %s", id)
	}
	m.engines = make(map[string]*Engine)
}

// IsRunning checks if a trader is running
func (m *EngineManager) IsRunning(traderID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if engine, exists := m.engines[traderID]; exists {
		return engine.IsRunning()
	}
	return false
}

// GetStatus returns the status of a trader
func (m *EngineManager) GetStatus(traderID string) map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if engine, exists := m.engines[traderID]; exists {
		return engine.GetStatus()
	}

	return map[string]interface{}{
		"running":   false,
		"trader_id": traderID,
		"message":   "Trader not running",
	}
}

// GetAccount returns account info for a trader
func (m *EngineManager) GetAccount(traderID string) map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if engine, exists := m.engines[traderID]; exists {
		return engine.GetAccount()
	}

	return map[string]interface{}{
		"error": "Trader not running",
	}
}

// GetPositions returns positions for a trader
func (m *EngineManager) GetPositions(traderID string) []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if engine, exists := m.engines[traderID]; exists {
		return engine.GetPositions()
	}

	return []map[string]interface{}{}
}

// GetRunningTraders returns list of running trader IDs
func (m *EngineManager) GetRunningTraders() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.engines))
	for id := range m.engines {
		ids = append(ids, id)
	}
	return ids
}
