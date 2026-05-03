package main

import (
	"math/rand"
	"strings"
	"time"
)

type Market struct {
	ID           string   `json:"id"`
	Platform     string   `json:"platform"`
	Question     string   `json:"question"`
	Category     string   `json:"category"`
	Volume       float64  `json:"volume"`
	Liquidity    float64  `json:"liquidity"`
	StartDate    string   `json:"startDate"`
	EndDate      string   `json:"endDate"`
	Outcomes     []string `json:"outcomes"`
	Active       bool     `json:"active"`
	VolumeChange *int     `json:"volumeChange,omitempty"`
}

type MarketFilters struct {
	Platforms  []string
	Category   string
	MinVolume  float64
	ActiveOnly bool
	EndsBefore string
	SortBy     string
	Limit      int
}

type MarketIndex struct {
	platforms   []string
	markets     []Market
	lastUpdated *time.Time
	autoRefresh bool
}

func NewMarketIndex(platforms []string, autoRefresh bool) *MarketIndex {
	mi := &MarketIndex{
		platforms:   platforms,
		autoRefresh: autoRefresh,
		markets:     initMockMarkets(),
	}
	if autoRefresh {
		go mi.startAutoRefresh()
	}
	return mi
}

func initMockMarkets() []Market {
	return []Market{
		{ID: "poly-001", Platform: "polymarket", Question: "Will Trump win 2028 election?", Category: "politics", Volume: 12500000, Liquidity: 2500000, StartDate: "2024-01-01", EndDate: "2028-11-05", Outcomes: []string{"YES", "NO"}, Active: true},
		{ID: "kal-001", Platform: "kalshi", Question: "Will Trump win 2028 election?", Category: "politics", Volume: 5200000, Liquidity: 1100000, StartDate: "2024-01-01", EndDate: "2028-11-05", Outcomes: []string{"YES", "NO"}, Active: true},
		{ID: "poly-002", Platform: "polymarket", Question: "Will Bitcoin hit $100K in 2024?", Category: "crypto", Volume: 8750000, Liquidity: 1800000, StartDate: "2024-01-01", EndDate: "2024-12-31", Outcomes: []string{"YES", "NO"}, Active: true},
		{ID: "man-001", Platform: "manifold", Question: "ETH above $4000 by EOY?", Category: "crypto", Volume: 125000, Liquidity: 50000, StartDate: "2024-01-01", EndDate: "2024-12-31", Outcomes: []string{"YES", "NO"}, Active: true},
		{ID: "poly-003", Platform: "polymarket", Question: "Will Fed cut rates in September?", Category: "finance", Volume: 3450000, Liquidity: 890000, StartDate: "2024-06-01", EndDate: "2024-09-30", Outcomes: []string{"YES", "NO"}, Active: true},
		{ID: "kal-002", Platform: "kalshi", Question: "Will Fed cut rates in September?", Category: "finance", Volume: 2100000, Liquidity: 450000, StartDate: "2024-06-01", EndDate: "2024-09-30", Outcomes: []string{"YES", "NO"}, Active: true},
		{ID: "poly-004", Platform: "polymarket", Question: "Will Chiefs win Super Bowl?", Category: "sports", Volume: 4300000, Liquidity: 920000, StartDate: "2024-08-01", EndDate: "2025-02-09", Outcomes: []string{"YES", "NO"}, Active: true},
	}
}

func (mi *MarketIndex) startAutoRefresh() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		mi.Update("")
	}
}

func (mi *MarketIndex) Search(query string, f MarketFilters) []Market {
	results := make([]Market, 0, len(mi.markets))

	for _, m := range mi.markets {
		if query != "" {
			q := strings.ToLower(query)
			if !strings.Contains(strings.ToLower(m.Question), q) && !strings.Contains(strings.ToLower(m.Category), q) {
				continue
			}
		}
		if len(f.Platforms) > 0 && !containsStr(f.Platforms, m.Platform) {
			continue
		}
		if f.Category != "" && m.Category != f.Category {
			continue
		}
		if f.MinVolume > 0 && m.Volume < f.MinVolume {
			continue
		}
		if f.ActiveOnly && !m.Active {
			continue
		}
		if f.EndsBefore != "" && m.EndDate > f.EndsBefore {
			continue
		}
		results = append(results, m)
	}

	switch f.SortBy {
	case "volume":
		sortMarketsByVolume(results)
	case "endDate":
		sortMarketsByEndDate(results)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

func (mi *MarketIndex) GetTrendingMarkets(limit int) []Market {
	sorted := make([]Market, len(mi.markets))
	copy(sorted, mi.markets)
	sortMarketsByVolume(sorted)

	if limit <= 0 {
		limit = 10
	}
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}

	result := make([]Market, len(sorted))
	for i, m := range sorted {
		vc := rand.Intn(70) - 10
		m.VolumeChange = &vc
		result[i] = m
	}
	return result
}

func (mi *MarketIndex) GetClosingSoon(within string, minVolume float64, limit int) []Market {
	now := time.Now()
	var hours float64
	switch within {
	case "24h":
		hours = 24
	case "48h":
		hours = 48
	default:
		hours = 168
	}
	cutoff := now.Add(time.Duration(hours) * time.Hour)

	results := []Market{}
	for _, m := range mi.markets {
		endDate, err := time.Parse("2006-01-02", m.EndDate)
		if err != nil {
			continue
		}
		if endDate.Before(cutoff) && endDate.After(now) && m.Active {
			if minVolume > 0 && m.Volume < minVolume {
				continue
			}
			results = append(results, m)
		}
	}

	sortMarketsByEndDate(results)
	if limit <= 0 {
		limit = 20
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

func (mi *MarketIndex) GetMarket(platform, marketID string) *Market {
	for _, m := range mi.markets {
		if m.Platform == platform && m.ID == marketID {
			cp := m
			return &cp
		}
	}
	return nil
}

func (mi *MarketIndex) Update(platform string) {
	now := time.Now()
	mi.lastUpdated = &now
}

func sortMarketsByVolume(markets []Market) {
	for i := 1; i < len(markets); i++ {
		for j := i; j > 0 && markets[j].Volume > markets[j-1].Volume; j-- {
			markets[j], markets[j-1] = markets[j-1], markets[j]
		}
	}
}

func sortMarketsByEndDate(markets []Market) {
	for i := 1; i < len(markets); i++ {
		for j := i; j > 0 && markets[j].EndDate < markets[j-1].EndDate; j-- {
			markets[j], markets[j-1] = markets[j-1], markets[j]
		}
	}
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
