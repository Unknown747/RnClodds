package main

import (
        "encoding/json"
        "fmt"
        "log/slog"
        "net/http"
        "strconv"
        "strings"
        "time"
)

const (
        manifoldSearchURL = "https://api.manifold.markets/v0/search-markets"
        kalshiBaseURL     = "https://api.elections.kalshi.com/trade-api/v2"
        fetchTimeout      = 15 * time.Second
)

// MarketFetcher pulls live market data from Manifold and Kalshi.
type MarketFetcher struct {
        client *http.Client
}

func NewMarketFetcher() *MarketFetcher {
        return &MarketFetcher{
                client: &http.Client{Timeout: fetchTimeout},
        }
}

// FetchAll retrieves live markets from all supported platforms.
// Returns a merged markets list + a price map keyed by lowercase question and market ID.
func (f *MarketFetcher) FetchAll() ([]Market, map[string]float64) {
        prices := make(map[string]float64)
        var markets []Market

        manifoldMarkets, manifoldPrices, err := f.fetchManifold()
        if err != nil {
                slog.Warn("Manifold fetch failed", "error", err)
        } else {
                markets = append(markets, manifoldMarkets...)
                for k, v := range manifoldPrices {
                        prices[k] = v
                }
                slog.Info("Manifold markets fetched", "count", len(manifoldMarkets))
        }

        kalshiMarkets, kalshiPrices, err := f.fetchKalshi()
        if err != nil {
                slog.Warn("Kalshi fetch failed", "error", err)
        } else {
                markets = append(markets, kalshiMarkets...)
                for k, v := range kalshiPrices {
                        prices[k] = v
                }
                slog.Info("Kalshi markets fetched", "count", len(kalshiMarkets))
        }

        return markets, prices
}

// ─── Manifold ────────────────────────────────────────────────────────────────

type manifoldMarket struct {
        ID          string  `json:"id"`
        Question    string  `json:"question"`
        Probability float64 `json:"probability"`
        Volume      float64 `json:"volume"`
        CloseTime   *int64  `json:"closeTime"` // epoch ms
        IsResolved  bool    `json:"isResolved"`
        OutcomeType string  `json:"outcomeType"`
}

func (f *MarketFetcher) fetchManifold() ([]Market, map[string]float64, error) {
        // search-markets supports contractType, filter, and sort parameters
        url := manifoldSearchURL + "?term=&contractType=BINARY&filter=open&limit=100&sort=liquidity"
        req, _ := http.NewRequest(http.MethodGet, url, nil)
        req.Header.Set("Accept", "application/json")

        resp, err := f.client.Do(req)
        if err != nil {
                return nil, nil, fmt.Errorf("manifold request: %w", err)
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
                return nil, nil, fmt.Errorf("manifold HTTP %d", resp.StatusCode)
        }

        var raw []manifoldMarket
        if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
                return nil, nil, fmt.Errorf("manifold decode: %w", err)
        }

        markets := make([]Market, 0, len(raw))
        prices := make(map[string]float64, len(raw))

        for _, m := range raw {
                if m.IsResolved {
                        continue
                }
                endDate := ""
                if m.CloseTime != nil {
                        endDate = time.UnixMilli(*m.CloseTime).Format("2006-01-02")
                }

                mkt := Market{
                        ID:           "man-" + m.ID,
                        Platform:     "manifold",
                        Question:     m.Question,
                        Category:     classifyQuestion(m.Question),
                        Volume:       m.Volume,
                        Liquidity:    m.Volume * 0.1,
                        StartDate:    "2024-01-01",
                        EndDate:      endDate,
                        Outcomes:     []string{"YES", "NO"},
                        Active:       true,
                        CurrentPrice: m.Probability,
                }
                markets = append(markets, mkt)

                key := strings.ToLower(m.Question)
                prices[key] = m.Probability
                prices["man-"+m.ID] = m.Probability
        }

        return markets, prices, nil
}

// ─── Kalshi ───────────────────────────────────────────────────────────────────

type kalshiMarket struct {
        Ticker         string `json:"ticker"`
        Title          string `json:"title"`
        Subtitle       string `json:"subtitle"`
        Status         string `json:"status"`
        MarketType     string `json:"market_type"`
        YesAskDollars  string `json:"yes_ask_dollars"`
        YesBidDollars  string `json:"yes_bid_dollars"`
        LastPriceDollars string `json:"last_price_dollars"`
        VolumeFP       string `json:"volume_fp"`
        LiquidityDollars string `json:"liquidity_dollars"`
        CloseTime      string `json:"close_time"` // RFC3339
}

