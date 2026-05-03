const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
const ws = new WebSocket(`${wsProtocol}//${window.location.host}`);

let currentTab = 'all';
let tradesCache = [];
let pricesCache = {};

const els = {
  wsStatus: document.getElementById('ws-status'),
  botToggle: document.getElementById('bot-toggle'),
  botStatusLabel: document.getElementById('bot-status-label'),
  totalPnl: document.getElementById('total-pnl'),
  dailyPnl: document.getElementById('daily-pnl'),
  winRate: document.getElementById('win-rate'),
  closedTrades: document.getElementById('closed-trades'),
  openTrades: document.getElementById('open-trades'),
  totalTrades: document.getElementById('total-trades'),
  tradesBody: document.getElementById('trades-body'),
  logsContainer: document.getElementById('logs-container'),
  clearLogs: document.getElementById('clear-logs')
};

function formatCurrency(value, digits = 2) {
  const num = Number(value || 0);
  return `$${num.toFixed(digits)}`;
}

function renderStats(stats) {
  els.totalPnl.textContent = formatCurrency(stats.totalPnl);
  els.dailyPnl.textContent = `Today: ${formatCurrency(stats.dailyPnl)}`;
  els.winRate.textContent = `${Number(stats.winRate || 0).toFixed(1)}%`;
  els.closedTrades.textContent = `${stats.closedTrades || 0} closed trades`;
  els.openTrades.textContent = stats.openTrades || 0;
  els.totalTrades.textContent = `${stats.totalTrades || 0} total`;
  els.botStatusLabel.textContent = stats.botRunning ? 'Running' : 'Stopped';
  els.botStatusLabel.className = `stat-value ${stats.botRunning ? 'status-running' : 'status-stopped'}`;
  els.botToggle.textContent = stats.botRunning ? 'Stop Bot' : 'Start Bot';
  els.botToggle.className = `btn ${stats.botRunning ? 'btn-stop' : 'btn-start'}`;

  if (stats.prices) {
    pricesCache = stats.prices;
    for (const [market, price] of Object.entries(stats.prices)) {
      const priceEl = document.getElementById(`price-${market}`);
      if (priceEl) priceEl.textContent = formatCurrency(price, market === 'DOGE-USD' ? 4 : 2);
    }
  }
}

function renderTrades() {
  const filtered = currentTab === 'all'
    ? tradesCache
    : tradesCache.filter(t => t.status === currentTab);

  if (!filtered.length) {
    els.tradesBody.innerHTML = '<tr><td colspan="9" class="empty-row">No trades available.</td></tr>';
    return;
  }

  els.tradesBody.innerHTML = filtered.map(trade => `
    <tr>
      <td>#${trade.id}</td>
      <td>${trade.market}</td>
      <td class="side ${trade.side}">${trade.side.toUpperCase()}</td>
      <td>${formatCurrency(trade.size)}</td>
      <td>${formatCurrency(trade.entry_price, trade.market === 'DOGE-USD' ? 4 : 2)}</td>
      <td>${trade.exit_price ? formatCurrency(trade.exit_price, trade.market === 'DOGE-USD' ? 4 : 2) : '-'}</td>
      <td class="${Number(trade.pnl || 0) >= 0 ? 'pnl-positive' : 'pnl-negative'}">${trade.pnl ? formatCurrency(trade.pnl) : '-'}</td>
      <td>${trade.strategy || '-'}</td>
      <td><span class="status-badge ${trade.status}">${trade.status}</span></td>
    </tr>
  `).join('');
}

function addLog(message, level = 'info') {
  const entry = document.createElement('div');
  entry.className = `log-entry ${level}`;
  entry.textContent = `[${new Date().toLocaleTimeString()}] ${message}`;
  els.logsContainer.prepend(entry);
}

async function loadInitialData() {
  const [statsRes, tradesRes, logsRes, statusRes] = await Promise.all([
    fetch('/api/stats'),
    fetch('/api/trades?limit=25'),
    fetch('/api/logs?limit=50'),
    fetch('/api/bot/status')
  ]);

  const stats = await statsRes.json();
  tradesCache = await tradesRes.json();
  const logs = await logsRes.json();
  const status = await statusRes.json();

  stats.botRunning = status.running;
  renderStats(stats);
  renderTrades();

  els.logsContainer.innerHTML = '';
  logs.reverse().forEach(log => addLog(log.message, log.level));
}

ws.addEventListener('open', () => {
  els.wsStatus.innerHTML = '<span class="dot connected"></span><span class="status-text">Connected</span>';
});

ws.addEventListener('close', () => {
  els.wsStatus.innerHTML = '<span class="dot disconnected"></span><span class="status-text">Disconnected</span>';
});

ws.addEventListener('message', (event) => {
  const msg = JSON.parse(event.data);

  if (msg.type === 'price_update') {
    const { market, price } = msg.data;
    const el = document.getElementById(`price-${market}`);
    if (el) el.textContent = formatCurrency(price, market === 'DOGE-USD' ? 4 : 2);
  }

  if (msg.type === 'new_trade') {
    tradesCache.unshift({ ...msg.data, created_at: new Date().toISOString() });
    renderTrades();
    addLog(`New ${msg.data.side.toUpperCase()} trade on ${msg.data.market}`, 'info');
  }

  if (msg.type === 'trade_closed') {
    const idx = tradesCache.findIndex(t => t.id === msg.data.id);
    if (idx !== -1) {
      tradesCache[idx] = { ...tradesCache[idx], ...msg.data, status: 'closed' };
      renderTrades();
    }
    addLog(`Trade #${msg.data.id} closed with PnL ${formatCurrency(msg.data.pnl)}`, 'info');
  }

  if (msg.type === 'stats_update') {
    renderStats(msg.data);
  }

  if (msg.type === 'bot_status') {
    renderStats({ ...pricesCache, ...msg.data, botRunning: msg.data.running });
  }
});

els.botToggle.addEventListener('click', async () => {
  const endpoint = els.botToggle.textContent.includes('Start') ? '/api/bot/start' : '/api/bot/stop';
  const res = await fetch(endpoint, { method: 'POST' });
  const data = await res.json();
  addLog(data.message || data.error || 'Bot action completed', data.error ? 'error' : 'info');
  await loadInitialData();
});

Array.from(document.querySelectorAll('.tab')).forEach(tab => {
  tab.addEventListener('click', () => {
    document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
    tab.classList.add('active');
    currentTab = tab.dataset.tab;
    renderTrades();
  });
});

els.clearLogs.addEventListener('click', () => {
  els.logsContainer.innerHTML = '';
});

loadInitialData();
