package backtest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"auto-trader/mcp"
)

// Manager manages multiple backtest runs
type Manager struct {
	runners  map[string]*Runner
	metadata map[string]*RunMetadata
	cancels  map[string]context.CancelFunc
	client   mcp.AIClient
	mu       sync.RWMutex
}

// NewManager creates a new backtest manager
func NewManager(client mcp.AIClient) *Manager {
	return &Manager{
		runners:  make(map[string]*Runner),
		metadata: make(map[string]*RunMetadata),
		cancels:  make(map[string]context.CancelFunc),
		client:   client,
	}
}

// Start starts a new backtest run
func (m *Manager) Start(ctx context.Context, cfg *Config) (string, error) {
	if cfg.RunID == "" {
		cfg.RunID = fmt.Sprintf("bt_%d", time.Now().UnixNano())
	}

	m.mu.Lock()
	if _, exists := m.runners[cfg.RunID]; exists {
		m.mu.Unlock()
		return "", fmt.Errorf("backtest %s already exists", cfg.RunID)
	}

	runner := NewRunner(cfg, m.client)
	m.runners[cfg.RunID] = runner
	m.metadata[cfg.RunID] = runner.GetMetadata()
	m.mu.Unlock()

	// Start in background
	go func() {
		runCtx, cancel := context.WithCancel(ctx)
		m.mu.Lock()
		m.cancels[cfg.RunID] = cancel
		m.mu.Unlock()

		if err := runner.Start(runCtx); err != nil {
			fmt.Printf("Backtest %s failed: %v\n", cfg.RunID, err)
		}

		// Update metadata
		m.mu.Lock()
		m.metadata[cfg.RunID] = runner.GetMetadata()
		m.mu.Unlock()
	}()

	return cfg.RunID, nil
}

// Stop stops a running backtest
func (m *Manager) Stop(runID string) error {
	m.mu.RLock()
	cancel, exists := m.cancels[runID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("backtest %s not found", runID)
	}

	cancel()
	return nil
}

// GetStatus returns the status of a backtest
func (m *Manager) GetStatus(runID string) (*RunMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runner, exists := m.runners[runID]
	if !exists {
		return nil, fmt.Errorf("backtest %s not found", runID)
	}

	return runner.GetMetadata(), nil
}

// GetMetrics returns the metrics of a backtest
func (m *Manager) GetMetrics(runID string) (*Metrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runner, exists := m.runners[runID]
	if !exists {
		return nil, fmt.Errorf("backtest %s not found", runID)
	}

	return runner.GetMetrics(), nil
}

// GetEquityCurve returns the equity curve of a backtest
func (m *Manager) GetEquityCurve(runID string) ([]EquityPoint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runner, exists := m.runners[runID]
	if !exists {
		return nil, fmt.Errorf("backtest %s not found", runID)
	}

	return runner.GetEquityCurve(), nil
}

// GetTrades returns the trades of a backtest
func (m *Manager) GetTrades(runID string) ([]TradeEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runner, exists := m.runners[runID]
	if !exists {
		return nil, fmt.Errorf("backtest %s not found", runID)
	}

	return runner.GetTrades(), nil
}

// ListRuns returns all backtest runs
func (m *Manager) ListRuns() []*RunMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var runs []*RunMetadata
	for _, runner := range m.runners {
		runs = append(runs, runner.GetMetadata())
	}
	return runs
}

// Delete removes a completed backtest
func (m *Manager) Delete(runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	runner, exists := m.runners[runID]
	if !exists {
		return fmt.Errorf("backtest %s not found", runID)
	}

	meta := runner.GetMetadata()
	if meta.Status == StatusRunning {
		return fmt.Errorf("cannot delete running backtest")
	}

	delete(m.runners, runID)
	delete(m.metadata, runID)
	delete(m.cancels, runID)

	return nil
}

// LoadKlines loads klines for a backtest
func (m *Manager) LoadKlines(runID, symbol string, klines []Kline) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runner, exists := m.runners[runID]
	if !exists {
		return fmt.Errorf("backtest %s not found", runID)
	}

	runner.LoadKlines(symbol, klines)
	return nil
}
