package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type StopLoss struct {
	Price       float64   `json:"price"`
	SizePercent float64   `json:"sizePercent"`
	SetAt       time.Time `json:"setAt"`
}

type TakeProfit struct {
	Type        string            `json:"type"`
	Price       float64           `json:"price,omitempty"`
	SizePercent float64           `json:"sizePercent,omitempty"`
	Levels      []TakeProfitLevel `json:"levels,omitempty"`
	SetAt       time.Time         `json:"setAt"`
}

type TakeProfitLevel struct {
	Price       float64 `json:"price"`
	SizePercent float64 `json:"sizePercent"`
	Triggered   bool    `json:"triggered"`
}

type TrailingStop struct {
	TrailPercent float64   `json:"trailPercent"`
	ActivateAt   float64   `json:"activateAt,omitempty"`
	IsActive     bool      `json:"isActive"`
	SetAt        time.Time `json:"setAt"`
}

type Position struct {
	ID            string        `json:"id"`
	Platform      string        `json:"platform"`
	Market        string        `json:"market"`
	Side          string        `json:"side"`
	Size          float64       `json:"size"`
	EntryPrice    float64       `json:"entryPrice"`
	CurrentPrice  float64       `json:"currentPrice"`
	StopLoss      *StopLoss     `json:"stopLoss"`
	TakeProfit    *TakeProfit   `json:"takeProfit"`
	TrailingStop  *TrailingStop `json:"trailingStop"`
	HighWaterMark float64       `json:"highWaterMark"`
	CreatedAt     time.Time     `json:"createdAt"`
	UpdatedAt     time.Time     `json:"updatedAt"`
}

type PnL struct {
	PnL        float64 `json:"pnl"`
	PnLPercent float64 `json:"pnlPercent"`
}

type PositionEvent struct {
	Type     string
	Position Position
	PnL      *PnL
	Extra    map[string]interface{}
}

type PositionSummary struct {
	Count             int     `json:"count"`
	TotalValue        float64 `json:"totalValue"`
	UnrealizedPnl     float64 `json:"unrealizedPnl"`
	WithStopLoss      int     `json:"withStopLoss"`
	WithTakeProfit    int     `json:"withTakeProfit"`
	WithTrailingStop  int     `json:"withTrailingStop"`
}

type PositionManager struct {
	mu             sync.RWMutex
	positions      map[string]*Position
	checkInterval  time.Duration
	ticker         *time.Ticker
	done           chan struct{}
	EventHandlers  map[string][]func(PositionEvent)
}

func NewPositionManager(checkIntervalMs int) *PositionManager {
	if checkIntervalMs <= 0 {
		checkIntervalMs = 5000
	}
	return &PositionManager{
		positions:     make(map[string]*Position),
		checkInterval: time.Duration(checkIntervalMs) * time.Millisecond,
		done:          make(chan struct{}),
		EventHandlers: make(map[string][]func(PositionEvent)),
	}
}

func (pm *PositionManager) On(event string, handler func(PositionEvent)) {
	pm.EventHandlers[event] = append(pm.EventHandlers[event], handler)
}

func (pm *PositionManager) emit(event string, e PositionEvent) {
	for _, h := range pm.EventHandlers[event] {
		go h(e)
	}
}

func (pm *PositionManager) Start() {
	pm.ticker = time.NewTicker(pm.checkInterval)
	go func() {
		for {
			select {
			case <-pm.ticker.C:
				pm.monitorPositions()
			case <-pm.done:
				return
			}
		}
	}()
	fmt.Println("Position manager started")
}

func (pm *PositionManager) Stop() {
	if pm.ticker != nil {
		pm.ticker.Stop()
	}
	close(pm.done)
}

