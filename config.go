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
        Trading struct {
                DefaultPositionSize int
                MaxPositionSize     int
                MaxDailyLoss        int
                MaxLeverage         int
        }
        Risk struct {
                MaxDrawdown   float64
                MaxPositions  int
                MinConfidence float64
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
        DBPath string
}

func LoadConfig() *Config {
        godotenv.Load()

        c := &Config{}
        c.APIKeys.Anthropic = os.Getenv("ANTHROPIC_API_KEY")
        c.APIKeys.OpenAI = os.Getenv("OPENAI_API_KEY")
        c.APIKeys.Google = os.Getenv("GOOGLE_API_KEY")
        c.APIKeys.Groq = os.Getenv("GROQ_API_KEY")

        c.Trading.DefaultPositionSize = envInt("DEFAULT_POSITION_SIZE", 100)
        c.Trading.MaxPositionSize = envInt("MAX_POSITION_SIZE", 1000)
        c.Trading.MaxDailyLoss = envInt("MAX_DAILY_LOSS", 500)
        c.Trading.MaxLeverage = 5

        c.Risk.MaxDrawdown = 20
        c.Risk.MaxPositions = 10
        c.Risk.MinConfidence = 0.6

        c.Platforms.PredictionMarkets = []string{"polymarket", "kalshi", "manifold"}
        c.Platforms.Futures = []string{"hyperliquid", "binance", "bybit"}
        c.Platforms.DEX = []string{"jupiter", "raydium", "pumpdotfun"}

        port := os.Getenv("PORT")
        if port == "" {
                port = "5000"
        }
        c.Server.Port = port
        c.Server.Host = "0.0.0.0"

        c.DBPath = os.Getenv("DB_PATH")
        if c.DBPath == "" {
                c.DBPath = "./clodds.db"
        }

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
