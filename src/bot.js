import { dbRun, dbAll, dbGet } from './database.js';
import { config } from './config.js';

let botRunning = false;
let botInterval = null;
let broadcastFn = null;

const DEFAULT_POSITION_SIZE = config.trading.defaultPositionSize;
const MAX_POSITION_SIZE = config.trading.maxPositionSize;
const MAX_DAILY_LOSS = config.trading.maxDailyLoss;

const markets = [
  { id: 'BTC-USD', name: 'Bitcoin', basePrice: 65000, volatility: 0.02 },
  { id: 'ETH-USD', name: 'Ethereum', basePrice: 3200, volatility: 0.025 },
  { id: 'SOL-USD', name: 'Solana', basePrice: 145, volatility: 0.035 },
  { id: 'DOGE-USD', name: 'Dogecoin', basePrice: 0.15, volatility: 0.05 },
];

const marketPrices = {};
markets.forEach(m => { marketPrices[m.id] = m.basePrice; });

export function setBroadcast(fn) {
  broadcastFn = fn;
}

function broadcast(type, data) {
  if (broadcastFn) {
    broadcastFn({ type, data });
  }
}

function simulatePrice(market) {
  const current = marketPrices[market.id];
  const change = current * market.volatility * (Math.random() - 0.5) * 0.4;
  const newPrice = Math.max(current + change, current * 0.85);
  marketPrices[market.id] = newPrice;
  return newPrice;
}

function generateSignal(market, price) {
  const rand = Math.random();
  if (rand < 0.3) return 'buy';
  if (rand < 0.6) return 'sell';
  return null;
}

async function getDailyPnl() {
  const today = new Date().toISOString().split('T')[0];
  const result = await dbGet(
    `SELECT COALESCE(SUM(pnl), 0) as total FROM trades WHERE date(closed_at) = ? AND status = 'closed'`,
    [today]
  );
  return result ? result.total : 0;
}

async function runBotCycle() {
  if (!botRunning) return;

  const dailyPnl = await getDailyPnl();

  for (const market of markets) {
    const price = simulatePrice(market);

    await dbRun(
      `INSERT INTO market_data (market, price, volume) VALUES (?, ?, ?)`,
      [market.id, price, Math.random() * 1000000]
    );

    broadcast('price_update', { market: market.id, price, timestamp: new Date().toISOString() });

    if (dailyPnl <= -MAX_DAILY_LOSS) {
      await logBot('warn', `Daily loss limit reached ($${MAX_DAILY_LOSS}). Skipping trades.`);
      continue;
    }

    const signal = generateSignal(market, price);

    if (signal) {
      const size = DEFAULT_POSITION_SIZE + Math.random() * (MAX_POSITION_SIZE - DEFAULT_POSITION_SIZE) * 0.3;
      const strategy = ['momentum', 'mean_reversion', 'breakout'][Math.floor(Math.random() * 3)];

      const result = await dbRun(
        `INSERT INTO trades (market, side, size, entry_price, status, strategy) VALUES (?, ?, ?, ?, 'open', ?)`,
        [market.id, signal, size, price, strategy]
      );

      const tradeId = result.lastID;

      await logBot('info', `New ${signal.toUpperCase()} trade on ${market.id} at $${price.toFixed(4)}`, {
        tradeId, size, strategy
      });

      broadcast('new_trade', { id: tradeId, market: market.id, side: signal, size, entry_price: price, strategy, status: 'open' });

      setTimeout(async () => {
        await closeTrade(tradeId, market, price);
      }, 5000 + Math.random() * 25000);
    }
  }

  const stats = await getStats();
  broadcast('stats_update', stats);
}

async function closeTrade(tradeId, market, entryPrice) {
  const exitPrice = marketPrices[market.id] || entryPrice;
  const trade = await dbGet(`SELECT * FROM trades WHERE id = ?`, [tradeId]);
  if (!trade || trade.status === 'closed') return;

  const priceDiff = trade.side === 'buy' ? exitPrice - entryPrice : entryPrice - exitPrice;
  const pnl = (priceDiff / entryPrice) * trade.size;

  await dbRun(
    `UPDATE trades SET exit_price = ?, pnl = ?, status = 'closed', closed_at = CURRENT_TIMESTAMP WHERE id = ?`,
    [exitPrice, pnl, tradeId]
  );

  await logBot('info', `Closed trade #${tradeId} on ${market.id}. PnL: $${pnl.toFixed(2)}`, {
    tradeId, exitPrice, pnl
  });

  broadcast('trade_closed', { id: tradeId, market: market.id, exit_price: exitPrice, pnl });

  const stats = await getStats();
  broadcast('stats_update', stats);
}

async function logBot(level, message, data = null) {
  await dbRun(
    `INSERT INTO bot_logs (level, message, data) VALUES (?, ?, ?)`,
    [level, message, data ? JSON.stringify(data) : null]
  );
}

export async function getStats() {
  const [totalTrades, openTrades, closedTrades, totalPnl, winRate, dailyPnl] = await Promise.all([
    dbGet(`SELECT COUNT(*) as count FROM trades`),
    dbGet(`SELECT COUNT(*) as count FROM trades WHERE status = 'open'`),
    dbGet(`SELECT COUNT(*) as count FROM trades WHERE status = 'closed'`),
    dbGet(`SELECT COALESCE(SUM(pnl), 0) as total FROM trades WHERE status = 'closed'`),
    dbGet(`SELECT 
      ROUND(100.0 * SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) / NULLIF(COUNT(*), 0), 1) as rate 
      FROM trades WHERE status = 'closed'`),
    dbGet(`SELECT COALESCE(SUM(pnl), 0) as total FROM trades WHERE status = 'closed' AND date(closed_at) = date('now')`)
  ]);

  return {
    totalTrades: totalTrades?.count || 0,
    openTrades: openTrades?.count || 0,
    closedTrades: closedTrades?.count || 0,
    totalPnl: totalPnl?.total || 0,
    winRate: winRate?.rate || 0,
    dailyPnl: dailyPnl?.total || 0,
    botRunning,
    prices: { ...marketPrices }
  };
}

export async function startBot() {
  if (botRunning) return { success: false, message: 'Bot is already running' };
  botRunning = true;
  await logBot('info', 'Bot started');
  broadcast('bot_status', { running: true });
  botInterval = setInterval(runBotCycle, 3000);
  runBotCycle();
  return { success: true, message: 'Bot started successfully' };
}

export async function stopBot() {
  if (!botRunning) return { success: false, message: 'Bot is not running' };
  botRunning = false;
  if (botInterval) {
    clearInterval(botInterval);
    botInterval = null;
  }
  await logBot('info', 'Bot stopped');
  broadcast('bot_status', { running: false });
  return { success: true, message: 'Bot stopped successfully' };
}

export function isBotRunning() {
  return botRunning;
}

export function getMarketPrices() {
  return { ...marketPrices };
}
