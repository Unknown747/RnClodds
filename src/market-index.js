/**
 * Market Index - Search and discover markets across platforms
 * Based on CloddsBot market-index skill documentation
 * Source: tessl.io/registry/skills/github/alsk1992/CloddsBot/market-index
 */

class MarketIndex {
  constructor(options = {}) {
    this.platforms = options.platforms || ['polymarket', 'kalshi', 'manifold'];
    this.cache = new Map();
    this.lastUpdated = null;
    this.autoRefresh = options.autoRefresh || false;
    this.refreshIntervalMs = options.refreshIntervalMs || 300000; // 5 minutes
    
    if (this.autoRefresh) {
      this.startAutoRefresh();
    }
    
    // Mock market data
    this.markets = this.initializeMockMarkets();
  }

  initializeMockMarkets() {
    return [
      // Politics
      { id: 'poly-001', platform: 'polymarket', question: 'Will Trump win 2028 election?', category: 'politics', volume: 12500000, liquidity: 2500000, startDate: '2024-01-01', endDate: '2028-11-05', outcomes: ['YES', 'NO'], active: true },
      { id: 'kal-001', platform: 'kalshi', question: 'Will Trump win 2028 election?', category: 'politics', volume: 5200000, liquidity: 1100000, startDate: '2024-01-01', endDate: '2028-11-05', outcomes: ['YES', 'NO'], active: true },
      
      // Crypto
      { id: 'poly-002', platform: 'polymarket', question: 'Will Bitcoin hit $100K in 2024?', category: 'crypto', volume: 8750000, liquidity: 1800000, startDate: '2024-01-01', endDate: '2024-12-31', outcomes: ['YES', 'NO'], active: true },
      { id: 'man-001', platform: 'manifold', question: 'ETH above $4000 by EOY?', category: 'crypto', volume: 125000, liquidity: 50000, startDate: '2024-01-01', endDate: '2024-12-31', outcomes: ['YES', 'NO'], active: true },
      
      // Finance
      { id: 'poly-003', platform: 'polymarket', question: 'Will Fed cut rates in September?', category: 'finance', volume: 3450000, liquidity: 890000, startDate: '2024-06-01', endDate: '2024-09-30', outcomes: ['YES', 'NO'], active: true },
      { id: 'kal-002', platform: 'kalshi', question: 'Will Fed cut rates in September?', category: 'finance', volume: 2100000, liquidity: 450000, startDate: '2024-06-01', endDate: '2024-09-30', outcomes: ['YES', 'NO'], active: true },
      
      // Sports
      { id: 'poly-004', platform: 'polymarket', question: 'Will Chiefs win Super Bowl?', category: 'sports', volume: 4300000, liquidity: 920000, startDate: '2024-08-01', endDate: '2025-02-09', outcomes: ['YES', 'NO'], active: true }
    ];
  }

  startAutoRefresh() {
    setInterval(() => {
      this.update();
    }, this.refreshIntervalMs);
  }

  /**
   * Search markets
   * @param {string} query - Search query
   * @param {Object} filters - Search filters
   * @returns {Array} Matching markets
   */
  async search(query, filters = {}) {
    let results = [...this.markets];
    
    // Apply search query
    if (query) {
      const lowerQuery = query.toLowerCase();
      results = results.filter(m => 
        m.question.toLowerCase().includes(lowerQuery) ||
        m.category.toLowerCase().includes(lowerQuery)
      );
    }
    
    // Apply filters
    if (filters.platforms && filters.platforms.length) {
      results = results.filter(m => filters.platforms.includes(m.platform));
    }
    
    if (filters.category) {
      results = results.filter(m => m.category === filters.category);
    }
    
    if (filters.minVolume) {
      results = results.filter(m => m.volume >= filters.minVolume);
    }
    
    if (filters.activeOnly) {
      results = results.filter(m => m.active === true);
    }
    
    if (filters.endsBefore) {
      results = results.filter(m => m.endDate <= filters.endsBefore);
    }
    
    // Sort results
    const sortBy = filters.sortBy || 'relevance';
    if (sortBy === 'volume') {
      results.sort((a, b) => b.volume - a.volume);
    } else if (sortBy === 'endDate') {
      results.sort((a, b) => new Date(a.endDate) - new Date(b.endDate));
    } else if (sortBy === 'created') {
      results.sort((a, b) => new Date(b.startDate) - new Date(a.startDate));
    }
    
    const limit = filters.limit || 20;
    return results.slice(0, limit);
  }

