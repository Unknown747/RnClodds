package main

import (
	"fmt"
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

type PortfolioSummary struct {
	Positions   PositionSummary    `json:"positions"`
	DailyTrades int                `json:"dailyTrades"`
	DailyPnL    float64            `json:"dailyPnL"`
	Preferences map[string]string  `json:"preferences"`
	Rules       []Memory           `json:"rules"`
	IsActive    bool               `json:"isActive"`
}

func NewTradingBot(cfg *Config, mem *MemoryManager) *TradingBot {
	platforms := cfg.Platforms.PredictionMarkets

	bot := &TradingBot{
		config:      cfg,
		memory:      mem,
		positions:   NewPositionManager(5000),
		router:      NewSmartRouter(platforms, "balanced"),
		marketIndex: NewMarketIndex(platforms, true),
		userID:      "cli-user",
		tradingMode: "balanced",
	}
	return bot
}

func (b *TradingBot) Start() {
	fmt.Println("🚀 Starting Trading Bot...")
	b.positions.Start()
	b.setupEventHandlers()
	b.isActive = true
	b.startedAt = time.Now()
	fmt.Println("✅ Trading bot is active")
}

func (b *TradingBot) setupEventHandlers() {
	b.positions.On("stopLossTriggered", func(e PositionEvent) {
		fmt.Printf("🛑 STOP LOSS TRIGGERED: %s @ %.3f\n", e.Position.Market, e.Position.CurrentPrice)
		if e.PnL != nil {
			b.memory.LogDaily(b.userID, time.Now(), 1, e.PnL.PnL,
				fmt.Sprintf("Stop loss triggered on %s", e.Position.Market))
		}
	})

	b.positions.On("takeProfitTriggered", func(e PositionEvent) {
		fmt.Printf("✅ TAKE PROFIT TRIGGERED: %s @ %.3f\n", e.Position.Market, e.Position.CurrentPrice)
		if e.PnL != nil {
			b.memory.LogDaily(b.userID, time.Now(), 1, e.PnL.PnL,
				fmt.Sprintf("Take profit triggered on %s", e.Position.Market))
		}
	})

	b.positions.On("trailingStopTriggered", func(e PositionEvent) {
		fmt.Printf("📉 TRAILING STOP TRIGGERED: %s @ %.3f\n", e.Position.Market, e.Position.CurrentPrice)
	})
}

func (b *TradingBot) ExecuteOrder(params OrderParams) (*OrderResult, error) {
	fmt.Printf("📊 Executing order: %s %s for $%.2f\n", params.Side, params.Market, params.Size)

	if err := b.checkTradingLimits(); err != nil {
		return nil, err
	}

	route, err := b.router.FindBestRoute(params.Market, params.Side, params.Size, b.tradingMode)
	if err != nil {
		return nil, fmt.Errorf("routing error: %w", err)
	}

	fmt.Printf("🏆 Best route: %s @ $%.3f\n", route.Platform, route.ExpectedPrice)

	marketData := b.marketIndex.GetMarket(route.Platform, params.Market)
	entryPrice := route.ExpectedPrice
	if marketData != nil {
		entryPrice = 0.50
	}

	position := b.positions.AddPosition(route.Platform, params.Market, params.Side, params.Size, entryPrice)

	if params.StopLoss != nil {
		b.positions.SetStopLoss(position.ID, *params.StopLoss, 0, 100)
	}
	if params.TakeProfit != nil {
		b.positions.SetTakeProfit(position.ID, *params.TakeProfit, 0, nil)
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

	fmt.Printf("Closing position %s at current price\n", positionID)

	b.positions.RemoveAllStops(positionID)
	b.positions.DeletePosition(positionID)

	b.memory.Remember(b.userID, "note", fmt.Sprintf("close_%s", positionID),
		fmt.Sprintf("Closed position %s", pos.Market),
		nil,
	)
	return nil
}

func (b *TradingBot) GetPortfolioSummary() (*PortfolioSummary, error) {
	positionsSummary := b.positions.GetSummary()

	prefs, err := b.memory.GetPreferences(b.userID)
	if err != nil {
		prefs = map[string]string{}
	}

	rules, err := b.memory.GetRules(b.userID)
	if err != nil {
		rules = []Memory{}
	}
	if rules == nil {
		rules = []Memory{}
	}

	return &PortfolioSummary{
		Positions:   positionsSummary,
		DailyTrades: b.dailyTrades,
		DailyPnL:    b.dailyPnL,
		Preferences: prefs,
		Rules:       rules,
		IsActive:    b.isActive,
	}, nil
}

func (b *TradingBot) SetPreference(key, value string) error {
	return b.memory.Remember(b.userID, "preference", key, value, nil)
}

func (b *TradingBot) AddRule(rule string) error {
	return b.memory.Remember(b.userID, "rule", "", rule,
		map[string]interface{}{"addedAt": time.Now().Format(time.RFC3339)},
	)
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
	fmt.Println("🛑 Stopping trading bot...")
	b.positions.Stop()
	b.memory.Close()
	b.isActive = false
	fmt.Println("✅ Trading bot stopped")
}
