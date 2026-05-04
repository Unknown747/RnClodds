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
        aiEngine    *AIEngine
        riskMgr     *RiskManager
        cache       *Cache
        scanner     *OpportunityScanner
        userID      string
        tradingMode string
        startedAt   time.Time
        isActive    bool
}

type OrderParams struct {
        Market     string
        Side       string
        Size       float64
        StopLoss   *float64
        TakeProfit *float64
        // If true, use Kelly Criterion to override Size with optimal sizing
        UseKelly bool
}

type OrderResult struct {
        OrderID      string           `json:"orderId"`
        Platform     string           `json:"platform"`
        EntryPrice   float64          `json:"entryPrice"`
        Size         float64          `json:"size"`
        KellySize    float64          `json:"kellySize,omitempty"`
        Route        *Route           `json:"route"`
        Position     *Position        `json:"position"`
        AIAnalysis   *ConsensusResult `json:"aiAnalysis,omitempty"`
}

type PortfolioSummary struct {
        Positions   PositionSummary   `json:"positions"`
        DailyTrades int               `json:"dailyTrades"`
        DailyPnL    float64           `json:"dailyPnL"`
        Preferences map[string]string `json:"preferences"`
        Rules       []Memory          `json:"rules"`
        IsActive    bool              `json:"isActive"`
        RiskStatus  RiskStatus        `json:"riskStatus"`
}

func NewTradingBot(cfg *Config, mem *MemoryManager) *TradingBot {
        mi := NewMarketIndex(cfg.Platforms.PredictionMarkets, true)
        ai := NewAIEngine(cfg, mem)
        return &TradingBot{
                config:      cfg,
                memory:      mem,
                positions:   NewPositionManager(5000),
                router:      NewSmartRouter(cfg.Platforms.PredictionMarkets, cfg.TradingMode, mi),
                marketIndex: mi,
                aiEngine:    ai,
                riskMgr:     NewRiskManager(cfg, mem, cfg.UserID),
                cache:       NewCache(),
                scanner:     NewOpportunityScanner(cfg, ai, mi, cfg.UserID),
                userID:      cfg.UserID,
                tradingMode: cfg.TradingMode,
        }
}

func (b *TradingBot) Start() {
        slog.Info("Starting Trading Bot")
        b.positions.Start()
        b.setupEventHandlers()
        b.scanner.Start()
        b.isActive = true
        b.startedAt = time.Now()
        slog.Info("Trading bot is active",
                "mode", b.tradingMode,
                "userId", b.userID,
                "kellyFraction", b.config.Risk.KellyFraction,
                "minConsensus", b.config.Risk.MinConsensusProviders,
        )
}

func (b *TradingBot) setupEventHandlers() {
        b.positions.On("stopLossTriggered", func(e PositionEvent) {
                slog.Warn("Stop loss triggered",
                        "market", e.Position.Market,
                        "price", e.Position.CurrentPrice,
                        "pnl", e.PnL.PnL,
                )
                b.riskMgr.RecordTrade(e.PnL.PnL)
                b.memory.LogDaily(b.userID, time.Now(), 0, e.PnL.PnL,
                        fmt.Sprintf("Stop loss on %s", e.Position.Market))
        })

        b.positions.On("takeProfitTriggered", func(e PositionEvent) {
                slog.Info("Take profit triggered",
                        "market", e.Position.Market,
                        "price", e.Position.CurrentPrice,
                        "pnl", e.PnL.PnL,
                )
                b.riskMgr.RecordTrade(e.PnL.PnL)
                b.memory.LogDaily(b.userID, time.Now(), 0, e.PnL.PnL,
                        fmt.Sprintf("Take profit on %s", e.Position.Market))
        })

        b.positions.On("trailingStopTriggered", func(e PositionEvent) {
                slog.Warn("Trailing stop triggered",
                        "market", e.Position.Market,
                        "price", e.Position.CurrentPrice,
                        "highWaterMark", e.Position.HighWaterMark,
                )
                b.riskMgr.RecordTrade(e.PnL.PnL)
                b.memory.LogDaily(b.userID, time.Now(), 0, e.PnL.PnL,
                        fmt.Sprintf("Trailing stop on %s", e.Position.Market))
        })
}

