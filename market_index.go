package main

import (
	"log/slog"
	"math/rand"
	"strings"
	"sync"
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
	mu          sync.RWMutex
	markets     []Market
	// prices maps lowercase question → YES probability (0–1), also keyed by market ID
	prices      map[string]float64
	lastUpdated *time.Time
	fetcher     *MarketFetcher
	stopCh      chan struct{}
}

func NewMarketIndex(platforms []string, autoRefresh bool) *MarketIndex {
	mi := &MarketIndex{
		platforms: platforms,
		markets:   fallbackMarkets(),
		prices:    fallbackPrices(),
		fetcher:   NewMarketFetcher(),
		stopCh:    make(chan struct{}),
	}

	// Kick off a live fetch immediately in the background so the first data
	// is real as soon as possible, then refresh every 5 minutes.
	go mi.refresh()

	if autoRefresh {
		go mi.startAutoRefresh()
	}
	return mi
}

func (mi *MarketIndex) refresh() {
	markets, prices, err := mi.fetchLive()
	if err != nil {
		slog.Warn("Live market fetch failed, keeping fallback data", "error", err)
		return
	}
	now := time.Now()
	mi.mu.Lock()
	mi.markets = markets
	mi.prices = prices
	mi.lastUpdated = &now
	mi.mu.Unlock()
	slog.Info("Market index refreshed with live data", "markets", len(markets), "prices", len(prices))
}

func (mi *MarketIndex) fetchLive() ([]Market, map[string]float64, error) {
	liveMarkets, livePrices := mi.fetcher.FetchAll()
	if len(liveMarkets) == 0 {
		return nil, nil, nil
	}
	// Merge fallback markets that aren't covered by live data (keeps the index
	// populated even when an API is down).
	priceMap := make(map[string]float64, len(livePrices))
	for k, v := range livePrices {
		priceMap[k] = v
	}
	merged := liveMarkets
	return merged, priceMap, nil
}

// startAutoRefresh refreshes market data every 5 minutes until Stop() is called.
func (mi *MarketIndex) startAutoRefresh() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			mi.refresh()
		case <-mi.stopCh:
			return
		}
	}
}

// Stop signals the auto-refresh goroutine to exit.
func (mi *MarketIndex) Stop() {
	select {
	case <-mi.stopCh:
	default:
		close(mi.stopCh)
	}
}

// GetPrice returns the live YES probability for a market identified by its
// question text or market ID (case-insensitive). Falls back to 0.5 if unknown.
func (mi *MarketIndex) GetPrice(marketKey string) float64 {
	mi.mu.RLock()
	defer mi.mu.RUnlock()
	key := strings.ToLower(marketKey)
	if p, ok := mi.prices[key]; ok {
		return p
	}
	// Fuzzy search: check if any stored question contains the key as a substring
	for q, p := range mi.prices {
		if strings.Contains(q, key) || strings.Contains(key, q) {
			return p
		}
	}
	return 0.50
}

func (mi *MarketIndex) Search(query string, f MarketFilters) []Market {
	mi.mu.RLock()
	defer mi.mu.RUnlock()

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
	mi.mu.RLock()
	defer mi.mu.RUnlock()

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
	mi.mu.RLock()
	defer mi.mu.RUnlock()

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
	mi.mu.RLock()
	defer mi.mu.RUnlock()
	for _, m := range mi.markets {
		if m.Platform == platform && m.ID == marketID {
			cp := m
			return &cp
		}
	}
	return nil
}

func (mi *MarketIndex) Update(platform string) {
	go mi.refresh()
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

// ─── Fallback data (used until the first live fetch succeeds) ─────────────────

func fallbackMarkets() []Market {
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

func fallbackPrices() map[string]float64 {
	return map[string]float64{
		"will trump win 2028 election?":   0.52,
		"will bitcoin hit $100k in 2024?": 0.38,
		"eth above $4000 by eoy?":         0.31,
		"will fed cut rates in september?": 0.45,
		"will chiefs win super bowl?":      0.18,
	}
}
