package main

import (
	"encoding/json"
	"log/slog"
	"math"
)

type PlatformStat struct {
	Trades int     `json:"trades"`
	PnL    float64 `json:"pnl"`
}

type JournalEntry struct {
	Date   string  `json:"date"`
	Trades int     `json:"trades"`
	PnL    float64 `json:"pnl"`
	Notes  string  `json:"notes"`
}

type AnalyticsData struct {
	TotalTrades    int                      `json:"totalTrades"`
	TotalClosed    int                      `json:"totalClosed"`
	TotalPnL       float64                  `json:"totalPnL"`
	WinRate        float64                  `json:"winRate"`
	AvgPnL         float64                  `json:"avgPnL"`
	BestTrade      float64                  `json:"bestTrade"`
	WorstTrade     float64                  `json:"worstTrade"`
	ByPlatform     map[string]*PlatformStat `json:"byPlatform"`
	RecentJournals []JournalEntry           `json:"recentJournals"`
	DailyTrades    int                      `json:"dailyTrades"`
	DailyPnL       float64                  `json:"dailyPnL"`
}

// GetAnalytics queries SQLite for historical trading analytics.
func (mm *MemoryManager) GetAnalytics(userID string) (*AnalyticsData, error) {
	data := &AnalyticsData{
		ByPlatform:     make(map[string]*PlatformStat),
		RecentJournals: []JournalEntry{},
		BestTrade:      math.Inf(-1),
		WorstTrade:     math.Inf(1),
	}

	// 1. Total trades executed
	if err := mm.db.QueryRow(
		`SELECT COUNT(*) FROM memories WHERE userId=? AND type='note' AND key LIKE 'trade_%'`,
		userID,
	).Scan(&data.TotalTrades); err != nil {
		return nil, err
	}

	// 2. Total positions closed
	if err := mm.db.QueryRow(
		`SELECT COUNT(*) FROM memories WHERE userId=? AND type='note' AND key LIKE 'close_%'`,
		userID,
	).Scan(&data.TotalClosed); err != nil {
		return nil, err
	}

	// 3. Per-platform breakdown from trade metadata
	tradeRows, err := mm.db.Query(
		`SELECT metadata FROM memories WHERE userId=? AND type='note' AND key LIKE 'trade_%'`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer tradeRows.Close()

	for tradeRows.Next() {
		var rawMeta string
		if err := tradeRows.Scan(&rawMeta); err != nil {
			continue
		}
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(rawMeta), &meta); err != nil {
			continue
		}
		platform, _ := meta["platform"].(string)
		if platform == "" {
			platform = "unknown"
		}
		if _, ok := data.ByPlatform[platform]; !ok {
			data.ByPlatform[platform] = &PlatformStat{}
		}
		data.ByPlatform[platform].Trades++
	}
	if err := tradeRows.Err(); err != nil {
		return nil, err
	}

	// 4. Journal entries — P&L stats and win rate
	journalRows, err := mm.db.Query(
		`SELECT key, content, COALESCE(metadata,'{}') FROM memories
		 WHERE userId=? AND type='note' AND key LIKE 'journal_%'
		 ORDER BY key DESC LIMIT 30`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer journalRows.Close()

	wins := 0
	journalCount := 0

	for journalRows.Next() {
		var key, content, rawMeta string
		if err := journalRows.Scan(&key, &content, &rawMeta); err != nil {
			continue
		}
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(rawMeta), &meta); err != nil {
			continue
		}

		pnl, _ := meta["pnl"].(float64)
		trades := 0
		if t, ok := meta["trades"].(float64); ok {
			trades = int(t)
		}

		// Strip "journal_" prefix (8 chars) to get date string
		date := key
		if len(key) > 8 {
			date = key[8:]
		}

		data.RecentJournals = append(data.RecentJournals, JournalEntry{
			Date:   date,
			Trades: trades,
			PnL:    pnl,
			Notes:  content,
		})

		data.TotalPnL += pnl
		journalCount++
		if pnl > 0 {
			wins++
		}
		if pnl > data.BestTrade {
			data.BestTrade = pnl
		}
		if pnl < data.WorstTrade {
			data.WorstTrade = pnl
		}
	}
	if err := journalRows.Err(); err != nil {
		return nil, err
	}

	// Compute derived stats
	if journalCount > 0 {
		data.WinRate = math.Round(float64(wins)/float64(journalCount)*10000) / 100
		data.AvgPnL = math.Round(data.TotalPnL/float64(journalCount)*100) / 100
	}
	if math.IsInf(data.BestTrade, -1) {
		data.BestTrade = 0
	}
	if math.IsInf(data.WorstTrade, 1) {
		data.WorstTrade = 0
	}
	data.TotalPnL = math.Round(data.TotalPnL*100) / 100

	slog.Info("Analytics computed",
		"userId", userID,
		"totalTrades", data.TotalTrades,
		"totalClosed", data.TotalClosed,
		"totalPnL", data.TotalPnL,
		"winRate", data.WinRate,
	)

	return data, nil
}
