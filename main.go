package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
)

func main() {
	fmt.Println("============================================================")
	fmt.Println("🤖 CloddsBot Trading Terminal (Go)")
	fmt.Println("============================================================")
	fmt.Println("Supported platforms: Polymarket, Kalshi, Manifold, Hyperliquid, Binance, Jupiter")
	fmt.Println("Trading features: Stop-loss, Take-profit, Trailing stop, Smart routing")
	fmt.Println("============================================================")

	cfg := LoadConfig()

	mem, err := NewMemoryManager(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to init memory manager: %v", err)
	}

	bot := NewTradingBot(cfg, mem)
	bot.Start()

	mux := http.NewServeMux()

	mux.Handle("/", http.FileServer(http.Dir("public")))

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		summary, err := bot.GetPortfolioSummary()
		if err != nil {
			jsonError(w, err.Error(), 500)
			return
		}
		resp := map[string]interface{}{
			"status":   "running",
			"positions": summary.Positions,
			"dailyTrades": summary.DailyTrades,
			"dailyPnL":    summary.DailyPnL,
			"preferences": summary.Preferences,
			"rules":       summary.Rules,
			"isActive":    summary.IsActive,
		}
		jsonOK(w, resp)
	})

	mux.HandleFunc("/api/positions", func(w http.ResponseWriter, r *http.Request) {
		positions := bot.GetOpenPositions()
		jsonOK(w, positions)
	})

	mux.HandleFunc("/api/markets/trending", func(w http.ResponseWriter, r *http.Request) {
		trending := bot.GetTrendingMarkets()
		jsonOK(w, trending)
	})

	mux.HandleFunc("/api/markets/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		category := r.URL.Query().Get("category")
		minVolStr := r.URL.Query().Get("minVolume")
		minVol, _ := strconv.ParseFloat(minVolStr, 64)

		filters := MarketFilters{
			Category:  category,
			MinVolume: minVol,
		}
		results := bot.SearchMarkets(q, filters)
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
			jsonError(w, err.Error(), 400)
			return
		}
		resp := map[string]interface{}{
			"success":    true,
			"orderId":    result.OrderID,
			"platform":   result.Platform,
			"entryPrice": result.EntryPrice,
			"size":       result.Size,
			"route":      result.Route,
			"position":   result.Position,
		}
		jsonOK(w, resp)
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
			jsonError(w, err.Error(), 500)
			return
		}
		jsonOK(w, map[string]interface{}{"success": true})
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
			jsonError(w, err.Error(), 500)
			return
		}
		jsonOK(w, map[string]interface{}{"success": true})
	})

	addr := cfg.Server.Host + ":" + cfg.Server.Port

	go func() {
		fmt.Printf(`
    ╔══════════════════════════════════════════════════════════╗
    ║                                                          ║
    ║   🤖 CloddsBot is running! (Go Edition)                  ║
    ║                                                          ║
    ║   Web interface: http://%s               ║
    ║   API endpoint:  http://%s/api/status    ║
    ║                                                          ║
    ╚══════════════════════════════════════════════════════════╝
`, addr, addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n📝 Shutting down...")
	bot.Stop()
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
