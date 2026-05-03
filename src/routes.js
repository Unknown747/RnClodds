import express from 'express';
import { dbAll, dbGet, dbRun } from './database.js';
import { startBot, stopBot, getStats, isBotRunning, getMarketPrices } from './bot.js';

const router = express.Router();

router.get('/stats', async (req, res) => {
  try {
    const stats = await getStats();
    res.json(stats);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/trades', async (req, res) => {
  try {
    const limit = parseInt(req.query.limit) || 50;
    const status = req.query.status || null;
    let sql = `SELECT * FROM trades`;
    const params = [];
    if (status) {
      sql += ` WHERE status = ?`;
      params.push(status);
    }
    sql += ` ORDER BY created_at DESC LIMIT ?`;
    params.push(limit);
    const trades = await dbAll(sql, params);
    res.json(trades);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/trades/:id', async (req, res) => {
  try {
    const trade = await dbGet(`SELECT * FROM trades WHERE id = ?`, [req.params.id]);
    if (!trade) return res.status(404).json({ error: 'Trade not found' });
    res.json(trade);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/logs', async (req, res) => {
  try {
    const limit = parseInt(req.query.limit) || 100;
    const logs = await dbAll(
      `SELECT * FROM bot_logs ORDER BY created_at DESC LIMIT ?`,
      [limit]
    );
    res.json(logs);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/prices', async (req, res) => {
  try {
    res.json(getMarketPrices());
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/market-history/:market', async (req, res) => {
  try {
    const limit = parseInt(req.query.limit) || 100;
    const data = await dbAll(
      `SELECT price, volume, timestamp FROM market_data WHERE market = ? ORDER BY timestamp DESC LIMIT ?`,
      [req.params.market, limit]
    );
    res.json(data.reverse());
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.post('/bot/start', async (req, res) => {
  try {
    const result = await startBot();
    res.json(result);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.post('/bot/stop', async (req, res) => {
  try {
    const result = await stopBot();
    res.json(result);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/bot/status', async (req, res) => {
  try {
    res.json({ running: isBotRunning() });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/config', async (req, res) => {
  try {
    const configs = await dbAll(`SELECT key, value FROM bot_config`);
    const config = {};
    configs.forEach(c => { config[c.key] = c.value; });
    res.json(config);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.post('/config', async (req, res) => {
  try {
    const entries = Object.entries(req.body);
    for (const [key, value] of entries) {
      await dbRun(
        `INSERT INTO bot_config (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = CURRENT_TIMESTAMP`,
        [key, String(value), String(value)]
      );
    }
    res.json({ success: true });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

export default router;
