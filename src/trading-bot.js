/**
 * Trading Bot - Core trading functionality
 * Integrates memory, positions, routing, and market index
 */

import MemoryManager from './memory-manager.js';
import PositionManager from './position-manager.js';
import SmartRouter from './router.js';
import MarketIndex from './market-index.js';

class TradingBot {
  constructor(config = {}) {
    this.config = config;
    this.memory = new MemoryManager(config.dbPath);
    this.positions = new PositionManager(config.positionOptions);
    this.router = new SmartRouter(config.routerOptions);
    this.marketIndex = new MarketIndex(config.indexOptions);
    this.userId = config.userId || 'default-user';
    
    // Trading state
    this.dailyPnL = 0;
    this.dailyTrades = 0;
    this.isActive = false;
  }

  /**
   * Initialize and start the trading bot
   */
  async start() {
    console.log('🚀 Starting Trading Bot...');
    
    // Start position monitoring
    await this.positions.start();
    
    // Setup event handlers
    this.setupEventHandlers();
    
    this.isActive = true;
    console.log('✅ Trading bot is active');
    
    return this;
  }

  setupEventHandlers() {
    this.positions.on('stopLossTriggered', async (data) => {
      console.log(`🛑 STOP LOSS TRIGGERED:`, data);
      await this.memory.logDaily({
        userId: this.userId,
        date: new Date(),
        trades: 1,
        pnl: data.pnl.pnl,
        notes: `Stop loss triggered on ${data.position.market}`
      });
    });
    
    this.positions.on('takeProfitTriggered', async (data) => {
      console.log(`✅ TAKE PROFIT TRIGGERED:`, data);
      await this.memory.logDaily({
        userId: this.userId,
        date: new Date(),
        trades: 1,
        pnl: data.pnl.pnl,
        notes: `Take profit triggered on ${data.position.market}`
      });
    });
    
    this.positions.on('trailingStopTriggered', async (data) => {
      console.log(`📉 TRAILING STOP TRIGGERED:`, data);
    });
  }

  /**
   * Execute a market order
   * @param {Object} params - Order parameters
   * @param {string} params.market - Market name
   * @param {string} params.side - 'YES'/'NO' or 'buy'/'sell'
   * @param {number} params.size - Position size in dollars
   * @param {Object} params.risk - Risk parameters (stopLoss, takeProfit)
   */
  async executeOrder({ market, side, size, risk = {} }) {
    console.log(`📊 Executing order: ${side} ${market} for $${size}`);
    
    // Check trading limits
    await this.checkTradingLimits();
    
    // Find best route
    const route = await this.router.findBestRoute({
      market,
      side,
      size,
      mode: this.config.tradingMode || 'balanced'
    });
    
    console.log(`🏆 Best route: ${route.platform} @ $${route.expectedPrice}`);
    
    // Get current market price
    const marketData = await this.marketIndex.getMarket(route.platform, market);
    const entryPrice = marketData ? this.getCurrentPrice(marketData) : route.expectedPrice;
    
    // Create position
    const position = this.positions.addPosition({
      platform: route.platform,
      market: market,
      side: side,
      size: size,
      entryPrice: entryPrice
    });
    
    // Set risk management
    if (risk.stopLoss) {
      await this.positions.setStopLoss({
        positionId: position.id,
        ...risk.stopLoss
      });
    }
    
    if (risk.takeProfit) {
      await this.positions.setTakeProfit({
        positionId: position.id,
        ...risk.takeProfit
      });
    }
    
    if (risk.trailingStop) {
      await this.positions.setTrailingStop({
        positionId: position.id,
        ...risk.trailingStop
      });
    }
    
    // Update trading stats
    this.dailyTrades++;
    
    // Store in memory
    await this.memory.remember({
      userId: this.userId,
      type: 'note',
      key: `trade_${position.id}`,
      content: `Executed ${side} ${market} for $${size} @ $${entryPrice}`,
      metadata: { positionId: position.id, platform: route.platform }
    });
    
    return {
      orderId: position.id,
      platform: route.platform,
      entryPrice: entryPrice,
      size: size,
      route: route,
      position: position
    };
  }

  /**
   * Search for markets
   * @param {string} query - Search query
   * @param {Object} filters - Search filters
   */
  async searchMarkets(query, filters = {}) {
    return await this.marketIndex.search(query, filters);
  }

  /**
   * Get trending markets
   */
  async getTrendingMarkets() {
    return await this.marketIndex.getTrendingMarkets({ limit: 10 });
  }

  /**
   * Get markets closing soon
   */
  async getClosingSoonMarkets() {
    return await this.marketIndex.getClosingSoon({ within: '48h', minVolume: 1000 });
  }

  /**
   * Get all open positions
   */
  getOpenPositions() {
    return this.positions.listPositions();
  }

  /**
   * Close a position
   * @param {string} positionId - Position identifier
   */
  async closePosition(positionId) {
    const position = this.positions.getPosition(positionId);
    if (!position) {
      throw new Error(`Position ${positionId} not found`);
    }
    
    // Execute closing order
    console.log(`Closing position ${positionId} at current price`);
    
    // Remove all stops and close
    await this.positions.removeAllStops(positionId);
    this.positions.positions.delete(positionId);
    
    await this.memory.remember({
      userId: this.userId,
      type: 'note',
      key: `close_${positionId}`,
      content: `Closed position ${position.market}`
    });
    
    return { positionId, closed: true };
  }

  /**
   * Get portfolio summary
   */
  async getPortfolioSummary() {
    const positionsSummary = await this.positions.getSummary();
    const preferences = await this.memory.getPreferences(this.userId);
    const rules = await this.memory.getRules(this.userId);
    
    return {
      positions: positionsSummary,
      dailyTrades: this.dailyTrades,
      dailyPnL: this.dailyPnL,
      preferences,
      rules,
      isActive: this.isActive
    };
  }

  /**
   * Set user preference
   * @param {string} key - Preference key
   * @param {string} value - Preference value
   */
  async setPreference(key, value) {
    return await this.memory.remember({
      userId: this.userId,
      type: 'preference',
      key: key,
      content: value
    });
  }

  /**
   * Add trading rule
   * @param {string} rule - Trading rule content
   */
  async addRule(rule) {
    return await this.memory.remember({
      userId: this.userId,
      type: 'rule',
      content: rule,
      metadata: { addedAt: new Date().toISOString() }
    });
  }

  /**
   * Check trading limits before executing order
   */
  async checkTradingLimits() {
    const summary = await this.positions.getSummary();
    
    // Check max positions
    if (summary.count >= this.config.trading.maxPositions) {
      throw new Error(`Max positions (${this.config.trading.maxPositions}) reached`);
    }
    
    // Check daily loss limit
    if (this.dailyPnL <= -this.config.trading.maxDailyLoss) {
      throw new Error(`Daily loss limit of $${this.config.trading.maxDailyLoss} reached`);
    }
    
    return true;
  }

  /**
   * Update daily P&L
   * @param {number} pnl - Profit/Loss amount
   */
  updateDailyPnL(pnl) {
    this.dailyPnL += pnl;
  }

  getCurrentPrice(marketData) {
    // Mock price - in production, fetch from market
    return 0.50;
  }

  /**
   * Stop the trading bot
   */
  async stop() {
    console.log('🛑 Stopping trading bot...');
    this.positions.stop();
    this.memory.close();
    this.isActive = false;
    console.log('✅ Trading bot stopped');
  }
}

export default TradingBot;
