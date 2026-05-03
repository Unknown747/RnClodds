import dotenv from 'dotenv';
dotenv.config();

export const config = {
  apiKeys: {
    anthropic: process.env.ANTHROPIC_API_KEY,
    openai: process.env.OPENAI_API_KEY,
    google: process.env.GOOGLE_API_KEY
  },
  trading: {
    defaultPositionSize: parseInt(process.env.DEFAULT_POSITION_SIZE) || 100,
    maxPositionSize: parseInt(process.env.MAX_POSITION_SIZE) || 1000,
    maxDailyLoss: parseInt(process.env.MAX_DAILY_LOSS) || 500,
    maxLeverage: 5
  },
  risk: {
    maxDrawdown: 20,
    maxPositions: 10,
    minConfidence: 0.6
  },
  platforms: {
    predictionMarkets: ['polymarket', 'kalshi', 'manifold'],
    futures: ['hyperliquid', 'binance', 'bybit'],
    dex: ['jupiter', 'raydium', 'pumpdotfun']
  },
  server: {
    port: 18789,
    host: 'localhost'
  }
};
