/**
 * Position Manager - Manages trading positions with stop-loss and take-profit
 * Based on CloddsBot positions skill documentation
 * Source: tessl.io/registry/skills/github/alsk1992/CloddsBot/positions
 */

import EventEmitter from 'events';

class PositionManager extends EventEmitter {
  constructor(options = {}) {
    super();
    this.positions = new Map();
    this.checkIntervalMs = options.checkIntervalMs || 5000;
    this.orderType = options.orderType || 'market';
    this.limitBuffer = options.limitBuffer || 0.01;
    this.intervalId = null;
  }

  /**
   * Start monitoring positions
   */
  async start() {
    if (this.intervalId) return;
    
    this.intervalId = setInterval(() => {
      this.monitorPositions();
    }, this.checkIntervalMs);
    
    console.log('Position manager started');
  }

  /**
   * Stop monitoring
   */
  stop() {
    if (this.intervalId) {
      clearInterval(this.intervalId);
      this.intervalId = null;
    }
  }

  /**
   * Add or update a position
   * @param {Object} position - Position data
   */
  addPosition(position) {
    const pos = {
      id: position.id || this.generateId(),
      platform: position.platform,
      market: position.market,
      side: position.side,
      size: position.size,
      entryPrice: position.entryPrice,
      currentPrice: position.entryPrice,
      stopLoss: position.stopLoss || null,
      takeProfit: position.takeProfit || null,
      trailingStop: position.trailingStop || null,
      highWaterMark: position.entryPrice,
      createdAt: new Date(),
      updatedAt: new Date()
    };
    
    this.positions.set(pos.id, pos);
    this.emit('positionAdded', pos);
    
    return pos;
  }

  /**
   * Update position price
   * @param {string} positionId - Position identifier
   * @param {number} currentPrice - Current market price
   */
  updatePrice(positionId, currentPrice) {
    const pos = this.positions.get(positionId);
    if (!pos) return null;
    
    pos.currentPrice = currentPrice;
    pos.updatedAt = new Date();
    
    // Update high water mark for trailing stop
    if (currentPrice > pos.highWaterMark) {
      pos.highWaterMark = currentPrice;
    }
    
    this.positions.set(positionId, pos);
    return pos;
  }

  /**
   * List all positions
   * @param {string} platform - Filter by platform (optional)
   */
  listPositions(platform = null) {
    const positions = Array.from(this.positions.values());
    
    if (platform) {
      return positions.filter(p => p.platform === platform);
    }
    
    return positions;
  }

  /**
   * Get single position details
   * @param {string} positionId - Position identifier
   */
  getPosition(positionId) {
    return this.positions.get(positionId);
  }

  /**
   * Set stop-loss for a position
   * @param {Object} params - Stop-loss parameters
   * @param {string} params.positionId - Position ID
   * @param {number} params.price - Stop price (absolute)
   * @param {number} params.percentFromEntry - Stop percentage from entry
   * @param {number} params.sizePercent - Percentage of position to exit
   */
  async setStopLoss({ positionId, price = null, percentFromEntry = null, sizePercent = 100 }) {
    const pos = this.positions.get(positionId);
    if (!pos) throw new Error(`Position ${positionId} not found`);
    
    let stopPrice = price;
    
    if (percentFromEntry && !price) {
      // Calculate percentage from entry
      const direction = pos.side === 'YES' || pos.side === 'long' ? 1 : -1;
      stopPrice = pos.entryPrice * (1 - (percentFromEntry / 100) * direction);
    }
    
    pos.stopLoss = {
      price: stopPrice,
      sizePercent,
      setAt: new Date()
    };
    
    this.positions.set(positionId, pos);
    this.emit('stopLossSet', { positionId, stopPrice, sizePercent });
    
    return { positionId, stopPrice, sizePercent };
  }

  /**
   * Set take-profit for a position
   * @param {Object} params - Take-profit parameters
   * @param {string} params.positionId - Position ID
   * @param {number} params.price - Target price (absolute)
   * @param {number} params.percentFromEntry - Target percentage from entry
   * @param {Array} params.levels - Multiple take-profit levels
   */
  async setTakeProfit({ positionId, price = null, percentFromEntry = null, levels = null }) {
    const pos = this.positions.get(positionId);
    if (!pos) throw new Error(`Position ${positionId} not found`);
    
    if (levels) {
      pos.takeProfit = {
        type: 'multi-level',
        levels: levels.map(level => ({
          price: level.price,
          sizePercent: level.sizePercent,
          triggered: false
        })),
        setAt: new Date()
      };
    } else {
      let targetPrice = price;
      
      if (percentFromEntry && !price) {
        const direction = pos.side === 'YES' || pos.side === 'long' ? 1 : -1;
        targetPrice = pos.entryPrice * (1 + (percentFromEntry / 100) * direction);
      }
      
      pos.takeProfit = {
        type: 'single',
        price: targetPrice,
        sizePercent: 100,
        setAt: new Date()
      };
    }
    
    this.positions.set(positionId, pos);
    this.emit('takeProfitSet', { positionId, takeProfit: pos.takeProfit });
    
    return { positionId, takeProfit: pos.takeProfit };
  }

  /**
   * Set trailing stop for a position
   * @param {Object} params - Trailing stop parameters
   * @param {string} params.positionId - Position ID
   * @param {number} params.trailPercent - Percentage to trail from high
   * @param {number} params.activateAt - Price to activate trailing stop
   */
  async setTrailingStop({ positionId, trailPercent, activateAt = null }) {
    const pos = this.positions.get(positionId);
    if (!pos) throw new Error(`Position ${positionId} not found`);
    
    pos.trailingStop = {
      trailPercent,
      activateAt,
      isActive: activateAt ? false : true,
      setAt: new Date()
    };
    
    this.positions.set(positionId, pos);
    this.emit('trailingStopSet', { positionId, trailPercent, activateAt });
    
    return { positionId, trailPercent, activateAt };
  }

