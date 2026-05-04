# CloddsBot Trading Terminal

## Overview
AI-powered trading bot for prediction markets, crypto & futures. Supports Polymarket, Kalshi, Manifold, Hyperliquid, Binance, and Jupiter. Features stop-loss, take-profit, trailing stop, smart routing, multi-provider AI consensus, Kelly Criterion sizing, adaptive risk, opportunity scanner, risk manager, and backtesting.

## Architecture
- **Runtime**: Go 1.25.0
- **Entry point**: `main.go`
- **Database**: SQLite (`modernc.org/sqlite`) at `./clodds.db` — WAL mode + 5s busy timeout
- **Web server**: Standard library `net/http`, serves on port 5000
- **Frontend**: Static HTML/JS dashboard in `public/`

## Project Structure
```
main.go             - Entry point, HTTP server, all API routes
config.go           - All configuration loaded from environment variables
trading_bot.go      - Core orchestrator: AI → consensus gate → Kelly sizing → risk → routing → position
ai_engine.go        - Multi-provider AI consensus (Gemini, Groq, OpenAI, Anthropic) with consensus gate
risk_manager.go     - Adaptive risk: Kelly criterion, consecutive loss protection, daily limits, kill switch
scanner.go          - Background opportunity scanner: scans all markets every N minutes
position_manager.go - Position tracking, stop-loss/take-profit/trailing stop
router.go           - Smart order routing; prices come from live MarketIndex (real API data)
market_fetcher.go   - Live market data clients: Manifold (search-markets API) + Kalshi (elections API)
market_index.go     - Market discovery and search; fetches live data on startup + every 5 min, fallback to mock
memory_manager.go   - SQLite persistence: upsert-safe for journals, preferences, rules, trade logs
analytics.go        - Historical analytics queries (MemoryManager.GetAnalytics)
cache.go            - In-memory TTL cache with stoppable eviction goroutine
public/
  index.html        - Web UI (trading terminal dashboard)
start               - Bash hot-reload script: builds binary, auto-rebuilds on .go changes
```