func (pm *PositionManager) AddPosition(platform, market, side string, size, entryPrice float64) *Position {
	pos := &Position{
		ID:            generatePositionID(),
		Platform:      platform,
		Market:        market,
		Side:          side,
		Size:          size,
		EntryPrice:    entryPrice,
		CurrentPrice:  entryPrice,
		HighWaterMark: entryPrice,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	pm.mu.Lock()
	pm.positions[pos.ID] = pos
	pm.mu.Unlock()

	pm.emit("positionAdded", PositionEvent{Type: "positionAdded", Position: *pos})
	return pos
}

func (pm *PositionManager) UpdatePrice(positionID string, currentPrice float64) *Position {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pos, ok := pm.positions[positionID]
	if !ok {
		return nil
	}
	pos.CurrentPrice = currentPrice
	pos.UpdatedAt = time.Now()
	if currentPrice > pos.HighWaterMark {
		pos.HighWaterMark = currentPrice
	}
	return pos
}

func (pm *PositionManager) ListPositions(platform string) []Position {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make([]Position, 0)
	for _, p := range pm.positions {
		if platform == "" || p.Platform == platform {
			result = append(result, *p)
		}
	}
	return result
}

func (pm *PositionManager) GetPosition(positionID string) *Position {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if p, ok := pm.positions[positionID]; ok {
		cp := *p
		return &cp
	}
	return nil
}

func (pm *PositionManager) SetStopLoss(positionID string, price, percentFromEntry, sizePercent float64) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pos, ok := pm.positions[positionID]
	if !ok {
		return fmt.Errorf("position %s not found", positionID)
	}

	stopPrice := price
	if percentFromEntry > 0 && price == 0 {
		direction := 1.0
		if pos.Side != "YES" && pos.Side != "long" {
			direction = -1
		}
		stopPrice = pos.EntryPrice * (1 - (percentFromEntry/100)*direction)
	}
	if sizePercent == 0 {
		sizePercent = 100
	}

	pos.StopLoss = &StopLoss{Price: stopPrice, SizePercent: sizePercent, SetAt: time.Now()}
	return nil
}

func (pm *PositionManager) SetTakeProfit(positionID string, price, percentFromEntry float64, levels []TakeProfitLevel) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pos, ok := pm.positions[positionID]
	if !ok {
		return fmt.Errorf("position %s not found", positionID)
	}

	if len(levels) > 0 {
		pos.TakeProfit = &TakeProfit{Type: "multi-level", Levels: levels, SetAt: time.Now()}
	} else {
		targetPrice := price
		if percentFromEntry > 0 && price == 0 {
			direction := 1.0
			if pos.Side != "YES" && pos.Side != "long" {
				direction = -1
			}
			targetPrice = pos.EntryPrice * (1 + (percentFromEntry/100)*direction)
		}
		pos.TakeProfit = &TakeProfit{Type: "single", Price: targetPrice, SizePercent: 100, SetAt: time.Now()}
	}
	return nil
}

func (pm *PositionManager) SetTrailingStop(positionID string, trailPercent, activateAt float64) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pos, ok := pm.positions[positionID]
	if !ok {
		return fmt.Errorf("position %s not found", positionID)
	}

	pos.TrailingStop = &TrailingStop{
		TrailPercent: trailPercent,
		ActivateAt:   activateAt,
		IsActive:     activateAt == 0,
		SetAt:        time.Now(),
	}
	return nil
}

func (pm *PositionManager) RemoveAllStops(positionID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pos, ok := pm.positions[positionID]
	if !ok {
		return fmt.Errorf("position %s not found", positionID)
	}
	pos.StopLoss = nil
	pos.TakeProfit = nil
	pos.TrailingStop = nil
	return nil
}

func (pm *PositionManager) DeletePosition(positionID string) {
	pm.mu.Lock()
	delete(pm.positions, positionID)
	pm.mu.Unlock()
}

func (pm *PositionManager) GetSummary() PositionSummary {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	s := PositionSummary{}
	for _, pos := range pm.positions {
		s.Count++
		s.TotalValue += pos.CurrentPrice * pos.Size
		pnl := pm.calculatePnL(pos)
		s.UnrealizedPnl += pnl.PnL
		if pos.StopLoss != nil {
			s.WithStopLoss++
		}
		if pos.TakeProfit != nil {
			s.WithTakeProfit++
		}
		if pos.TrailingStop != nil {
			s.WithTrailingStop++
		}
	}
	return s
}

func (pm *PositionManager) monitorPositions() {
	pm.mu.Lock()
	ids := make([]string, 0, len(pm.positions))
	for id := range pm.positions {
		ids = append(ids, id)
	}
	pm.mu.Unlock()

	for _, id := range ids {
		pm.mu.RLock()
		pos, ok := pm.positions[id]
		if !ok {
			pm.mu.RUnlock()
			continue
		}
		posCopy := *pos
		pm.mu.RUnlock()

		pm.checkStopLoss(&posCopy)
		pm.checkTakeProfit(&posCopy)
		pm.checkTrailingStop(&posCopy)
	}
}