  /**
   * Remove all stops from a position
   * @param {string} positionId - Position identifier
   */
  async removeAllStops(positionId) {
    const pos = this.positions.get(positionId);
    if (!pos) throw new Error(`Position ${positionId} not found`);
    
    pos.stopLoss = null;
    pos.takeProfit = null;
    pos.trailingStop = null;
    
    this.positions.set(positionId, pos);
    this.emit('stopsRemoved', positionId);
    
    return { positionId, removed: true };
  }

  /**
   * Monitor positions and trigger stops
   */
  async monitorPositions() {
    for (const [id, pos] of this.positions) {
      await this.checkStopLoss(pos);
      await this.checkTakeProfit(pos);
      await this.checkTrailingStop(pos);
    }
  }

  async checkStopLoss(pos) {
    if (!pos.stopLoss) return;
    
    const shouldTrigger = pos.side === 'YES' || pos.side === 'long'
      ? pos.currentPrice <= pos.stopLoss.price
      : pos.currentPrice >= pos.stopLoss.price;
    
    if (shouldTrigger) {
      this.emit('stopLossTriggered', {
        position: pos,
        exitPrice: pos.currentPrice,
        pnl: this.calculatePnL(pos),
        sizePercent: pos.stopLoss.sizePercent
      });
      
      // Close or reduce position
      if (pos.stopLoss.sizePercent >= 100) {
        this.positions.delete(pos.id);
      } else {
        // Partial exit - reduce size
        pos.size = pos.size * (1 - pos.stopLoss.sizePercent / 100);
        pos.stopLoss = null;
        this.positions.set(pos.id, pos);
      }
    }
  }

  async checkTakeProfit(pos) {
    if (!pos.takeProfit) return;
    
    if (pos.takeProfit.type === 'single') {
      const shouldTrigger = pos.side === 'YES' || pos.side === 'long'
        ? pos.currentPrice >= pos.takeProfit.price
        : pos.currentPrice <= pos.takeProfit.price;
      
      if (shouldTrigger) {
        this.emit('takeProfitTriggered', {
          position: pos,
          exitPrice: pos.currentPrice,
          pnl: this.calculatePnL(pos)
        });
        this.positions.delete(pos.id);
      }
    } else if (pos.takeProfit.type === 'multi-level') {
      for (const level of pos.takeProfit.levels) {
        if (!level.triggered) {
          const shouldTrigger = pos.side === 'YES' || pos.side === 'long'
            ? pos.currentPrice >= level.price
            : pos.currentPrice <= level.price;
          
          if (shouldTrigger) {
            level.triggered = true;
            this.emit('takeProfitTriggered', {
              position: pos,
              exitPrice: pos.currentPrice,
              sizePercent: level.sizePercent,
              level: level
            });
            
            // Partial exit
            pos.size = pos.size * (1 - level.sizePercent / 100);
            this.positions.set(pos.id, pos);
          }
        }
      }
      
      // Check if all levels triggered
      const allTriggered = pos.takeProfit.levels.every(l => l.triggered);
      if (allTriggered) {
        this.positions.delete(pos.id);
      }
    }
  }

  async checkTrailingStop(pos) {
    if (!pos.trailingStop) return;
    
    const trail = pos.trailingStop;
    
    // Check activation condition
    if (!trail.isActive && trail.activateAt) {
      if (pos.currentPrice >= trail.activateAt) {
        trail.isActive = true;
        pos.highWaterMark = pos.currentPrice;
        this.emit('trailingStopActivated', { positionId: pos.id });
      } else {
        return;
      }
    }
    
    if (!trail.isActive) return;
    
    const trailPrice = pos.highWaterMark * (1 - trail.trailPercent / 100);
    const shouldTrigger = pos.currentPrice <= trailPrice;
    
    if (shouldTrigger) {
      this.emit('trailingStopTriggered', {
        position: pos,
        exitPrice: pos.currentPrice,
        highWaterMark: pos.highWaterMark,
        pnl: this.calculatePnL(pos)
      });
      this.positions.delete(pos.id);
    }
  }

  calculatePnL(position) {
    const priceDiff = position.currentPrice - position.entryPrice;
    const direction = position.side === 'YES' || position.side === 'long' ? 1 : -1;
    const pnl = direction * priceDiff * position.size;
    const pnlPercent = (pnl / (position.entryPrice * position.size)) * 100;
    
    return { pnl, pnlPercent };
  }

  /**
   * Get position summary
   */
  async getSummary() {
    const positions = Array.from(this.positions.values());
    let totalValue = 0;
    let totalPnl = 0;
    let withStopLoss = 0;
    let withTakeProfit = 0;
    
    for (const pos of positions) {
      const value = pos.currentPrice * pos.size;
      totalValue += value;
      totalPnl += this.calculatePnL(pos).pnl;
      if (pos.stopLoss) withStopLoss++;
      if (pos.takeProfit) withTakeProfit++;
    }
    
    return {
      count: positions.length,
      totalValue,
      unrealizedPnl: totalPnl,
      withStopLoss,
      withTakeProfit,
      withTrailingStop: positions.filter(p => p.trailingStop).length
    };
  }

  generateId() {
    return `pos_${Date.now()}_${Math.random().toString(36).substr(2, 8)}`;
  }
}

export default PositionManager;
