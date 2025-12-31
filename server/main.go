package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"auto-trader-ahh/api"
	"auto-trader-ahh/config"
	"auto-trader-ahh/store"
	"auto-trader-ahh/trader"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║      Passive Income Ahh - AI-Powered Trading System        ║")
	fmt.Println("║        OpenRouter + Binance Futures                        ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Load configuration
	cfg := config.Load()

	// Validate configuration
	if cfg.OpenRouterAPIKey == "" {
		log.Fatal("OPENROUTER_API_KEY is required. Set it in .env file or environment.")
	}

	log.Printf("Configuration loaded:")
	log.Printf("  - AI Model: %s", cfg.OpenRouterModel)
	log.Printf("  - Binance Testnet: %v", cfg.BinanceTestnet)
	log.Printf("  - Default Trading Pairs: %v", cfg.TradingPairs)
	log.Printf("  - Default Leverage: %dx", cfg.Leverage)
	log.Printf("  - Default Interval: %d minutes", cfg.TradingInterval)
	fmt.Println()

	// Initialize database
	if err := store.Init("data"); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer store.Close()

	// Create engine manager
	engineManager := trader.NewEngineManager(cfg)

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start API server in background
	server := api.NewServer(cfg.APIPort, engineManager, cfg)
	go func() {
		if err := server.Start(); err != nil {
			log.Printf("API server error: %v", err)
		}
	}()

	log.Printf("API server running on http://localhost:%s", cfg.APIPort)
	log.Println()
	log.Println("Endpoints:")
	log.Println("  - GET  /api/health                 - Health check")
	log.Println("  - GET  /api/strategies             - List strategies")
	log.Println("  - POST /api/strategies             - Create strategy")
	log.Println("  - GET  /api/traders                - List traders")
	log.Println("  - POST /api/traders                - Create trader")
	log.Println("  - POST /api/traders/{id}/start     - Start trader")
	log.Println("  - POST /api/traders/{id}/stop      - Stop trader")
	log.Println("  - GET  /api/status?trader_id=x     - Get trader status")
	log.Println("  - GET  /api/positions?trader_id=x  - Get positions")
	log.Println("  - GET  /api/decisions?trader_id=x  - Get decisions")
	log.Println("  - GET  /api/backtest               - List backtests")
	log.Println("  - POST /api/backtest/start         - Start backtest")
	log.Println("  - GET  /api/debate/sessions        - List debates")
	log.Println("  - POST /api/debate/sessions        - Create debate")
	log.Println()
	log.Println("Press Ctrl+C to stop")
	fmt.Println()

	// Wait for shutdown signal
	<-sigCh
	log.Println("\nShutdown signal received...")

	// Stop all engines
	engineManager.StopAll()

	log.Println("Goodbye!")
}