// ExecuteOrder is the full trade pipeline:
//  1. AI pre-trade consensus analysis
//  2. Consensus gate (min N providers must agree)
//  3. Kelly Criterion position sizing
//  4. Risk manager checks (limits, drawdown, adaptive threshold)
//  5. Smart routing to best platform
//  6. Open position with optional stops
func (b *TradingBot) ExecuteOrder(params OrderParams) (*OrderResult, error) {
        slog.Info("Executing order",
                "side", params.Side,
                "market", params.Market,
                "size", params.Size,
        )

        // --- Step 1: AI Pre-trade Analysis ---
        var aiResult *ConsensusResult
        aiConfidence := 0.0
        kellySize := 0.0

        analysis, err := b.AnalyzeMarket(params.Market, "")
        if err != nil {
                slog.Warn("AI pre-trade analysis skipped", "reason", err.Error())
        } else {
                aiResult = analysis
                aiConfidence = analysis.Best.Confidence

                // --- Step 2: AI Confidence Gate ---
                effectiveMinConf := b.riskMgr.Status().EffectiveMinConf
                if aiConfidence < effectiveMinConf {
                        protMode := ""
                        if b.riskMgr.Status().ConsecutiveLosses > 0 {
                                protMode = fmt.Sprintf(" [protection mode: %d consecutive losses]",
                                        b.riskMgr.Status().ConsecutiveLosses)
                        }
                        return nil, fmt.Errorf(
                                "AI confidence %.0f%% below threshold %.0f%% (direction: %s)%s",
                                aiConfidence*100, effectiveMinConf*100, analysis.Best.Direction, protMode,
                        )
                }

                // --- Step 3: Kelly Criterion Position Sizing ---
                kellySize = b.riskMgr.ComputeKellySize(aiConfidence)
                if params.UseKelly || params.Size == 0 {
                        params.Size = kellySize
                }

                slog.Info("AI analysis passed + Kelly sizing",
                        "market", params.Market,
                        "direction", analysis.Best.Direction,
                        "confidence", aiConfidence,
                        "agreingProviders", analysis.AgreingCount,
                        "totalProviders", analysis.TotalValid,
                        "kellySize", kellySize,
                        "finalSize", params.Size,
                )
        }

        // --- Step 4: Risk Manager Check ---
        if err := b.riskMgr.CheckOrder(params.Size, aiConfidence); err != nil {
                return nil, err
        }
        if err := b.checkPositionLimit(); err != nil {
                return nil, err
        }

        // --- Step 5: Smart Routing ---
        route, err := b.router.FindBestRoute(params.Market, params.Side, params.Size, b.tradingMode)
        if err != nil {
                return nil, fmt.Errorf("routing error: %w", err)
        }

        slog.Info("Best route selected",
                "platform", route.Platform,
                "expectedPrice", route.ExpectedPrice,
                "fees", route.Fees,
        )

        entryPrice := route.ExpectedPrice

        // --- Step 6: Open Position ---
        position := b.positions.AddPosition(route.Platform, params.Market, params.Side, params.Size, entryPrice)

        if params.StopLoss != nil {
                b.positions.SetStopLoss(position.ID, *params.StopLoss, 0, 100)
        }
        if params.TakeProfit != nil {
                b.positions.SetTakeProfit(position.ID, *params.TakeProfit, 0, nil)
        }

        // RiskManager is single source of truth for daily counters
        b.riskMgr.RecordTrade(0)

        // Build trade note
        aiNote := ""
        if aiResult != nil {
                aiNote = fmt.Sprintf(" [AI: %s %.0f%% conf, %d/%d agree]",
                        aiResult.Best.Direction, aiConfidence*100,
                        aiResult.AgreingCount, aiResult.TotalValid)
        }
        b.memory.Remember(b.userID, "note", fmt.Sprintf("trade_%s", position.ID),
                fmt.Sprintf("Executed %s %s $%.2f @ $%.3f%s", params.Side, params.Market, params.Size, entryPrice, aiNote),
                map[string]interface{}{"positionId": position.ID, "platform": route.Platform},
        )

        b.memory.LogDaily(b.userID, time.Now(), 1, 0,
                fmt.Sprintf("Opened %s %s $%.2f on %s%s", params.Side, params.Market, params.Size, route.Platform, aiNote))

        return &OrderResult{
                OrderID:    position.ID,
                Platform:   route.Platform,
                EntryPrice: entryPrice,
                Size:       params.Size,
                KellySize:  math.Round(kellySize*100) / 100,
                Route:      route,
                Position:   position,
                AIAnalysis: aiResult,
        }, nil
}