type kalshiResponse struct {
        Markets []kalshiMarket `json:"markets"`
        Cursor  string         `json:"cursor"`
}

func (f *MarketFetcher) fetchKalshi() ([]Market, map[string]float64, error) {
        url := kalshiBaseURL + "/markets?limit=200&status=open"
        req, _ := http.NewRequest(http.MethodGet, url, nil)
        req.Header.Set("Accept", "application/json")

        resp, err := f.client.Do(req)
        if err != nil {
                return nil, nil, fmt.Errorf("kalshi request: %w", err)
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
                return nil, nil, fmt.Errorf("kalshi HTTP %d", resp.StatusCode)
        }

        var raw kalshiResponse
        if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
                return nil, nil, fmt.Errorf("kalshi decode: %w", err)
        }

        markets := make([]Market, 0, len(raw.Markets))
        prices := make(map[string]float64, len(raw.Markets))

        for _, m := range raw.Markets {
                if m.Status != "active" || m.MarketType != "binary" || m.Title == "" {
                        continue
                }

                // Determine current probability from bid/ask midpoint or last price
                yesBid, _ := strconv.ParseFloat(m.YesBidDollars, 64)
                yesAsk, _ := strconv.ParseFloat(m.YesAskDollars, 64)
                lastPrice, _ := strconv.ParseFloat(m.LastPriceDollars, 64)
                var yesProb float64
                switch {
                case yesBid > 0 && yesAsk > 0:
                        yesProb = (yesBid + yesAsk) / 2
                case yesAsk > 0:
                        yesProb = yesAsk
                case yesBid > 0:
                        yesProb = yesBid
                case lastPrice > 0:
                        yesProb = lastPrice
                default:
                        // No price data — skip this market
                        continue
                }

                vol, _ := strconv.ParseFloat(m.VolumeFP, 64)
                liq, _ := strconv.ParseFloat(m.LiquidityDollars, 64)

                endDate := ""
                if m.CloseTime != "" {
                        if t, err := time.Parse(time.RFC3339, m.CloseTime); err == nil {
                                endDate = t.Format("2006-01-02")
                        }
                }

                question := m.Title
                if m.Subtitle != "" {
                        question = m.Title + " — " + m.Subtitle
                }

                mkt := Market{
                        ID:           "kal-" + m.Ticker,
                        Platform:     "kalshi",
                        Question:     question,
                        Category:     classifyQuestion(question),
                        Volume:       vol,
                        Liquidity:    liq,
                        StartDate:    "2024-01-01",
                        EndDate:      endDate,
                        Outcomes:     []string{"YES", "NO"},
                        Active:       true,
                        CurrentPrice: yesProb,
                }
                markets = append(markets, mkt)

                key := strings.ToLower(question)
                prices[key] = yesProb
                prices["kal-"+m.Ticker] = yesProb
        }

        return markets, prices, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// classifyQuestion assigns a category based on keywords in the market question.
func classifyQuestion(q string) string {
        q = strings.ToLower(q)
        switch {
        case containsAny(q, "bitcoin", "btc", "ethereum", "eth", "crypto", "sol", "doge", "xrp", "altcoin"):
                return "crypto"
        case containsAny(q, "president", "election", "senate", "congress", "trump", "biden", "harris",
                "democrat", "republican", "vote", "party", "governor", "prime minister", "political"):
                return "politics"
        case containsAny(q, "nfl", "nba", "mlb", "nhl", "soccer", "world cup", "super bowl",
                "championship", "playoff", "match", "game", "player", "team", "stanley cup",
                "series winner", "tennis", "golf", "ufc", "boxing"):
                return "sports"
        case containsAny(q, "fed", "inflation", "gdp", "interest rate", "recession", "stock",
                "s&p", "nasdaq", "dow", "market crash", "ipo", "economy", "tariff"):
                return "finance"
        case containsAny(q, "ai", "artificial intelligence", "gpt", "openai", "anthropic", "model",
                "tech", "apple", "google", "microsoft", "meta", "nvidia"):
                return "tech"
        default:
                return "general"
        }
}

func containsAny(s string, keywords ...string) bool {
        for _, k := range keywords {
                if strings.Contains(s, k) {
                        return true
                }
        }
        return false
}
