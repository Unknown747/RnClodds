/**
 * Smart Router - Routes orders to best platform based on price, liquidity, fees
 * Based on CloddsBot router skill documentation
 * Source: tessl.io/registry/skills/github/alsk1992/CloddsBot/router
 */

class SmartRouter {
  constructor(options = {}) {
    this.platforms = options.platforms || ['polymarket', 'kalshi', 'manifold'];
    this.defaultMode = options.defaultMode || 'balanced';
    
    // Fee structures (based on documentation)
    this.fees = {
      polymarket: { maker: 0, taker: 0, note: 'Zero fees on most markets' },
      kalshi: { maker: 0.17, taker: 1.2, note: 'Formula-based, capped ~2%' },
      manifold: { maker: 0, taker: 0, note: 'Play money' }
    };
    
    // Mock liquidity data (in production, fetch from API)
    this.liquidityData = {
      polymarket: { depth1Pct: 500000, depth2Pct: 1000000, spread: 0.02 },
      kalshi: { depth1Pct: 250000, depth2Pct: 600000, spread: 0.05 },
      manifold: { depth1Pct: 10000, depth2Pct: 50000, spread: 0.1 }
    };
  }

  /**
   * Find best route for an order
   * @param {Object} params - Order parameters
   * @param {string} params.market - Market identifier
   * @param {string} params.side - 'YES'/'NO' or 'long'/'short' or 'buy'/'sell'
   * @param {number} params.size - Order size in dollars
   * @param {string} params.mode - 'best-price' | 'best-liquidity' | 'lowest-fee' | 'balanced'
   * @param {Object} params.weights - Custom weights for balanced mode
   */
  async findBestRoute({ market, side, size, mode = this.defaultMode, weights = null }) {
    const comparison = await this.compare({ market, side, size });
    
    if (mode === 'best-price') {
      const best = comparison.reduce((best, curr) => 
        curr.price < best.price ? curr : best
      );
      return this.createRoute(best, mode);
    }
    
    if (mode === 'best-liquidity') {
      const best = comparison.reduce((best, curr) => 
        curr.liquidity > best.liquidity ? curr : best
      );
      return this.createRoute(best, mode);
    }
    
    if (mode === 'lowest-fee') {
      const best = comparison.reduce((best, curr) => 
        curr.fees < best.fees ? curr : best
      );
      return this.createRoute(best, mode);
    }
    
    // Balanced mode
    const w = weights || { price: 0.4, liquidity: 0.3, fees: 0.3 };
    
    for (const platform of comparison) {
      // Normalize scores
      const bestPrice = Math.min(...comparison.map(p => p.price));
      const bestLiquidity = Math.max(...comparison.map(p => p.liquidity));
      const lowestFees = Math.min(...comparison.map(p => p.fees));
      
      const priceScore = bestPrice / platform.price;
      const liquidityScore = platform.liquidity / bestLiquidity;
      const feeScore = lowestFees / (platform.fees + 0.01);
      
      platform.balancedScore = 
        (priceScore * w.price) +
        (liquidityScore * w.liquidity) +
        (feeScore * w.fees);
    }
    
    const best = comparison.reduce((best, curr) => 
      curr.balancedScore > best.balancedScore ? curr : best
    );
    
    return this.createRoute(best, mode);
  }

  /**
   * Compare all platforms for an order
   * @param {Object} params - Order parameters
   */
  async compare({ market, side, size }) {
    const results = [];
    
    for (const platform of this.platforms) {
      // Mock price data (in production, fetch from market API)
      const basePrice = this.getBasePrice(market);
      const price = this.getPlatformPrice(platform, basePrice, side);
      
      // Calculate fees
      const feeStructure = this.fees[platform];
      const fees = (size * feeStructure.taker) / 100;
      
      // Get liquidity data
      const liquidity = this.liquidityData[platform];
      const slippage = size > liquidity.depth1Pct ? 2 : (size / liquidity.depth1Pct) * 100;
      
      results.push({
        name: platform,
        price: price,
        liquidity: liquidity.depth1Pct,
        slippage: slippage,
        fees: fees,
        netCost: size * price + fees,
        fillProbability: Math.max(0, 100 - slippage),
        score: 0
      });
    }
    
    return results;
  }

