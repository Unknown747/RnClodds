# CloddsBot Trading Terminal

## Overview
AI-powered trading bot for prediction markets, crypto & futures. Supports Polymarket, Kalshi, Manifold, Hyperliquid, Binance, and Jupiter. Features stop-loss, take-profit, trailing stop, and smart routing.

## Architecture
- **Runtime**: Node.js 20, ESM modules (`"type": "module"`)
- **Entry point**: `src/index.js`
- **Package manager**: npm

## Project Structure
```
src/
  index.js          - Main entry point, Express server (port 5000)
  config.js         - Configuration (reads from .env)
  trading-bot.js    - Core trading logic
  memory-manager.js - SQLite-based user preferences and trade logs
  position-manager.js - Position tracking with stop-loss/take-profit/trailing stop
  router.js         - Smart order routing across platforms
  market-index.js   - Market search and discovery
public/
  index.html        - Web UI (trading terminal dashboard)
```

## Key Dependencies
- `express` - Web server
- `sqlite3` - Persistent memory/preferences storage
- `ws` - WebSocket support
- `node-fetch` - HTTP requests
- `dotenv` - Environment variable loading

## Server Configuration
- Host: `0.0.0.0`
- Port: `5000` (or `process.env.PORT`)
- Static files served from `public/`

## Environment Variables
Stored in `.env`:
- `ANTHROPIC_API_KEY` - Anthropic AI API key
- `OPENAI_API_KEY` - OpenAI API key
- `GOOGLE_API_KEY` - Google API key
- `DEFAULT_POSITION_SIZE` - Default trade size in USD (default: 100)
- `MAX_POSITION_SIZE` - Max trade size in USD (default: 1000)
- `MAX_DAILY_LOSS` - Max daily loss limit in USD (default: 500)
- `DB_PATH` - SQLite database path (default: ./clodds.db)

## Deployment
- Target: `vm` (always-running, maintains in-memory positions + SQLite state)
- Run command: `node src/index.js`

## Workflow
- **Start application** — runs `npm start`, serves on port 5000 (webview)

## API Endpoints
- `GET /api/status` - Bot status and portfolio summary
- `GET /api/positions` - Open positions
- `GET /api/markets/trending` - Trending markets
- `GET /api/markets/search?q=&category=&minVolume=` - Search markets
- `POST /api/trade` - Execute a trade order
- `POST /api/close` - Close a position
- `POST /api/preferences` - Set user preference
- `POST /api/rules` - Add trading rule

## Notes
- Market data and prices are currently mocked (no live API connections)
- Positions are tracked in-memory; they reset on restart
- SQLite DB persists user preferences and trade logs
- The `MemoryManager` uses async initialization (`this.ready` promise) to ensure DB is ready before queries
