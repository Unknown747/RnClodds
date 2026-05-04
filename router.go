package main

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

type FeeStructure struct {
	Maker float64
	Taker float64
	Note  string
}

type LiquidityData struct {
	Depth1Pct float64
	Depth2Pct float64
	Spread    float64
}

type PlatformResult struct {
	Name            string
	Price           float64
	Liquidity       float64
	Slippage        float64
	Fees            float64
	NetCost         float64
	FillProbability float64
	BalancedScore   float64
}

type Route struct {
	Platform         string  `json:"platform"`
	Mode             string  `json:"mode"`
	ExpectedPrice    float64 `json:"expectedPrice"`
	ExpectedSlippage float64 `json:"expectedSlippage"`
	Fees             float64 `json:"fees"`
	NetCost          float64 `json:"netCost"`
	FillProbability  float64 `json:"fillProbability"`
	Score            float64 `json:"score"`
}

type OrderExecution struct {
	OrderID        string    `json:"orderId"`
	Platform       string    `json:"platform"`
	FillPrice      float64   `json:"fillPrice"`
	ActualSlippage float64   `json:"actualSlippage"`
	Fees           float64   `json:"fees"`
	ExecutedAt     time.Time `json:"executedAt"`
}

type SplitLeg struct {
	Platform          string  `json:"platform"`
	Size              float64 `json:"size"`
	Price             float64 `json:"price"`
	EstimatedSlippage float64 `json:"estimatedSlippage"`
}

type SplitOrder struct {
	Legs          []SplitLeg `json:"legs"`
	TotalSlippage float64    `json:"totalSlippage"`
	AvgPrice      float64    `json:"avgPrice"`
}

type SmartRouter struct {
	platforms     []string
	defaultMode   string
	fees          map[string]FeeStructure
	liquidityData map[string]LiquidityData
	// marketIndex provides live prices from real prediction market APIs.
	marketIndex *MarketIndex
}

func NewSmartRouter(platforms []string, defaultMode string, mi *MarketIndex) *SmartRouter {
	if len(platforms) == 0 {
		platforms = []string{"polymarket", "kalshi", "manifold"}
	}
	if defaultMode == "" {
		defaultMode = "balanced"
	}
	return &SmartRouter{
		platforms:   platforms,
		defaultMode: defaultMode,
		marketIndex: mi,
		fees: map[string]FeeStructure{
			"polymarket": {Maker: 0, Taker: 0, Note: "Zero fees on most markets"},
			"kalshi":     {Maker: 0.17, Taker: 1.2, Note: "Formula-based, capped ~2%"},
			"manifold":   {Maker: 0, Taker: 0, Note: "Play money"},
		},
		liquidityData: map[string]LiquidityData{
			"polymarket": {Depth1Pct: 500000, Depth2Pct: 1000000, Spread: 0.02},
			"kalshi":     {Depth1Pct: 250000, Depth2Pct: 600000, Spread: 0.05},
			"manifold":   {Depth1Pct: 10000, Depth2Pct: 50000, Spread: 0.1},
		},
	}
}

func (r *SmartRouter) FindBestRoute(market, side string, size float64, mode string) (*Route, error) {
	if mode == "" {
		mode = r.defaultMode
	}
	comparison := r.compare(market, side, size)

	switch mode {
	case "best-price":
		best := comparison[0]
		for _, p := range comparison[1:] {
			if p.Price < best.Price {
				best = p
			}
		}
		return r.createRoute(best, mode), nil

	case "best-liquidity":
		best := comparison[0]
		for _, p := range comparison[1:] {
			if p.Liquidity > best.Liquidity {
				best = p
			}
		}
		return r.createRoute(best, mode), nil

	case "lowest-fee":
		best := comparison[0]
		for _, p := range comparison[1:] {
			if p.Fees < best.Fees {
				best = p
			}
		}
		return r.createRoute(best, mode), nil

	default:
		w := struct{ Price, Liquidity, Fees float64 }{0.4, 0.3, 0.3}

		bestPrice := comparison[0].Price
		bestLiquidity := comparison[0].Liquidity
		lowestFees := comparison[0].Fees
		for _, p := range comparison[1:] {
			if p.Price < bestPrice {
				bestPrice = p.Price
			}
			if p.Liquidity > bestLiquidity {
				bestLiquidity = p.Liquidity
			}
			if p.Fees < lowestFees {
				lowestFees = p.Fees
			}
		}

		for i := range comparison {
			priceScore := bestPrice / comparison[i].Price
			liquidityScore := comparison[i].Liquidity / bestLiquidity
			feeScore := lowestFees / (comparison[i].Fees + 0.01)
			comparison[i].BalancedScore = priceScore*w.Price + liquidityScore*w.Liquidity + feeScore*w.Fees
		}

		best := comparison[0]
		for _, p := range comparison[1:] {
			if p.BalancedScore > best.BalancedScore {
				best = p
			}
		}
		return r.createRoute(best, mode), nil
	}
}

