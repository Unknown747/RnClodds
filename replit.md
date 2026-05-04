# CloddsBot Trading Terminal

## Overview
AI-powered trading bot for prediction markets, crypto & futures. Supports Polymarket, Kalshi, Manifold, Hyperliquid, Binance, and Jupiter. Features stop-loss, take-profit, trailing stop, smart routing, AI consensus engine, risk manager, and backtesting.

## Architecture
- **Runtime**: Go 1.25.0
- **Entry point**: `main.go`
- **Database**: SQLite (`modernc.org/sqlite`) at `./clodds.db` — WAL mode enabled
- **Web server**: Standard library `net/http`, serves on port 5000
- **Frontend**: Static HTML/JS dashboard in `public/`

## Project Structure
```
main.go             - Entry point, HTTP server, all API routes
config.go           - All configuration loaded from environment variables
trading_bot.go      - Core orchestrator: AI → risk check → routing → position
ai_engine.go        - AI consensus engine (Gemini, Groq, OpenAI, Anthropic)
risk_manager.go     - Trade limits, daily P&L, drawdown, kill switch, backtest
position_manager.go - Position tracking, stop-loss/take-profit/trailing stop
router.go           - Smart order routing across prediction market platforms
market_index.go     - Market discovery and search (currently mock data)
memory_manager.go   - SQLite persistence for preferences, rules, trade/journal logs
analytics.go        - Historical analytics queries (MemoryManager.GetAnalytics)
cache.go            - In-memory TTL cache with background eviction goroutine
public/
  index.html        - Web UI (trading terminal dashboard)
start               - Bash hot-reload script: builds binary, auto-rebuilds on .go changes
```

## Key Dependencies
- `github.com/joho/godotenv` - Environment variable loading from `.env`
- `modernc.org/sqlite` - Pure Go SQLite implementation

## Server Configuration
- Host: `0.0.0.0`
- Port: `5000` (or `PORT` env var)
- Static files served from `public/`

## Environment Variables

### Secrets — set via Replit Secrets (never in .env)
- `ANTHROPIC_API_KEY` - Anthropic Claude Haiku API key
- `OPENAI_API_KEY` - OpenAI GPT-4o-mini API key
- `GOOGLE_API_KEY` - Google Gemini 2.0 Flash API key
- `GROQ_API_KEY` - Groq LLaMA 70B API key

At least one AI key is required for AI analysis. The bot degrades gracefully without any keys (trade still executes, AI gate is skipped).

### Non-sensitive config — set via Replit env vars (shared)
| Variable | Default | Description |
|---|---|---|
| `DEFAULT_POSITION_SIZE` | 100 | Default trade size (USD) |
| `MAX_POSITION_SIZE` | 1000 | Max single trade size (USD) |
| `MAX_DAILY_LOSS` | 500 | Daily loss limit (USD) |
| `MAX_DAILY_TRADES` | 20 | Max trades per day |
| `MAX_POSITIONS` | 10 | Max concurrent open positions |
| `MAX_DRAWDOWN` | 20 | Max portfolio drawdown (%) |
| `MIN_CONFIDENCE` | 0.6 | Min AI confidence to allow a trade (0.0–1.0) |
| `MAX_LEVERAGE` | 5 | Max leverage multiplier |
| `TRADING_MODE` | balanced | Router mode: balanced / best-price / best-liquidity / lowest-fee |
| `USER_ID` | cli-user | User identifier for SQLite records |
| `AI_DEFAULT_MODEL` | google | Preferred AI provider |
| `DB_PATH` | ./clodds.db | SQLite database file path |
| `PORT` | 5000 | HTTP server port |

## Deployment
- Target: `vm` (always-running, maintains in-memory positions + SQLite state)
- Start: `./start` (builds binary with hot-reload on Go file changes)

## Workflow
- **Start application** — runs `./start`, serves on port 5000 (webview)

## API Endpoints
- `GET /api/status` - Bot status and portfolio summary
- `GET /api/positions` - Open positions
- `GET /api/markets/trending` - Trending markets
- `GET /api/markets/search?q=&category=&minVolume=` - Search markets
- `POST /api/trade` - Execute a trade (AI-gated, risk-checked, routed)
- `POST /api/close` - Close a position
- `POST /api/preferences` - Set user preference
- `POST /api/rules` - Add trading rule
- `GET /api/analytics` - Historical analytics + live session stats
- `POST /api/ai/analyze` - AI consensus market analysis
- `GET /api/ai/providers` - Check which AI providers are configured
- `GET /api/risk` - Risk status (daily trades, PnL, drawdown, limits)
- `POST /api/risk/killswitch` - Toggle kill switch (halt all trading)
- `GET /api/backtest` - Run backtest on journal history

## Bug Fixes Applied (Full Audit)
1. **Anthropic never called** — `callAnthropic()` added; Anthropic now included in AI consensus jobs
2. **Duplicate daily counters** — Removed `dailyTrades`/`dailyPnL` from TradingBot; RiskManager is now single source of truth
3. **Journal duplicate entries** — `LogDaily` now reads + accumulates existing day's data before upsert via `Remember()`; `Remember()` upserts on any non-empty key (not just preferences)
4. **Market index goroutine leak** — `MarketIndex` now has `stopCh chan struct{}` and `Stop()` method; auto-refresh respects the channel
5. **Cache eviction goroutine leak** — `Cache` now has `stopCh` and `Stop()` method; `TradingBot.Stop()` calls both
6. **Hardcoded risk config** — All risk/trading params (`MIN_CONFIDENCE`, `MAX_POSITIONS`, `MAX_DRAWDOWN`, `MAX_DAILY_TRADES`, `TRADING_MODE`, `USER_ID`) now read from env via `envFloat()`/`envInt()`/`getenv()`
7. **Dead code** — `_ = entry` removed from `analytics.go`
8. **SQLite reliability** — WAL mode and 5-second busy timeout added to `NewMemoryManager()`

## Notes
- Market data and prices are currently mocked (no live exchange API connections)
- Positions are tracked in-memory; they reset on restart (trade logs persist in SQLite)
- SQLite DB persists user preferences, rules, AI analysis logs, and daily journals
- AI consensus runs all configured providers in parallel; best-scored result wins
- The `.env` file holds non-sensitive default values only; real API keys must go in Replit Secrets
