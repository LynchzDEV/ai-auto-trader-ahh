package trader

import (
	"context"
	"log"
	"time"

	"auto-trader-ahh/exchange"
	"auto-trader-ahh/store"
)

// runCopyTradingCycle runs a lightweight cycle for Copy Trading mode
// It only monitors status, balance, and positions without executing any strategy
func (e *Engine) runCopyTradingCycle(ctx context.Context) {
	log.Printf("[%s] === Copy Trading Mode: Monitoring ===", e.name)

	// 1. Check Copy Trading Status
	status, err := e.binance.GetCopyTradingStatus(ctx)
	if err != nil {
		log.Printf("[%s] Error checking copy trading status: %v", e.name, err)
	} else {
		log.Printf("[%s] Status: LeadTrader=%v, CopyTrader=%v", e.name, status.IsLeadTrader, status.IsCopyTrader)
	}

	// 2. Sync Account Info (Balance)
	account, err := e.binance.GetAccountInfo(ctx)
	if err != nil {
		log.Printf("[%s] Error getting account info: %v", e.name, err)
	} else {
		e.mu.Lock()
		e.account = account

		// Save equity snapshot for history/charts
		e.equityStore.Save(&store.EquitySnapshot{
			TraderID:      e.id,
			Timestamp:     time.Now(),
			TotalEquity:   account.TotalMarginBalance,
			Balance:       account.TotalWalletBalance,
			UnrealizedPnL: account.TotalUnrealizedProfit,
		})
		e.mu.Unlock()

		log.Printf("[%s] Balance: $%.2f, Equity: $%.2f, Unrealized PnL: $%.2f",
			e.name, account.TotalWalletBalance, account.TotalMarginBalance, account.TotalUnrealizedProfit)
	}

	// 3. Sync Positions
	positions, err := e.binance.GetPositions(ctx)
	if err != nil {
		log.Printf("[%s] Error getting positions: %v", e.name, err)
	} else {
		e.mu.Lock()
		e.positions = make(map[string]*exchange.Position)
		activeCount := 0
		for i := range positions {
			e.positions[positions[i].Symbol] = &positions[i]
			if positions[i].PositionAmt != 0 {
				activeCount++
				log.Printf("[%s] Active Position: %s %s %.4f (PnL: $%.2f)",
					e.name, positions[i].Symbol, positions[i].PositionSide, positions[i].PositionAmt, positions[i].UnrealizedProfit)
			}
		}
		e.mu.Unlock()
		log.Printf("[%s] Synced %d active positions", e.name, activeCount)
	}

	// 4. Sync Trade History (to capture copy executions)
	e.syncTradeHistory(ctx)

	log.Printf("[%s] === Copy Trading Cycle Complete ===", e.name)
}
