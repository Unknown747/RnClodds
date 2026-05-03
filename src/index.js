/**
 * CloddsBot - Main Entry Point
 * AI-powered trading terminal for prediction markets, crypto & futures
 */

import TradingBot from './trading-bot.js';
import { config } from './config.js';
import express from 'express';

async function main() {
  console.log('='.repeat(60));
  console.log('🤖 CloddsBot Trading Terminal');
  console.log('='.repeat(60));
  console.log('Supported platforms: Polymarket, Kalshi, Manifold, Hyperliquid, Binance, Jupiter');
  console.log('Trading features: Stop-loss, Take-profit, Trailing stop, Smart routing');
  console.log('='.repeat(60));
  
  // Initialize trading bot
  const bot = new TradingBot({
    ...config,
    userId: process.env.USER_ID || 'cli-user',
    tradingMode: 'balanced',
    positionOptions: {
      checkIntervalMs: 5000,
      orderType: 'market'
    },
    routerOptions: {
      platforms: ['polymarket', 'kalshi', 'manifold'],
      defaultMode: 'balanced'
    },
    indexOptions: {
      platforms: ['polymarket', 'kalshi', 'manifold'],
      autoRefresh: true
    }
  });
  
  // Start bot
  await bot.start();
  
  // Setup Express server for web interface
  const app = express();
  const PORT = config.server.port || 18789;
  
  app.use(express.json());
  app.use(express.static('public'));
  
  // API Routes
  app.get('/api/status', async (req, res) => {
    const summary = await bot.getPortfolioSummary();
    res.json({ status: 'running', ...summary });
  });
  
  app.get('/api/positions', async (req, res) => {
    const positions = bot.getOpenPositions();
    res.json(positions);
  });
  
  app.get('/api/markets/trending', async (req, res) => {
    const trending = await bot.getTrendingMarkets();
    res.json(trending);
  });
  
  app.get('/api/markets/search', async (req, res) => {
    const { q, category, minVolume } = req.query;
    const results = await bot.searchMarkets(q || '', { category, minVolume: parseInt(minVolume) || 0 });
    res.json(results);
  });
  
  app.post('/api/trade', async (req, res) => {
    try {
      const { market, side, size, stopLoss, takeProfit } = req.body;
      const result = await bot.executeOrder({
        market,
        side,
        size: parseFloat(size),
        risk: {
          stopLoss: stopLoss ? { price: parseFloat(stopLoss) } : null,
          takeProfit: takeProfit ? { price: parseFloat(takeProfit) } : null
        }
      });
      res.json({ success: true, ...result });
    } catch (error) {
      res.status(400).json({ success: false, error: error.message });
    }
  });
  
  app.post('/api/close', async (req, res) => {
    try {
      const { positionId } = req.body;
      const result = await bot.closePosition(positionId);
      res.json({ success: true, ...result });
    } catch (error) {
      res.status(400).json({ success: false, error: error.message });
    }
  });
  
  app.post('/api/preferences', async (req, res) => {
    const { key, value } = req.body;
    await bot.setPreference(key, value);
    res.json({ success: true });
  });
  
  app.post('/api/rules', async (req, res) => {
    const { rule } = req.body;
    await bot.addRule(rule);
    res.json({ success: true });
  });
  
  // Start server
  app.listen(PORT, () => {
    console.log(`
    ╔══════════════════════════════════════════════════════════╗
    ║                                                          ║
    ║   🤖 CloddsBot is running!                               ║
    ║                                                          ║
    ║   Web interface: http://${config.server.host}:${PORT}           ║
    ║   API endpoint:  http://${config.server.host}:${PORT}/api/status  ║
    ║                                                          ║
    ║   Commands you can use:                                  ║
    ║   - /positions: View all open positions                  ║
    ║   - /search <query>: Search markets                      ║
    ║   - /trending: Show trending markets                     ║
    ║   - /close <id>: Close a position                        ║
    ║                                                          ║
    ╚══════════════════════════════════════════════════════════╝
    `);
  });
  
  // Handle graceful shutdown
  process.on('SIGINT', async () => {
    console.log('\n📝 Shutting down...');
    await bot.stop();
    process.exit(0);
  });
  
  return bot;
}

// Run the bot
main().catch(console.error);

export default main;