func (r *SmartRouter) compare(market, side string, size float64) []PlatformResult {
	// Pull the live base price from the MarketIndex (real API data).
	basePrice := r.getLivePrice(market)

	results := make([]PlatformResult, 0, len(r.platforms))
	for _, platform := range r.platforms {
		price := r.getPlatformPrice(platform, basePrice, side)

		feeStructure := r.fees[platform]
		fees := (size * feeStructure.Taker) / 100

		liq := r.liquidityData[platform]
		var slippage float64
		if size > liq.Depth1Pct {
			slippage = 2
		} else {
			slippage = (size / liq.Depth1Pct) * 100
		}

		results = append(results, PlatformResult{
			Name:            platform,
			Price:           price,
			Liquidity:       liq.Depth1Pct,
			Slippage:        slippage,
			Fees:            fees,
			NetCost:         size*price + fees,
			FillProbability: math.Max(0, 100-slippage),
		})
	}
	return results
}

func (r *SmartRouter) Execute(route *Route) *OrderExecution {
	spread := (rand.Float64() - 0.5) * route.ExpectedSlippage / 100
	actualPrice := route.ExpectedPrice * (1 + spread)
	actualSlippage := math.Abs((actualPrice-route.ExpectedPrice)/route.ExpectedPrice) * 100

	return &OrderExecution{
		OrderID:        fmt.Sprintf("ord_%d_%s", time.Now().UnixMilli(), randStr(8)),
		Platform:       route.Platform,
		FillPrice:      actualPrice,
		ActualSlippage: actualSlippage,
		Fees:           route.Fees,
		ExecutedAt:     time.Now(),
	}
}

func (r *SmartRouter) SplitOrder(market, side string, size, maxSlippage float64) *SplitOrder {
	if maxSlippage == 0 {
		maxSlippage = 0.02
	}
	comparison := r.compare(market, side, size)

	viable := []PlatformResult{}
	for _, p := range comparison {
		if p.Slippage <= maxSlippage {
			viable = append(viable, p)
		}
	}

	if len(viable) == 0 {
		return &SplitOrder{Legs: []SplitLeg{}, TotalSlippage: maxSlippage + 1}
	}

	totalLiquidity := 0.0
	for _, p := range viable {
		totalLiquidity += p.Liquidity
	}

	legs := []SplitLeg{}
	remainingSize := size

	for i := 0; i < len(viable)-1; i++ {
		p := viable[i]
		allocation := math.Min(p.Liquidity*0.8, remainingSize*(p.Liquidity/totalLiquidity))
		legs = append(legs, SplitLeg{Platform: p.Name, Size: allocation, Price: p.Price, EstimatedSlippage: p.Slippage})
		remainingSize -= allocation
	}

	last := viable[len(viable)-1]
	legs = append(legs, SplitLeg{Platform: last.Name, Size: remainingSize, Price: last.Price, EstimatedSlippage: last.Slippage})

	avgPrice := 0.0
	totalSlippage := 0.0
	for _, leg := range legs {
		avgPrice += leg.Price
		totalSlippage += leg.EstimatedSlippage
	}
	avgPrice /= float64(len(legs))
	totalSlippage /= float64(len(legs))

	return &SplitOrder{Legs: legs, TotalSlippage: totalSlippage, AvgPrice: avgPrice}
}

// getLivePrice fetches the current YES probability for a market from the live
// MarketIndex (which pulls from real Manifold / Polymarket APIs).
func (r *SmartRouter) getLivePrice(market string) float64 {
	if r.marketIndex != nil {
		return r.marketIndex.GetPrice(market)
	}
	return 0.50
}

// getPlatformPrice applies a small per-platform spread on top of the live base price.
// Real arbitrage differences between platforms are typically < 1–2%.
func (r *SmartRouter) getPlatformPrice(platform string, basePrice float64, side string) float64 {
	spreads := map[string]float64{
		"polymarket": 0.000, // tightest spread, highest liquidity
		"kalshi":     0.010, // ~1% wider
		"manifold":   0.020, // play money, wider spreads
	}
	spread, ok := spreads[platform]
	if !ok {
		spread = 0.005
	}
	// For YES bets, ask price is base + half spread; for NO it's (1-base) + half spread
	if side == "NO" {
		basePrice = 1.0 - basePrice
	}
	return math.Min(0.99, math.Max(0.01, basePrice+spread))
}

func (r *SmartRouter) createRoute(p PlatformResult, mode string) *Route {
	return &Route{
		Platform:         p.Name,
		Mode:             mode,
		ExpectedPrice:    p.Price,
		ExpectedSlippage: p.Slippage,
		Fees:             p.Fees,
		NetCost:          p.NetCost,
		FillProbability:  p.FillProbability,
		Score:            p.BalancedScore,
	}
}