  /**
   * Get markets by category
   * @param {string} category - Market category
   * @param {Object} options - Pagination options
   */
  async getMarketsByCategory(category, options = {}) {
    return this.search('', { category, ...options });
  }

  /**
   * Get all categories with market counts
   */
  async getCategories() {
    const categories = {};
    
    for (const market of this.markets) {
      if (!categories[market.category]) {
        categories[market.category] = { name: market.category, marketCount: 0 };
      }
      categories[market.category].marketCount++;
    }
    
    return Object.values(categories);
  }

  /**
   * Get newly created markets
   * @param {Object} options - Time filter options
   */
  async getNewMarkets(options = {}) {
    const since = options.since || Date.now() - 24 * 60 * 60 * 1000;
    const sinceDate = new Date(since);
    
    return this.markets.filter(m => new Date(m.startDate) >= sinceDate);
  }

  /**
   * Get trending/hot markets
   * @param {Object} options - Trending options
   */
  async getTrendingMarkets(options = {}) {
    const period = options.period || '24h';
    // Sort by volume change (mock implementation)
    const trending = [...this.markets].sort((a, b) => b.volume - a.volume);
    const limit = options.limit || 10;
    
    return trending.slice(0, limit).map(m => ({
      ...m,
      volumeChange: Math.floor(Math.random() * 60) - 10 // Mock volume change %
    }));
  }

  /**
   * Get markets closing soon
   * @param {Object} options - Time window options
   */
  async getClosingSoon(options = {}) {
    const within = options.within || '48h'; // '24h', '48h', '7d'
    const now = new Date();
    
    let hours;
    if (within === '24h') hours = 24;
    else if (within === '48h') hours = 48;
    else hours = 168;
    
    const cutoff = new Date(now.getTime() + hours * 60 * 60 * 1000);
    
    let results = this.markets.filter(m => {
      const endDate = new Date(m.endDate);
      return endDate <= cutoff && endDate > now && m.active;
    });
    
    results.sort((a, b) => new Date(a.endDate) - new Date(b.endDate));
    
    if (options.minVolume) {
      results = results.filter(m => m.volume >= options.minVolume);
    }
    
    const limit = options.limit || 20;
    return results.slice(0, limit);
  }

  /**
   * Get single market details
   * @param {string} platform - Platform name
   * @param {string} marketId - Market identifier
   */
  async getMarket(platform, marketId) {
    return this.markets.find(m => m.platform === platform && m.id === marketId);
  }

  /**
   * Update market index from sources
   */
  async update(platform = null) {
    if (platform) {
      console.log(`Updating market index for ${platform}...`);
    } else {
      console.log('Updating all market indices...');
    }
    this.lastUpdated = new Date();
    return true;
  }

  /**
   * Get index statistics
   */
  async getStats() {
    const byPlatform = {};
    const byCategory = {};
    
    for (const market of this.markets) {
      byPlatform[market.platform] = (byPlatform[market.platform] || 0) + 1;
      byCategory[market.category] = (byCategory[market.category] || 0) + 1;
    }
    
    return {
      totalMarkets: this.markets.length,
      byPlatform,
      byCategory,
      lastUpdated: this.lastUpdated
    };
  }

  /**
   * Get index health status
   */
  async getStatus() {
    const ageMinutes = this.lastUpdated 
      ? (Date.now() - this.lastUpdated.getTime()) / (1000 * 60)
      : null;
    
    return {
      status: ageMinutes && ageMinutes < 10 ? 'healthy' : 'stale',
      marketCount: this.markets.length,
      platforms: this.platforms,
      ageMinutes: ageMinutes || 0,
      lastUpdated: this.lastUpdated
    };
  }
}

export default MarketIndex;
