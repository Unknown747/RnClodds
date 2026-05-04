# CloddsBot Trading Terminal

## Overview
AI-powered trading bot for prediction markets, crypto & futures. Supports Polymarket, Kalshi, Manifold, Hyperliquid, Binance, and Jupiter. Features stop-loss, take-profit, trailing stop, smart routing, AI consensus engine, risk manager, and backtesting.

## Architecture
- **Runtime**: Go 1.25.0
- **Entry point**: `main.go`
- **Database**: SQLite (`modernc.org/sqlite`) at `./clodds.db`
- **Web server**: Standard library `net/http`, serves on port 5000
- **Frontend**: Static HTML/JS dashboard in `public/`

## Project Structure
```
main.go           - Entry point, HTTP server, all API routes
config.go         - Configuration loaded from environment variables
trading_bot.go    - Core trading orchestrator (links AI, risk, routing)
ai_engine.go      - AI consensus engine (Gemini, Groq, OpenAI, Anthropic)
risk_manager.go   - Trade limits, daily P&L, kill switch
position_manager.go - Position tracking, stop-loss/take-profit/trailing stop
router.go         - Smart order routing across platforms
market_index.go   - Market discovery and search (currently mock data)
memory_manager.go - SQLite persistence for preferences, rules, trade logs
analytics.go      - Analytics and reporting
cache.go          - In-memory caching utilities
public/
  index.html      - Web UI (trading terminal dashboard)
```

## Key Dependencies
- `github.com/joho/godotenv` - Environment variable loading
- `modernc.org/sqlite` - Pure Go SQLite implementation

## Server Configuration
- Host: `0.0.0.0`
- Port: `5000` (or `PORT` env var)
- Static files served from `public/`

## Environment Variables
Set via Replit Secrets for sensitive values:
- `ANTHROPIC_API_KEY` - Anthropic Claude API key (optional)
- `OPENAI_API_KEY` - OpenAI API key (optional)
- `GOOGLE_API_KEY` - Google Gemini API key (optional)
- `GROQ_API_KEY` - Groq API key (optional)

Set via Replit env vars (non-sensitive):
- `DEFAULT_POSITION_SIZE` - Default trade size in USD (default: 100)
- `MAX_POSITION_SIZE` - Max trade size in USD (default: 1000)
- `MAX_DAILY_LOSS` - Max daily loss limit in USD (default: 500)
- `DB_PATH` - SQLite database path (default: ./clodds.db)

## Deployment
- Target: `vm` (always-running, maintains in-memory positions + SQLite state)
- Start: `./start` (builds binary with hot-reload on Go file changes)
- The `start` script compiles the Go binary and auto-rebuilds on code changes

## Workflow
- **Start application** — runs `./start`, serves on port 5000 (webview)

## API Endpoints
- `GET /api/status` - Bot status and portfolio summary
- `GET /api/positions` - Open positions
- `GET /api/markets/trending` - Trending markets
- `GET /api/markets/search?q=&category=&minVolume=` - Search markets
- `POST /api/trade` - Execute a trade order
- `POST /api/close` - Close a position
- `POST /api/preferences` - Set user preference
- `POST /api/rules` - Add trading rule
- `GET /api/analytics` - Analytics data
- `POST /api/ai/analyze` - AI market analysis (requires AI provider key)
- `GET /api/ai/providers` - Check which AI providers are configured
- `GET /api/risk` - Risk status
- `POST /api/risk/killswitch` - Toggle kill switch
- `GET /api/backtest` - Run backtest

## Notes
- Market data and prices are currently mocked (no live exchange API connections)
- Positions are tracked in-memory; they reset on restart (trade logs persist in SQLite)
- SQLite DB persists user preferences, rules, and AI analysis logs
- AI analysis requires at least one API key (Google, Groq, OpenAI, or Anthropic)
- The app works without AI keys but the `/api/ai/analyze` endpoint will return an error
