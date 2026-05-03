package main

import (
	"fmt"
	"log/slog"
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
	OrderID    string         `json:"orderId"`
	Platform   string         `json:"platform"`
	EntryPrice float64        `json:"entryPrice"`
	Size       float64        `json:"size"`
	Route      *Route         `json:"route"`
	Position   *Position      `json:"position"`
	AIAnalysis *ConsensusResult `json:"aiAnalysis,omitempty"`
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
	platforms := cfg.Platforms.PredictionMarkets
	userID := "cli-user"
	return &TradingBot{
		config:      cfg,
		memory:      mem,
		positions:   NewPositionManager(5000),
		router:      NewSmartRouter(platforms, "balanced"),
		marketIndex: NewMarketIndex(platforms, true),
		aiEngine:    NewAIEngine(cfg, mem),
		riskMgr:     NewRiskManager(cfg, mem, userID),
		cache:       NewCache(),
		userID:      userID,
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
		b.riskMgr.RecordTrade(e.PnL.PnL)
		b.dailyPnL += e.PnL.PnL
		b.memory.LogDaily(b.userID, time.Now(), 1, e.PnL.PnL,
			fmt.Sprintf("Stop loss triggered on %s", e.Position.Market))
	})

	b.positions.On("takeProfitTriggered", func(e PositionEvent) {
		slog.Info("Take profit triggered",
			"market", e.Position.Market,
			"price", e.Position.CurrentPrice,
			"pnl", e.PnL.PnL,
		)
		b.riskMgr.RecordTrade(e.PnL.PnL)
		b.dailyPnL += e.PnL.PnL
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

// ExecuteOrder runs risk checks, optional AI analysis, then places the order.
func (b *TradingBot) ExecuteOrder(params OrderParams) (*OrderResult, error) {
	slog.Info("Executing order",
		"side", params.Side,
		"market", params.Market,
		"size", params.Size,
	)

	// Risk check (no AI confidence yet at this stage — 0 = skip confidence check)
	if err := b.riskMgr.CheckOrder(params.Size, 0); err != nil {
		return nil, err
	}

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
	b.riskMgr.RecordTrade(0) // record trade event (P&L settled later)

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

// AnalyzeMarket runs AI consensus analysis on a market, with caching.
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

	// Cache for 5 minutes
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
		RiskStatus:  b.riskMgr.Status(),
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

func (b *TradingBot) checkTradingLimits() error {
	summary := b.positions.GetSummary()
	if summary.Count >= b.config.Risk.MaxPositions {
		return fmt.Errorf("max positions (%d) reached", b.config.Risk.MaxPositions)
	}
	return nil
}

func (b *TradingBot) UpdateDailyPnL(pnl float64) {
	b.dailyPnL += pnl
	b.riskMgr.RecordTrade(pnl)
}

func (b *TradingBot) Stop() {
	slog.Info("Stopping trading bot")
	b.positions.Stop()
	b.memory.Close()
	b.isActive = false
	slog.Info("Trading bot stopped")
}
