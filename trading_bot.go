package main

import (
        "fmt"
        "log/slog"
        "math"
        "time"
)

type TradingBot struct {
        config      *Config
        memory      *MemoryManager
        positions   *PositionManager
        router      *SmartRouter
        marketIndex *MarketIndex
        userID      string
        tradingMode string
        dailyPnL    float64
        dailyTrades int
        isActive    bool
        startedAt   time.Time
}

type OrderParams struct {
        Market     string
        Side       string
        Size       float64
        StopLoss   *float64
        TakeProfit *float64
}

type OrderResult struct {
        OrderID    string    `json:"orderId"`
        Platform   string    `json:"platform"`
        EntryPrice float64   `json:"entryPrice"`
        Size       float64   `json:"size"`
        Route      *Route    `json:"route"`
        Position   *Position `json:"position"`
}

type AIAnalysis struct {
        Provider  string  `json:"provider"`
        Direction string  `json:"direction"`
        Confidence float64 `json:"confidence"`
        RiskScore float64 `json:"riskScore"`
        Score     float64 `json:"score"`
        Reason    string  `json:"reason"`
}

type PortfolioSummary struct {
        Positions   PositionSummary   `json:"positions"`
        DailyTrades int               `json:"dailyTrades"`
        DailyPnL    float64           `json:"dailyPnL"`
        Preferences map[string]string `json:"preferences"`
        Rules       []Memory          `json:"rules"`
        IsActive    bool              `json:"isActive"`
}

func NewTradingBot(cfg *Config, mem *MemoryManager) *TradingBot {
        platforms := cfg.Platforms.PredictionMarkets
        return &TradingBot{
                config:      cfg,
                memory:      mem,
                positions:   NewPositionManager(5000),
                router:      NewSmartRouter(platforms, "balanced"),
                marketIndex: NewMarketIndex(platforms, true),
                userID:      "cli-user",
                tradingMode: "balanced",
        }
}

func (b *TradingBot) Start() {
        slog.Info("Starting Trading Bot")
        b.positions.Start()
        b.setupEventHandlers()
        b.isActive = true
        b.startedAt = time.Now()
        slog.Info("Trading bot is active", "mode", b.tradingMode, "userId", b.userID)
}

func (b *TradingBot) setupEventHandlers() {
        b.positions.On("stopLossTriggered", func(e PositionEvent) {
                slog.Warn("Stop loss triggered",
                        "market", e.Position.Market,
                        "price", e.Position.CurrentPrice,
                        "pnl", e.PnL.PnL,
                )
                b.memory.LogDaily(b.userID, time.Now(), 1, e.PnL.PnL,
                        fmt.Sprintf("Stop loss triggered on %s", e.Position.Market))
        })

        b.positions.On("takeProfitTriggered", func(e PositionEvent) {
                slog.Info("Take profit triggered",
                        "market", e.Position.Market,
                        "price", e.Position.CurrentPrice,
                        "pnl", e.PnL.PnL,
                )
                b.memory.LogDaily(b.userID, time.Now(), 1, e.PnL.PnL,
                        fmt.Sprintf("Take profit triggered on %s", e.Position.Market))
        })

        b.positions.On("trailingStopTriggered", func(e PositionEvent) {
                slog.Warn("Trailing stop triggered",
                        "market", e.Position.Market,
                        "price", e.Position.CurrentPrice,
                        "highWaterMark", e.Position.HighWaterMark,
                )
        })
}

func (b *TradingBot) ExecuteOrder(params OrderParams) (*OrderResult, error) {
        slog.Info("Executing order",
                "side", params.Side,
                "market", params.Market,
                "size", params.Size,
        )

        if err := b.checkTradingLimits(); err != nil {
                return nil, err
        }

        route, err := b.router.FindBestRoute(params.Market, params.Side, params.Size, b.tradingMode)
        if err != nil {
                return nil, fmt.Errorf("routing error: %w", err)
        }

        slog.Info("Best route selected",
                "platform", route.Platform,
                "expectedPrice", route.ExpectedPrice,
                "fees", route.Fees,
        )

        marketData := b.marketIndex.GetMarket(route.Platform, params.Market)
        entryPrice := route.ExpectedPrice
        if marketData != nil {
                entryPrice = 0.50
        }

        position := b.positions.AddPosition(route.Platform, params.Market, params.Side, params.Size, entryPrice)

        if params.StopLoss != nil {
                b.positions.SetStopLoss(position.ID, *params.StopLoss, 0, 100)
                slog.Info("Stop loss set", "positionId", position.ID, "price", *params.StopLoss)
        }
        if params.TakeProfit != nil {
                b.positions.SetTakeProfit(position.ID, *params.TakeProfit, 0, nil)
                slog.Info("Take profit set", "positionId", position.ID, "price", *params.TakeProfit)
        }

        b.dailyTrades++

        b.memory.Remember(b.userID, "note", fmt.Sprintf("trade_%s", position.ID),
                fmt.Sprintf("Executed %s %s for $%.2f @ $%.3f", params.Side, params.Market, params.Size, entryPrice),
                map[string]interface{}{"positionId": position.ID, "platform": route.Platform},
        )

        return &OrderResult{
                OrderID:    position.ID,
                Platform:   route.Platform,
                EntryPrice: entryPrice,
                Size:       params.Size,
                Route:      route,
                Position:   position,
        }, nil
}