// AnalyzeMarket runs AI consensus analysis on a market, with short-term caching.
func (b *TradingBot) AnalyzeMarket(market, context string) (*ConsensusResult, error) {
        cacheKey := "ai:" + market
        if cached, ok := b.cache.Get(cacheKey); ok {
                slog.Info("AI analysis cache hit", "market", market)
                return cached.(*ConsensusResult), nil
        }

        result, err := b.aiEngine.Analyze(b.userID, market, context)
        if err != nil {
                return nil, err
        }

        b.cache.Set(cacheKey, result, 5*time.Minute)
        return result, nil
}

func (b *TradingBot) GetRiskStatus() RiskStatus {
        return b.riskMgr.Status()
}

func (b *TradingBot) ToggleKillSwitch(enable bool) {
        b.riskMgr.ToggleKillSwitch(enable)
}

func (b *TradingBot) RunBacktest() (*BacktestResult, error) {
        return b.riskMgr.Backtest()
}

func (b *TradingBot) SearchMarkets(query string, f MarketFilters) []Market {
        cacheKey := "markets:" + query + ":" + f.Category
        if cached, ok := b.cache.Get(cacheKey); ok {
                return cached.([]Market)
        }
        results := b.marketIndex.Search(query, f)
        b.cache.Set(cacheKey, results, 60*time.Second)
        return results
}

func (b *TradingBot) GetTrendingMarkets() []Market {
        if cached, ok := b.cache.Get("trending"); ok {
                return cached.([]Market)
        }
        markets := b.marketIndex.GetTrendingMarkets(10)
        b.cache.Set("trending", markets, 60*time.Second)
        return markets
}

func (b *TradingBot) GetOpenPositions() []Position {
        return b.positions.ListPositions("")
}

// GetLivePositions returns all open positions with real-time P&L included.
func (b *TradingBot) GetLivePositions() []LivePosition {
        return b.positions.ListPositionsLive("")
}

// UpdatePositionPrice manually sets a position's current price (overrides simulation).
func (b *TradingBot) UpdatePositionPrice(positionID string, price float64) error {
        return b.positions.UpdatePriceManual(positionID, price)
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

        riskStatus := b.riskMgr.Status()

        return &PortfolioSummary{
                Positions:   b.positions.GetSummary(),
                DailyTrades: riskStatus.DailyTrades,
                DailyPnL:    riskStatus.DailyPnL,
                Preferences: prefs,
                Rules:       rules,
                IsActive:    b.isActive,
                RiskStatus:  riskStatus,
        }, nil
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

func (b *TradingBot) checkPositionLimit() error {
        summary := b.positions.GetSummary()
        if summary.Count >= b.config.Risk.MaxPositions {
                return fmt.Errorf("max positions (%d) reached", b.config.Risk.MaxPositions)
        }
        return nil
}

func (b *TradingBot) GetAnalytics() (*AnalyticsData, error) {
        data, err := b.memory.GetAnalytics(b.userID)
        if err != nil {
                return nil, err
        }
        riskStatus := b.riskMgr.Status()
        data.DailyTrades = riskStatus.DailyTrades
        data.DailyPnL = riskStatus.DailyPnL
        return data, nil
}

// GetScannerSignals returns the latest opportunity signals from the background scanner.
func (b *TradingBot) GetScannerSignals() []Signal {
        return b.scanner.GetSignals()
}

// GetScannerStatus returns the scanner's operational status.
func (b *TradingBot) GetScannerStatus() ScannerStatus {
        return b.scanner.GetStatus()
}

// ComputeKellySize returns the Kelly-optimal position size for a given confidence level.
func (b *TradingBot) ComputeKellySize(confidence float64) float64 {
        return b.riskMgr.ComputeKellySize(confidence)
}

func (b *TradingBot) Stop() {
        slog.Info("Stopping trading bot")
        b.scanner.Stop()
        b.positions.Stop()
        b.marketIndex.Stop()
        b.cache.Stop()
        b.memory.Close()
        b.isActive = false
        slog.Info("Trading bot stopped")
}
