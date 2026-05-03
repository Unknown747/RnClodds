package main

import (
        "encoding/json"
        "log/slog"
        "net/http"
        "os"
        "os/signal"
        "strconv"
        "syscall"
        "time"
)

func main() {
        setupLogger()

        slog.Info("CloddsBot Trading Terminal (Go) starting up")
        slog.Info("Supported platforms: Polymarket, Kalshi, Manifold, Hyperliquid, Binance, Jupiter")
        slog.Info("Trading features: Stop-loss, Take-profit, Trailing stop, Smart routing")

        cfg := LoadConfig()

        mem, err := NewMemoryManager(cfg.DBPath)
        if err != nil {
                slog.Error("Failed to init memory manager", "error", err)
                os.Exit(1)
        }

        bot := NewTradingBot(cfg, mem)
        bot.Start()

        mux := http.NewServeMux()
        mux.Handle("/", http.FileServer(http.Dir("public")))

        mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
                summary, err := bot.GetPortfolioSummary()
                if err != nil {
                        slog.Error("Failed to get portfolio summary", "error", err)
                        jsonError(w, err.Error(), 500)
                        return
                }
                jsonOK(w, map[string]interface{}{
                        "status":      "running",
                        "positions":   summary.Positions,
                        "dailyTrades": summary.DailyTrades,
                        "dailyPnL":    summary.DailyPnL,
                        "preferences": summary.Preferences,
                        "rules":       summary.Rules,
                        "isActive":    summary.IsActive,
                })
        })

        mux.HandleFunc("/api/positions", func(w http.ResponseWriter, r *http.Request) {
                jsonOK(w, bot.GetOpenPositions())
        })

        mux.HandleFunc("/api/markets/trending", func(w http.ResponseWriter, r *http.Request) {
                jsonOK(w, bot.GetTrendingMarkets())
        })

        mux.HandleFunc("/api/markets/search", func(w http.ResponseWriter, r *http.Request) {
                q := r.URL.Query().Get("q")
                category := r.URL.Query().Get("category")
                minVol, _ := strconv.ParseFloat(r.URL.Query().Get("minVolume"), 64)
                results := bot.SearchMarkets(q, MarketFilters{Category: category, MinVolume: minVol})
                jsonOK(w, results)
        })

        mux.HandleFunc("/api/trade", func(w http.ResponseWriter, r *http.Request) {
                if r.Method != http.MethodPost {
                        jsonError(w, "method not allowed", 405)
                        return
                }
                var body struct {
                        Market     string   `json:"market"`
                        Side       string   `json:"side"`
                        Size       float64  `json:"size"`
                        StopLoss   *float64 `json:"stopLoss"`
                        TakeProfit *float64 `json:"takeProfit"`
                }
                if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                        jsonError(w, "invalid request body", 400)
                        return
                }
                result, err := bot.ExecuteOrder(OrderParams{
                        Market:     body.Market,
                        Side:       body.Side,
                        Size:       body.Size,
                        StopLoss:   body.StopLoss,
                        TakeProfit: body.TakeProfit,
                })
                if err != nil {
                        slog.Warn("Trade execution failed", "market", body.Market, "error", err)
                        jsonError(w, err.Error(), 400)
                        return
                }
                jsonOK(w, map[string]interface{}{
                        "success":    true,
                        "orderId":    result.OrderID,
                        "platform":   result.Platform,
                        "entryPrice": result.EntryPrice,
                        "size":       result.Size,
                        "route":      result.Route,
                        "position":   result.Position,
                })
        })

        mux.HandleFunc("/api/close", func(w http.ResponseWriter, r *http.Request) {
                if r.Method != http.MethodPost {
                        jsonError(w, "method not allowed", 405)
                        return
                }
                var body struct {
                        PositionID string `json:"positionId"`
                }
                if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                        jsonError(w, "invalid request body", 400)
                        return
                }
                if err := bot.ClosePosition(body.PositionID); err != nil {
                        slog.Warn("Close position failed", "positionId", body.PositionID, "error", err)
                        jsonError(w, err.Error(), 400)
                        return
                }
                jsonOK(w, map[string]interface{}{"success": true, "positionId": body.PositionID, "closed": true})
        })

        mux.HandleFunc("/api/preferences", func(w http.ResponseWriter, r *http.Request) {
                if r.Method != http.MethodPost {
                        jsonError(w, "method not allowed", 405)
                        return
                }
                var body struct {
                        Key   string `json:"key"`
                        Value string `json:"value"`
                }
                if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                        jsonError(w, "invalid request body", 400)
                        return
                }
                if err := bot.SetPreference(body.Key, body.Value); err != nil {
                        slog.Error("Set preference failed", "key", body.Key, "error", err)
                        jsonError(w, err.Error(), 500)
                        return
                }
                jsonOK(w, map[string]interface{}{"success": true})
        })

        mux.HandleFunc("/api/ai/providers", func(w http.ResponseWriter, r *http.Request) {
                jsonOK(w, map[string]interface{}{
                        "groq":      cfg.APIKeys.Groq != "",
                        "anthropic": cfg.APIKeys.Anthropic != "",
                        "openai":    cfg.APIKeys.OpenAI != "",
                        "google":    cfg.APIKeys.Google != "",
                        "preferred": preferredAIProvider(cfg),
                })
        })

        mux.HandleFunc("/api/analytics", func(w http.ResponseWriter, r *http.Request) {
                data, err := bot.GetAnalytics()
                if err != nil {
                        slog.Error("Failed to get analytics", "error", err)
                        jsonError(w, err.Error(), 500)
                        return
                }
                jsonOK(w, data)
        })

        mux.HandleFunc("/api/rules", func(w http.ResponseWriter, r *http.Request) {
                if r.Method != http.MethodPost {
                        jsonError(w, "method not allowed", 405)
                        return
                }
                var body struct {
                        Rule string `json:"rule"`
                }
                if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                        jsonError(w, "invalid request body", 400)
                        return
                }
                if err := bot.AddRule(body.Rule); err != nil {
                        slog.Error("Add rule failed", "error", err)
                        jsonError(w, err.Error(), 500)
                        return
                }
                jsonOK(w, map[string]interface{}{"success": true})
        })

        addr := cfg.Server.Host + ":" + cfg.Server.Port
        slog.Info("Server listening", "addr", "http://"+addr)

        go func() {
                if err := http.ListenAndServe(addr, requestLogger(mux)); err != nil {
                        slog.Error("Server error", "error", err)
                        os.Exit(1)
                }
        }()

        quit := make(chan os.Signal, 1)
        signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
        <-quit

        slog.Info("Shutting down...")
        bot.Stop()
}