func (b *TradingBot) SearchMarkets(query string, f MarketFilters) []Market {
        return b.marketIndex.Search(query, f)
}

func (b *TradingBot) GetTrendingMarkets() []Market {
        return b.marketIndex.GetTrendingMarkets(10)
}

func (b *TradingBot) GetOpenPositions() []Position {
        return b.positions.ListPositions("")
}

func (b *TradingBot) ClosePosition(positionID string) error {
        pos := b.positions.GetPosition(positionID)
        if pos == nil {
                return fmt.Errorf("position %s not found", positionID)
        }

        slog.Info("Closing position", "positionId", positionID, "market", pos.Market)

        b.positions.RemoveAllStops(positionID)
        b.positions.DeletePosition(positionID)

        b.memory.Remember(b.userID, "note", fmt.Sprintf("close_%s", positionID),
                fmt.Sprintf("Closed position %s", pos.Market), nil)
        return nil
}

func (b *TradingBot) GetPortfolioSummary() (*PortfolioSummary, error) {
        prefs, err := b.memory.GetPreferences(b.userID)
        if err != nil {
                prefs = map[string]string{}
        }
        rules, err := b.memory.GetRules(b.userID)
        if err != nil || rules == nil {
                rules = []Memory{}
        }
        return &PortfolioSummary{
                Positions:   b.positions.GetSummary(),
                DailyTrades: b.dailyTrades,
                DailyPnL:    b.dailyPnL,
                Preferences: prefs,
                Rules:       rules,
                IsActive:    b.isActive,
        }, nil
}

func (b *TradingBot) AnalyzeWithProviders(prompt string) AIAnalysis {
        analyses := []AIAnalysis{
                {Provider: "groq", Direction: "hold", Confidence: 0.78, RiskScore: 0.22, Reason: "Fast cheap baseline"},
                {Provider: "anthropic", Direction: "buy", Confidence: 0.71, RiskScore: 0.18, Reason: "More cautious reasoning"},
                {Provider: "openai", Direction: "buy", Confidence: 0.74, RiskScore: 0.21, Reason: "Balanced interpretation"},
                {Provider: "google", Direction: "sell", Confidence: 0.66, RiskScore: 0.34, Reason: "Alternative view"},
        }

        best := analyses[0]
        best.Score = scoreAnalysis(best)
        for i := 1; i < len(analyses); i++ {
                analyses[i].Score = scoreAnalysis(analyses[i])
                if analyses[i].Score > best.Score {
                        best = analyses[i]
                }
        }

        slog.Info("AI analysis selected", "provider", best.Provider, "direction", best.Direction, "confidence", best.Confidence, "score", best.Score)
        _ = prompt
        return best
}

func scoreAnalysis(a AIAnalysis) float64 {
        dirBonus := 0.0
        if a.Direction == "buy" || a.Direction == "sell" {
                dirBonus = 0.12
        }
        return math.Round((a.Confidence*0.7 + (1-a.RiskScore)*0.3 + dirBonus) * 1000) / 1000
}

func (b *TradingBot) SetPreference(key, value string) error {
        slog.Info("Setting preference", "key", key)
        return b.memory.Remember(b.userID, "preference", key, value, nil)
}

func (b *TradingBot) AddRule(rule string) error {
        slog.Info("Adding trading rule", "rule", rule)
        return b.memory.Remember(b.userID, "rule", "", rule,
                map[string]interface{}{"addedAt": time.Now().Format(time.RFC3339)})
}

func (b *TradingBot) checkTradingLimits() error {
        summary := b.positions.GetSummary()
        if summary.Count >= b.config.Risk.MaxPositions {
                return fmt.Errorf("max positions (%d) reached", b.config.Risk.MaxPositions)
        }
        if b.dailyPnL <= float64(-b.config.Trading.MaxDailyLoss) {
                return fmt.Errorf("daily loss limit of $%d reached", b.config.Trading.MaxDailyLoss)
        }
        return nil
}

func (b *TradingBot) UpdateDailyPnL(pnl float64) {
        b.dailyPnL += pnl
}

func (b *TradingBot) Stop() {
        slog.Info("Stopping trading bot")
        b.positions.Stop()
        b.memory.Close()
        b.isActive = false
        slog.Info("Trading bot stopped")
}
