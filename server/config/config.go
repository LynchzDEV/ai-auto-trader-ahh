package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	// OpenRouter AI
	OpenRouterAPIKey string
	OpenRouterModel  string

	// Binance Futures
	BinanceAPIKey    string
	BinanceSecretKey string
	BinanceTestnet   bool

	// Trading Settings
	TradingPairs    []string
	Leverage        int
	MaxPositionPct  float64 // Max % of balance per position
	TradingInterval int     // Minutes between AI decisions

	// Intel Providers
	CoinAnkAPIKey string

	// Server
	APIPort        string
	AllowedOrigins []string // CORS allowed origins

	// Authentication
	AccessPasskey string
}

var cfg *Config

func Load() *Config {
	godotenv.Load()

	cfg = &Config{
		// OpenRouter
		OpenRouterAPIKey: getEnv("OPENROUTER_API_KEY", ""),
		OpenRouterModel:  getEnv("OPENROUTER_MODEL", "deepseek/deepseek-v3.2"),

		// Binance
		BinanceAPIKey:    getEnv("BINANCE_API_KEY", ""),
		BinanceSecretKey: getEnv("BINANCE_SECRET_KEY", ""),
		BinanceTestnet:   getEnvBool("BINANCE_TESTNET", true),

		// Intel
		CoinAnkAPIKey: getEnv("COINANK_API_KEY", ""),

		// Trading
		TradingPairs:    []string{"BTCUSDT", "ETHUSDT"},
		Leverage:        getEnvInt("LEVERAGE", 5),
		MaxPositionPct:  getEnvFloat("MAX_POSITION_PCT", 10.0),
		TradingInterval: getEnvInt("TRADING_INTERVAL", 5),

		// Server
		APIPort:        getEnv("API_PORT", "8080"),
		AllowedOrigins: getEnvSlice("ALLOWED_ORIGINS", []string{"http://localhost:3000"}),

		// Authentication
		AccessPasskey: getEnv("ACCESS_PASSKEY", ""),
	}

	// SECURITY: Require ACCESS_PASSKEY in production mode
	if !cfg.BinanceTestnet && cfg.AccessPasskey == "" {
		log.Fatal("FATAL: ACCESS_PASSKEY is required when BINANCE_TESTNET=false (production mode)")
	}

	// SECURITY: Enforce minimum passkey length
	if cfg.AccessPasskey != "" && len(cfg.AccessPasskey) < 32 {
		log.Fatal(fmt.Sprintf("FATAL: ACCESS_PASSKEY must be at least 32 characters (current: %d)", len(cfg.AccessPasskey)))
	}

	return cfg
}

func Get() *Config {
	if cfg == nil {
		Load()
	}
	return cfg
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		b, err := strconv.ParseBool(val)
		if err == nil {
			return b
		}
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		i, err := strconv.Atoi(val)
		if err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		f, err := strconv.ParseFloat(val, 64)
		if err == nil {
			return f
		}
	}
	return defaultVal
}

func getEnvSlice(key string, defaultVal []string) []string {
	if val := os.Getenv(key); val != "" {
		// Split by comma and trim whitespace
		parts := strings.Split(val, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	}
	return defaultVal
}