// setupLogger configures slog with time, level, and message.
func setupLogger() {
        slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
                Level: slog.LevelInfo,
                ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
                        if a.Key == slog.TimeKey {
                                a.Value = slog.StringValue(a.Value.Time().Format("15:04:05"))
                        }
                        return a
                },
        })))
}

// requestLogger wraps a handler and logs every HTTP request.
func requestLogger(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                start := time.Now()
                rw := &responseWriter{ResponseWriter: w, status: 200}
                next.ServeHTTP(rw, r)
                if r.URL.Path != "/" {
                        slog.Info("HTTP",
                                "method", r.Method,
                                "path", r.URL.Path,
                                "status", rw.status,
                                "duration", time.Since(start).String(),
                        )
                }
        })
}

type responseWriter struct {
        http.ResponseWriter
        status int
}

func (rw *responseWriter) WriteHeader(code int) {
        rw.status = code
        rw.ResponseWriter.WriteHeader(code)
}

func jsonOK(w http.ResponseWriter, data interface{}) {
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(code)
        json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": msg})
}

func preferredAIProvider(cfg *Config) string {
        if cfg.APIKeys.Google != "" {
                return "google"
        }
        if cfg.APIKeys.Groq != "" {
                return "groq"
        }
        if cfg.APIKeys.OpenAI != "" {
                return "openai"
        }
        if cfg.APIKeys.Anthropic != "" {
                return "anthropic"
        }
        return "none"
}