  /**
   * Split large order across multiple platforms
   * @param {Object} params - Split parameters
   * @param {string} params.market - Market identifier
   * @param {string} params.side - Order side
   * @param {number} params.size - Total order size
   * @param {number} params.maxSlippage - Maximum allowed slippage
   */
  async splitOrder({ market, side, size, maxSlippage = 0.02 }) {
    const comparison = await this.compare({ market, side, size });
    
    // Filter platforms that can handle the order without exceeding slippage
    const viable = comparison.filter(p => p.slippage <= maxSlippage);
    
    if (viable.length === 0) {
      return { legs: [], totalSlippage: maxSlippage + 1, avgPrice: 0 };
    }
    
    // Distribute order based on liquidity and score
    const totalLiquidity = viable.reduce((sum, p) => sum + p.liquidity, 0);
    const legs = [];
    let remainingSize = size;
    
    for (let i = 0; i < viable.length - 1; i++) {
      const platform = viable[i];
      const allocation = Math.min(
        platform.liquidity * 0.8,
        remainingSize * (platform.liquidity / totalLiquidity)
      );
      legs.push({
        platform: platform.name,
        size: allocation,
        price: platform.price,
        estimatedSlippage: platform.slippage
      });
      remainingSize -= allocation;
    }
    
    // Last platform gets the remainder
    const last = viable[viable.length - 1];
    legs.push({
      platform: last.name,
      size: remainingSize,
      price: last.price,
      estimatedSlippage: last.slippage
    });
    
    const avgPrice = legs.reduce((sum, leg) => sum + leg.price, 0) / legs.length;
    const totalSlippage = legs.reduce((sum, leg) => sum + leg.estimatedSlippage, 0) / legs.length;
    
    return { legs, totalSlippage, avgPrice };
  }

  /**
   * Execute a routed order
   * @param {Object} route - Route object from findBestRoute
   */
  async execute(route) {
    // Simulate execution
    const actualPrice = route.expectedPrice * (1 + (Math.random() - 0.5) * route.expectedSlippage / 100);
    const actualSlippage = Math.abs((actualPrice - route.expectedPrice) / route.expectedPrice) * 100;
    
    return {
      orderId: `ord_${Date.now()}_${Math.random().toString(36).substr(2, 8)}`,
      platform: route.platform,
      fillPrice: actualPrice,
      actualSlippage: actualSlippage,
      fees: route.fees,
      executedAt: new Date().toISOString()
    };
  }

  /**
   * Analyze fee structures across platforms
   * @param {Object} params - Fee analysis parameters
   */
  async analyzeFees({ market, side, size }) {
    const results = [];
    
    for (const [platform, fees] of Object.entries(this.fees)) {
      const totalFee = (size * fees.taker) / 100;
      results.push({
        name: platform,
        makerFee: fees.maker,
        takerFee: fees.taker,
        totalFee: totalFee,
        hasRebate: fees.maker < 0,
        note: fees.note
      });
    }
    
    return results;
  }

  /**
   * Analyze liquidity across platforms
   * @param {Object} params - Liquidity analysis parameters
   */
  async analyzeLiquidity({ market, side }) {
    const results = [];
    
    for (const [platform, liquidity] of Object.entries(this.liquidityData)) {
      results.push({
        name: platform,
        bestBid: this.getBasePrice(market) * 0.99,
        bestAsk: this.getBasePrice(market) * 1.01,
        spread: liquidity.spread,
        depth1Pct: liquidity.depth1Pct,
        depth2Pct: liquidity.depth2Pct,
        note: this.fees[platform]?.note || ''
      });
    }
    
    return results;
  }

  // Helper methods
  getBasePrice(market) {
    // Mock price logic
    const prices = {
      'trump-2028': 0.52,
      'fed-rate-cut': 0.45,
      'btc-100k': 0.38
    };
    return prices[market] || 0.50;
  }

  getPlatformPrice(platform, basePrice, side) {
    // Mock platform-specific pricing (Polymarket usually has best price)
    const multipliers = {
      polymarket: 1.00,
      kalshi: 1.01,
      manifold: 1.02
    };
    return basePrice * (multipliers[platform] || 1.00);
  }

  createRoute(platformData, mode) {
    return {
      platform: platformData.name,
      mode: mode,
      expectedPrice: platformData.price,
      expectedSlippage: platformData.slippage,
      fees: platformData.fees,
      netCost: platformData.netCost,
      fillProbability: platformData.fillProbability,
      score: platformData.balancedScore || platformData.score
    };
  }
}

export default SmartRouter;