## Trade Pipeline (ExecuteOrder)
```
1. AI Analysis      → call all configured providers concurrently
2. Consensus Gate   → reject if < MIN_CONSENSUS providers agree
3. Confidence Gate  → reject if confidence < effectiveMinConf (adaptive after losses)
4. Kelly Sizing     → compute optimal position size from confidence
5. Risk Check       → daily limits, position count, drawdown
6. Smart Routing    → best platform by price/liquidity/fees
7. Open Position    → add to position manager with optional stops
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

At least one AI key is required for AI analysis. Bot degrades gracefully without any keys.

### Non-sensitive config — set via Replit env vars (shared)
| Variable | Default | Description |
|---|---|---|
| `DEFAULT_POSITION_SIZE` | 100 | Base trade size (USD) used by Kelly formula |
| `MAX_POSITION_SIZE` | 1000 | Hard cap on any single trade (USD) |
| `MAX_DAILY_LOSS` | 500 | Daily loss limit — triggers kill switch (USD) |
| `MAX_DAILY_TRADES` | 20 | Max trades per day |
| `MAX_POSITIONS` | 10 | Max concurrent open positions |
| `MAX_DRAWDOWN` | 20 | Max portfolio drawdown (%) |
| `MIN_CONFIDENCE` | 0.6 | Base AI confidence threshold (0.0–1.0) |
| `KELLY_FRACTION` | 0.25 | Kelly bet fraction (0.25 = quarter Kelly, conservative) |
| `MIN_CONSENSUS` | 1 | Min AI providers that must agree on direction |
| `CONSECUTIVE_LOSS_LIMIT` | 3 | Raises confidence threshold +0.05 per loss; kill switch at 2× limit |
| `MAX_LEVERAGE` | 5 | Max leverage multiplier |
| `TRADING_MODE` | balanced | Router mode: balanced / best-price / best-liquidity / lowest-fee |
| `USER_ID` | cli-user | User identifier for SQLite records |
| `AI_DEFAULT_MODEL` | google | Preferred AI provider |
| `DB_PATH` | ./clodds.db | SQLite database file path |
| `PORT` | 5000 | HTTP server port |
| `SCANNER_ENABLED` | true | Enable background opportunity scanner |
| `SCANNER_INTERVAL_MINS` | 10 | How often scanner runs (minutes) |
| `SCANNER_MIN_CONF` | 0.75 | Min confidence to queue a signal |
| `SCANNER_MAX_SIGNALS` | 20 | Max signals kept in memory |

## Deployment
- Target: `vm` (always-running, maintains in-memory positions + SQLite state)
- Start: `./start` (builds binary with hot-reload on Go file changes)

## API Endpoints
- `GET /api/status` - Bot status and portfolio summary (includes adaptive risk info)
- `GET /api/positions` - Open positions
- `GET /api/markets/trending` - Trending markets
- `GET /api/markets/search?q=&category=&minVolume=` - Search markets
- `POST /api/trade` - Execute trade (AI-gated, Kelly-sized, risk-checked, routed)
  - Body: `{ market, side, size, stopLoss?, takeProfit?, useKelly? }`
  - Set `useKelly: true` to auto-size based on AI confidence
- `POST /api/close` - Close a position
- `GET /api/kelly?confidence=0.75` - Preview Kelly-optimal size for given confidence
- `POST /api/ai/analyze` - AI consensus analysis (all providers, consensus gate)
- `GET /api/ai/providers` - Check which AI providers are configured
- `GET /api/scanner/signals` - Latest high-confidence opportunities from background scanner
- `GET /api/scanner/status` - Scanner operational status (last scan, next scan, signal count)
- `GET /api/risk` - Risk status (daily counters, consecutive losses, adaptive threshold)
- `POST /api/risk/killswitch` - Toggle kill switch (also resets consecutive loss counter)
- `GET /api/analytics` - Historical analytics + live session stats
- `GET /api/backtest` - Run backtest on journal history (equity curve, Sharpe ratio)

## Kelly Criterion Sizing
With `DEFAULT_POSITION_SIZE=100`, `KELLY_FRACTION=0.25`:
| AI Confidence | Kelly Multiplier | Position Size |
|---|---|---|
| 60% | 1.20× | $120 |
| 70% | 1.40× | $140 |
| 80% | 1.60× | $160 |
| 90% | 1.80× | $180 |
| 100% | 2.00× | $200 |

## Adaptive Risk (Consecutive Loss Protection)
| Consecutive Losses | Effective Min Confidence | Status |
|---|---|---|
| 0 | 60% (base) | Normal |
| 1 | 65% | Protection mode |
| 2 | 70% | Protection mode |
| 3 | 75% | Protection mode |
| 6+ | Kill switch | Halted |

## Bug Fixes Applied (Full Audit)
1. Anthropic never called → `callAnthropic()` added with proper Claude Haiku API
2. Duplicate daily counters → RiskManager is single source of truth
3. Journal duplicate entries → `LogDaily` accumulates + upserts; `Remember()` upserts on any non-empty key
4. Market index goroutine leak → `Stop()` method with `stopCh` channel
5. Cache eviction goroutine leak → `Stop()` method with `stopCh` channel
6. Hardcoded risk config → all params configurable from env
7. Dead code `_ = entry` → removed from analytics.go
8. SQLite reliability → WAL mode + 5s busy timeout

## New Features (Enhancement Round)
1. **Kelly Criterion** — auto-sizes positions based on AI confidence (ComputeKellySize)
2. **Consensus Gate** — requires MIN_CONSENSUS providers to agree before allowing trade
3. **Adaptive Risk** — effectiveMinConf rises +0.05 per consecutive loss; auto kill switch at 2× limit
4. **Opportunity Scanner** — background goroutine scans all markets every 10 min, queues signals ≥ 75% confidence
5. **Kelly Calculator API** — `GET /api/kelly?confidence=X` preview sizing without placing a trade