func (pm *PositionManager) checkStopLoss(pos *Position) {
	if pos.StopLoss == nil {
		return
	}
	isLong := pos.Side == "YES" || pos.Side == "long"
	shouldTrigger := (isLong && pos.CurrentPrice <= pos.StopLoss.Price) ||
		(!isLong && pos.CurrentPrice >= pos.StopLoss.Price)

	if shouldTrigger {
		pnl := pm.calculatePnL(pos)
		pm.emit("stopLossTriggered", PositionEvent{
			Type:     "stopLossTriggered",
			Position: *pos,
			PnL:      &pnl,
		})
		if pos.StopLoss.SizePercent >= 100 {
			pm.mu.Lock()
			delete(pm.positions, pos.ID)
			pm.mu.Unlock()
		} else {
			pm.mu.Lock()
			if p, ok := pm.positions[pos.ID]; ok {
				p.Size = p.Size * (1 - p.StopLoss.SizePercent/100)
				p.StopLoss = nil
			}
			pm.mu.Unlock()
		}
	}
}

func (pm *PositionManager) checkTakeProfit(pos *Position) {
	if pos.TakeProfit == nil {
		return
	}
	isLong := pos.Side == "YES" || pos.Side == "long"

	if pos.TakeProfit.Type == "single" {
		shouldTrigger := (isLong && pos.CurrentPrice >= pos.TakeProfit.Price) ||
			(!isLong && pos.CurrentPrice <= pos.TakeProfit.Price)
		if shouldTrigger {
			pnl := pm.calculatePnL(pos)
			pm.emit("takeProfitTriggered", PositionEvent{Type: "takeProfitTriggered", Position: *pos, PnL: &pnl})
			pm.mu.Lock()
			delete(pm.positions, pos.ID)
			pm.mu.Unlock()
		}
	} else if pos.TakeProfit.Type == "multi-level" {
		allTriggered := true
		for i, level := range pos.TakeProfit.Levels {
			if !level.Triggered {
				shouldTrigger := (isLong && pos.CurrentPrice >= level.Price) ||
					(!isLong && pos.CurrentPrice <= level.Price)
				if shouldTrigger {
					pm.mu.Lock()
					if p, ok := pm.positions[pos.ID]; ok {
						p.TakeProfit.Levels[i].Triggered = true
						p.Size = p.Size * (1 - level.SizePercent/100)
					}
					pm.mu.Unlock()
					pnl := pm.calculatePnL(pos)
					pm.emit("takeProfitTriggered", PositionEvent{Type: "takeProfitTriggered", Position: *pos, PnL: &pnl})
				} else {
					allTriggered = false
				}
			}
		}
		if allTriggered {
			pm.mu.Lock()
			delete(pm.positions, pos.ID)
			pm.mu.Unlock()
		}
	}
}

func (pm *PositionManager) checkTrailingStop(pos *Position) {
	if pos.TrailingStop == nil {
		return
	}
	trail := pos.TrailingStop

	if !trail.IsActive && trail.ActivateAt > 0 {
		if pos.CurrentPrice >= trail.ActivateAt {
			pm.mu.Lock()
			if p, ok := pm.positions[pos.ID]; ok {
				p.TrailingStop.IsActive = true
				p.HighWaterMark = pos.CurrentPrice
			}
			pm.mu.Unlock()
		} else {
			return
		}
	}

	if !trail.IsActive {
		return
	}

	trailPrice := pos.HighWaterMark * (1 - trail.TrailPercent/100)
	if pos.CurrentPrice <= trailPrice {
		pnl := pm.calculatePnL(pos)
		pm.emit("trailingStopTriggered", PositionEvent{Type: "trailingStopTriggered", Position: *pos, PnL: &pnl})
		pm.mu.Lock()
		delete(pm.positions, pos.ID)
		pm.mu.Unlock()
	}
}

func (pm *PositionManager) calculatePnL(pos *Position) PnL {
	priceDiff := pos.CurrentPrice - pos.EntryPrice
	direction := 1.0
	if pos.Side != "YES" && pos.Side != "long" {
		direction = -1
	}
	pnl := direction * priceDiff * pos.Size
	pnlPercent := (pnl / (pos.EntryPrice * pos.Size)) * 100
	return PnL{PnL: pnl, PnLPercent: pnlPercent}
}

func generatePositionID() string {
	return fmt.Sprintf("pos_%d_%s", time.Now().UnixMilli(), randStr(8))
}

func randStr(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
