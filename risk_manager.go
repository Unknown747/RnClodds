package main

import (
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"
)

type RiskManager struct {
	cfg         *Config
	mem         *MemoryManager
	userID      string
	mu          sync.RWMutex
	killSwitch  bool
	dailyTrades int
	dailyPnL    float64
	resetDate   string
}

type RiskStatus struct {
	KillSwitch    bool    `json:"killSwitch"`
	DailyTrades   int     `json:"dailyTrades"`
	DailyTradeMax int     `json:"dailyTradeMax"`
	DailyPnL      float64 `json:"dailyPnL"`
	MaxDailyLoss  float64 `json:"maxDailyLoss"`
	Blocked       bool    `json:"blocked"`
	BlockReason   string  `json:"blockReason,omitempty"`
}

type BacktestResult struct {
	TotalTrades   int        `json:"totalTrades"`
	WinRate       float64    `json:"winRate"`
	TotalPnL      float64    `json:"totalPnL"`
	MaxDrawdown   float64    `json:"maxDrawdown"`
	SharpeRatio   float64    `json:"sharpeRatio"`
	EquityCurve   []float64  `json:"equityCurve"`
	EquityDates   []string   `json:"equityDates"`
	BestDay       float64    `json:"bestDay"`
	WorstDay      float64    `json:"worstDay"`
	AvgDailyPnL   float64    `json:"avgDailyPnL"`
	ComputedAt    time.Time  `json:"computedAt"`
}

func NewRiskManager(cfg *Config, mem *MemoryManager, userID string) *RiskManager {
	return &RiskManager{
		cfg:       cfg,
		mem:       mem,
		userID:    userID,
		resetDate: time.Now().Format("2006-01-02"),
	}
}

// Check resets daily counters if it's a new day.
func (rm *RiskManager) resetIfNewDay() {
	today := time.Now().Format("2006-01-02")
	if rm.resetDate != today {
		rm.resetDate = today
		rm.dailyTrades = 0
		rm.dailyPnL = 0
		rm.killSwitch = false
		slog.Info("Risk manager: daily counters reset", "date", today)
	}
}

// CheckOrder validates whether a new order is allowed.
func (rm *RiskManager) CheckOrder(size float64, aiConfidence float64) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.resetIfNewDay()

	if rm.killSwitch {
		return fmt.Errorf("kill switch active — trading halted for today")
	}

	maxDailyTrades := rm.cfg.Trading.MaxDailyTrades
	if maxDailyTrades > 0 && rm.dailyTrades >= maxDailyTrades {
		return fmt.Errorf("daily trade limit (%d) reached", maxDailyTrades)
	}

	maxLoss := float64(rm.cfg.Trading.MaxDailyLoss)
	if rm.dailyPnL <= -maxLoss {
		rm.killSwitch = true
		slog.Warn("Kill switch triggered", "dailyPnL", rm.dailyPnL, "maxDailyLoss", maxLoss)
		return fmt.Errorf("kill switch: daily loss limit $%.0f reached", maxLoss)
	}

	if size > float64(rm.cfg.Trading.MaxPositionSize) {
		return fmt.Errorf("order size $%.0f exceeds max $%d", size, rm.cfg.Trading.MaxPositionSize)
	}

	minConf := rm.cfg.Risk.MinConfidence
	if aiConfidence > 0 && aiConfidence < minConf {
		return fmt.Errorf("AI confidence %.2f below minimum %.2f — order rejected", aiConfidence, minConf)
	}

	return nil
}

// RecordTrade updates daily counters after a successful trade.
func (rm *RiskManager) RecordTrade(pnl float64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.resetIfNewDay()
	rm.dailyTrades++
	rm.dailyPnL += pnl

	maxLoss := float64(rm.cfg.Trading.MaxDailyLoss)
	if rm.dailyPnL <= -maxLoss && !rm.killSwitch {
		rm.killSwitch = true
		slog.Warn("Kill switch triggered by P&L", "dailyPnL", rm.dailyPnL)
	}
}

// Status returns current risk snapshot.
func (rm *RiskManager) Status() RiskStatus {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	blocked := rm.killSwitch
	reason := ""
	if rm.killSwitch {
		reason = "kill switch active"
	} else if rm.cfg.Trading.MaxDailyTrades > 0 && rm.dailyTrades >= rm.cfg.Trading.MaxDailyTrades {
		blocked = true
		reason = fmt.Sprintf("daily trade limit %d reached", rm.cfg.Trading.MaxDailyTrades)
	}

	return RiskStatus{
		KillSwitch:    rm.killSwitch,
		DailyTrades:   rm.dailyTrades,
		DailyTradeMax: rm.cfg.Trading.MaxDailyTrades,
		DailyPnL:      math.Round(rm.dailyPnL*100) / 100,
		MaxDailyLoss:  float64(rm.cfg.Trading.MaxDailyLoss),
		Blocked:       blocked,
		BlockReason:   reason,
	}
}

// ToggleKillSwitch manually enables/disables kill switch.
func (rm *RiskManager) ToggleKillSwitch(enable bool) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.killSwitch = enable
	slog.Info("Kill switch toggled", "enabled", enable)
}

// Backtest replays journal entries from SQLite and computes performance metrics.
func (rm *RiskManager) Backtest() (*BacktestResult, error) {
	journals, err := rm.mem.GetJournals(rm.userID, 365)
	if err != nil {
		return nil, err
	}

	if len(journals) == 0 {
		return &BacktestResult{ComputedAt: time.Now()}, nil
	}

	result := &BacktestResult{ComputedAt: time.Now()}
	equity := 0.0
	peak := 0.0
	maxDD := 0.0
	wins := 0
	var pnls []float64

	for _, j := range journals {
		result.TotalTrades += j.Trades
		equity += j.PnL
		pnls = append(pnls, j.PnL)
		result.EquityCurve = append(result.EquityCurve, math.Round(equity*100)/100)
		result.EquityDates = append(result.EquityDates, j.Date)

		if j.PnL > 0 {
			wins++
		}
		if j.PnL > result.BestDay {
			result.BestDay = j.PnL
		}
		if j.PnL < result.WorstDay {
			result.WorstDay = j.PnL
		}
		if equity > peak {
			peak = equity
		}
		dd := peak - equity
		if dd > maxDD {
			maxDD = dd
		}
	}

	n := float64(len(journals))
	result.TotalPnL = math.Round(equity*100) / 100
	result.MaxDrawdown = math.Round(maxDD*100) / 100
	result.BestDay = math.Round(result.BestDay*100) / 100
	result.WorstDay = math.Round(result.WorstDay*100) / 100
	result.AvgDailyPnL = math.Round(equity/n*100) / 100

	if len(journals) > 0 {
		result.WinRate = math.Round(float64(wins)/n*10000) / 100
	}

	// Sharpe ratio (simplified, risk-free = 0)
	if len(pnls) > 1 {
		mean := equity / n
		variance := 0.0
		for _, p := range pnls {
			d := p - mean
			variance += d * d
		}
		variance /= n
		stdDev := math.Sqrt(variance)
		if stdDev > 0 {
			result.SharpeRatio = math.Round(mean/stdDev*100) / 100
		}
	}

	slog.Info("Backtest complete",
		"totalTrades", result.TotalTrades,
		"totalPnL", result.TotalPnL,
		"winRate", result.WinRate,
		"maxDrawdown", result.MaxDrawdown,
		"sharpe", result.SharpeRatio,
	)
	return result, nil
}
