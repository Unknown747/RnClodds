package main

import (
	"log/slog"
	"math"
	"sync"
	"time"
)

// Signal is a high-confidence trading opportunity found by the scanner.
type Signal struct {
	Market     string    `json:"market"`
	Platform   string    `json:"platform"`
	Direction  string    `json:"direction"`
	Confidence float64   `json:"confidence"`
	RiskScore  float64   `json:"riskScore"`
	Reason     string    `json:"reason"`
	Provider   string    `json:"provider"`
	Consensus  string    `json:"consensus"`
	Score      float64   `json:"score"`
	DetectedAt time.Time `json:"detectedAt"`
}

// ScannerStatus reports the scanner's current state.
type ScannerStatus struct {
	Enabled      bool      `json:"enabled"`
	Running      bool      `json:"running"`
	SignalCount   int       `json:"signalCount"`
	LastScanAt   *time.Time `json:"lastScanAt"`
	NextScanAt   *time.Time `json:"nextScanAt"`
	MarketsScanned int     `json:"marketsScanned"`
	IntervalMins int       `json:"intervalMins"`
	MinSignalConf float64  `json:"minSignalConf"`
}

// OpportunityScanner scans all known markets on a schedule and queues high-confidence signals.
type OpportunityScanner struct {
	cfg            *Config
	aiEngine       *AIEngine
	marketIndex    *MarketIndex
	userID         string
	mu             sync.RWMutex
	signals        []Signal
	lastScanAt     *time.Time
	nextScanAt     *time.Time
	marketsScanned int
	running        bool
	stopCh         chan struct{}
}

func NewOpportunityScanner(cfg *Config, ai *AIEngine, mi *MarketIndex, userID string) *OpportunityScanner {
	return &OpportunityScanner{
		cfg:         cfg,
		aiEngine:    ai,
		marketIndex: mi,
		userID:      userID,
		signals:     []Signal{},
		stopCh:      make(chan struct{}),
	}
}

// Start begins the background scanning loop.
func (s *OpportunityScanner) Start() {
	if !s.cfg.Scanner.Enabled {
		slog.Info("Opportunity scanner disabled")
		return
	}
	s.running = true
	go s.loop()
	slog.Info("Opportunity scanner started",
		"intervalMins", s.cfg.Scanner.IntervalMins,
		"minConfidence", s.cfg.Scanner.MinSignalConf,
	)
}

// Stop signals the scanner loop to exit.
func (s *OpportunityScanner) Stop() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	s.running = false
	slog.Info("Opportunity scanner stopped")
}

func (s *OpportunityScanner) loop() {
	// Run an initial scan immediately on start
	s.scan()

	interval := time.Duration(s.cfg.Scanner.IntervalMins) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.scan()
		case <-s.stopCh:
			return
		}
	}
}

// scan iterates all markets, runs AI analysis, and stores high-confidence signals.
func (s *OpportunityScanner) scan() {
	markets := s.marketIndex.Search("", MarketFilters{ActiveOnly: true, Limit: 50})
	if len(markets) == 0 {
		return
	}

	now := time.Now()
	s.mu.Lock()
	s.lastScanAt = &now
	next := now.Add(time.Duration(s.cfg.Scanner.IntervalMins) * time.Minute)
	s.nextScanAt = &next
	s.mu.Unlock()

	slog.Info("Scanner: starting market scan", "markets", len(markets))

	var wg sync.WaitGroup
	found := make(chan Signal, len(markets))

	for _, m := range markets {
		wg.Add(1)
		go func(mkt Market) {
			defer wg.Done()
			result, err := s.aiEngine.Analyze(s.userID, mkt.Question, "")
			if err != nil {
				return
			}
			if result.Best.Confidence >= s.cfg.Scanner.MinSignalConf {
				found <- Signal{
					Market:     mkt.Question,
					Platform:   mkt.Platform,
					Direction:  result.Best.Direction,
					Confidence: result.Best.Confidence,
					RiskScore:  result.Best.RiskScore,
					Reason:     result.Best.Reason,
					Provider:   result.Best.Provider,
					Consensus:  result.Consensus,
					Score:      result.Best.Score,
					DetectedAt: time.Now(),
				}
			}
		}(m)
	}

	wg.Wait()
	close(found)

	var newSignals []Signal
	for sig := range found {
		newSignals = append(newSignals, sig)
	}

	// Sort by confidence descending (simple insertion sort)
	for i := 1; i < len(newSignals); i++ {
		for j := i; j > 0 && newSignals[j].Confidence > newSignals[j-1].Confidence; j-- {
			newSignals[j], newSignals[j-1] = newSignals[j-1], newSignals[j]
		}
	}

	s.mu.Lock()
	// Prepend new signals; keep only the newest MaxSignals entries
	s.signals = append(newSignals, s.signals...)
	max := s.cfg.Scanner.MaxSignals
	if len(s.signals) > max {
		s.signals = s.signals[:max]
	}
	s.marketsScanned += len(markets)
	s.mu.Unlock()

	slog.Info("Scanner: scan complete",
		"marketsScanned", len(markets),
		"signalsFound", len(newSignals),
	)
}

// GetSignals returns all current signals above the configured confidence threshold.
func (s *OpportunityScanner) GetSignals() []Signal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.signals) == 0 {
		return []Signal{}
	}
	out := make([]Signal, len(s.signals))
	copy(out, s.signals)
	return out
}

// GetStatus returns the scanner's current operational status.
func (s *OpportunityScanner) GetStatus() ScannerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ScannerStatus{
		Enabled:        s.cfg.Scanner.Enabled,
		Running:        s.running,
		SignalCount:    len(s.signals),
		LastScanAt:     s.lastScanAt,
		NextScanAt:     s.nextScanAt,
		MarketsScanned: s.marketsScanned,
		IntervalMins:   s.cfg.Scanner.IntervalMins,
		MinSignalConf:  math.Round(s.cfg.Scanner.MinSignalConf*1000) / 1000,
	}
}
