package main

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	APIKeys struct {
		Anthropic string
		OpenAI    string
		Google    string
		Groq      string
	}
	AI struct {
		Providers    []string
		DefaultModel string
	}
	Trading struct {
		DefaultPositionSize int
		MaxPositionSize     int
		MaxDailyLoss        int
		MaxDailyTrades      int
		MaxLeverage         int
	}
	Risk struct {
		MaxDrawdown          float64
		MaxPositions         int
		MinConfidence        float64
		KellyFraction        float64 // scales Kelly bet (0.25 = quarter Kelly, safer)
		MinConsensusProviders int     // min AI providers that must agree on direction
		ConsecutiveLossLimit int     // after N losses, raise confidence threshold
	}
	Scanner struct {
		Enabled      bool
		IntervalMins int
		MinSignalConf float64 // min confidence to queue a signal
		MaxSignals   int     // max signals to keep in memory
	}
	Platforms struct {
		PredictionMarkets []string
		Futures           []string
		DEX               []string
	}
	Server struct {
		Port string
		Host string
	}
	DBPath      string
	TradingMode string
	UserID      string
}

func LoadConfig() *Config {
	// Load .env for local defaults — real secrets must be set as Replit Secrets
	godotenv.Load()

	c := &Config{}

	// AI provider API keys (set via Replit Secrets)
	c.APIKeys.Anthropic = os.Getenv("ANTHROPIC_API_KEY")
	c.APIKeys.OpenAI = os.Getenv("OPENAI_API_KEY")
	c.APIKeys.Google = os.Getenv("GOOGLE_API_KEY")
	c.APIKeys.Groq = os.Getenv("GROQ_API_KEY")

	c.AI.Providers = []string{"google", "groq", "anthropic", "openai"}
	c.AI.DefaultModel = getenv("AI_DEFAULT_MODEL", "google")

	// Trading limits
	c.Trading.DefaultPositionSize = envInt("DEFAULT_POSITION_SIZE", 100)
	c.Trading.MaxPositionSize = envInt("MAX_POSITION_SIZE", 1000)
	c.Trading.MaxDailyLoss = envInt("MAX_DAILY_LOSS", 500)
	c.Trading.MaxDailyTrades = envInt("MAX_DAILY_TRADES", 20)
	c.Trading.MaxLeverage = envInt("MAX_LEVERAGE", 5)

	// Risk parameters
	c.Risk.MaxDrawdown = envFloat("MAX_DRAWDOWN", 20.0)
	c.Risk.MaxPositions = envInt("MAX_POSITIONS", 10)
	c.Risk.MinConfidence = envFloat("MIN_CONFIDENCE", 0.6)
	c.Risk.KellyFraction = envFloat("KELLY_FRACTION", 0.25)      // quarter Kelly — conservative
	c.Risk.MinConsensusProviders = envInt("MIN_CONSENSUS", 1)     // require N providers to agree
	c.Risk.ConsecutiveLossLimit = envInt("CONSECUTIVE_LOSS_LIMIT", 3) // raise threshold after 3 losses

	// Opportunity scanner
	c.Scanner.Enabled = getenv("SCANNER_ENABLED", "true") == "true"
	c.Scanner.IntervalMins = envInt("SCANNER_INTERVAL_MINS", 10)
	c.Scanner.MinSignalConf = envFloat("SCANNER_MIN_CONF", 0.75)
	c.Scanner.MaxSignals = envInt("SCANNER_MAX_SIGNALS", 20)

	// Supported platforms
	c.Platforms.PredictionMarkets = []string{"polymarket", "kalshi", "manifold"}
	c.Platforms.Futures = []string{"hyperliquid", "binance", "bybit"}
	c.Platforms.DEX = []string{"jupiter", "raydium", "pumpdotfun"}

	// Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "5000"
	}
	c.Server.Port = port
	c.Server.Host = "0.0.0.0"

	// Storage
	c.DBPath = getenv("DB_PATH", "./clodds.db")

	// Bot behaviour
	c.TradingMode = getenv("TRADING_MODE", "balanced")
	c.UserID = getenv("USER_ID", "cli-user")

	return c
}

func envInt(key string, def int) int {
	s := os.Getenv(key)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

func envFloat(key string, def float64) float64 {
	s := os.Getenv(key)
	if s == "" {
		return def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return v
}

func getenv(key, def string) string {
	s := os.Getenv(key)
	if s == "" {
		return def
	}
	return s
}
