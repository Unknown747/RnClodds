package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ProviderResult is what one AI provider returns.
type ProviderResult struct {
	Provider   string  `json:"provider"`
	Direction  string  `json:"direction"`
	Confidence float64 `json:"confidence"`
	RiskScore  float64 `json:"riskScore"`
	Reason     string  `json:"reason"`
	Score      float64 `json:"score"`
	LatencyMs  int64   `json:"latencyMs"`
	Error      string  `json:"error,omitempty"`
}

// ConsensusResult is the final decision after scoring all providers.
type ConsensusResult struct {
	Best      ProviderResult   `json:"best"`
	All       []ProviderResult `json:"all"`
	Market    string           `json:"market"`
	Consensus string           `json:"consensus"` // majority direction
	AnalyzedAt time.Time       `json:"analyzedAt"`
}

type AIEngine struct {
	cfg    *Config
	client *http.Client
	mem    *MemoryManager
}

func NewAIEngine(cfg *Config, mem *MemoryManager) *AIEngine {
	return &AIEngine{
		cfg: cfg,
		mem: mem,
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

// Analyze calls all available providers concurrently, scores results, returns best.
func (e *AIEngine) Analyze(userID, market, context string) (*ConsensusResult, error) {
	prompt := buildPrompt(market, context)

	type job struct {
		name string
		fn   func(string) ProviderResult
	}

	var jobs []job
	if e.cfg.APIKeys.Google != "" {
		jobs = append(jobs, job{"google", e.callGemini})
	}
	if e.cfg.APIKeys.Groq != "" {
		jobs = append(jobs, job{"groq", e.callGroq})
	}
	if e.cfg.APIKeys.OpenAI != "" {
		jobs = append(jobs, job{"openai", e.callOpenAI})
	}

	if len(jobs) == 0 {
		return nil, fmt.Errorf("no AI providers configured")
	}

	results := make([]ProviderResult, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(idx int, jb job) {
			defer wg.Done()
			r := jb.fn(prompt)
			r.Score = scoreResult(r)
			results[idx] = r
		}(i, j)
	}
	wg.Wait()

	// Filter out errors, keep valid ones
	var valid []ProviderResult
	for _, r := range results {
		if r.Error == "" {
			valid = append(valid, r)
		}
	}

	if len(valid) == 0 {
		// All failed — return first error
		return nil, fmt.Errorf("all providers failed: %s", results[0].Error)
	}

	// Pick best score
	best := valid[0]
	for _, r := range valid[1:] {
		if r.Score > best.Score {
			best = r
		}
	}

	// Majority consensus
	counts := map[string]int{}
	for _, r := range valid {
		counts[r.Direction]++
	}
	consensus := best.Direction
	maxCount := 0
	for dir, c := range counts {
		if c > maxCount {
			maxCount = c
			consensus = dir
		}
	}

	result := &ConsensusResult{
		Best:       best,
		All:        results,
		Market:     market,
		Consensus:  consensus,
		AnalyzedAt: time.Now(),
	}

	slog.Info("AI consensus",
		"market", market,
		"best_provider", best.Provider,
		"direction", best.Direction,
		"confidence", best.Confidence,
		"score", best.Score,
		"consensus", consensus,
	)

	// Save result to memory
	e.saveResult(userID, result)
	return result, nil
}

// callGroq calls the Groq API (OpenAI-compatible endpoint).
func (e *AIEngine) callGroq(prompt string) ProviderResult {
	start := time.Now()
	r := ProviderResult{Provider: "groq"}

	body := map[string]interface{}{
		"model": "llama-3.3-70b-versatile",
		"messages": []map[string]string{
			{"role": "system", "content": "You are an expert prediction market trading analyst. Respond ONLY in valid JSON with fields: direction (buy/sell/hold), confidence (0.0-1.0), risk_score (0.0-1.0), reason (max 120 chars)."},
			{"role": "user", "content": prompt},
		},
		"response_format": map[string]string{"type": "json_object"},
		"temperature":     0.2,
		"max_tokens":      200,
	}

	data, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+e.cfg.APIKeys.Groq)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	r.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		r.Error = fmt.Sprintf("groq HTTP %d: %s", resp.StatusCode, string(raw))
		return r
	}

	var out struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		r.Error = "failed to parse groq response"
		return r
	}

	return parseAIJSON(out.Choices[0].Message.Content, "groq", r.LatencyMs)
}

// callGemini calls the Google Gemini API.
func (e *AIEngine) callGemini(prompt string) ProviderResult {
	start := time.Now()
	r := ProviderResult{Provider: "google"}

	systemPrompt := "You are an expert prediction market trading analyst. Respond ONLY in valid JSON with fields: direction (buy/sell/hold), confidence (0.0-1.0), risk_score (0.0-1.0), reason (max 120 chars)."

	body := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]string{
					{"text": systemPrompt + "\n\n" + prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"responseMimeType": "application/json",
			"temperature":      0.2,
			"maxOutputTokens":  200,
		},
	}

	data, _ := json.Marshal(body)
	url := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=" + e.cfg.APIKeys.Google
	req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	r.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		r.Error = fmt.Sprintf("gemini HTTP %d: %s", resp.StatusCode, string(raw))
		return r
	}

	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		r.Error = "failed to parse gemini response"
		return r
	}

	return parseAIJSON(out.Candidates[0].Content.Parts[0].Text, "google", r.LatencyMs)
}

// callOpenAI calls the OpenAI API (fallback).
func (e *AIEngine) callOpenAI(prompt string) ProviderResult {
	start := time.Now()
	r := ProviderResult{Provider: "openai"}

	body := map[string]interface{}{
		"model": "gpt-4o-mini",
		"messages": []map[string]string{
			{"role": "system", "content": "You are an expert prediction market trading analyst. Respond ONLY in valid JSON with fields: direction (buy/sell/hold), confidence (0.0-1.0), risk_score (0.0-1.0), reason (max 120 chars)."},
			{"role": "user", "content": prompt},
		},
		"response_format": map[string]string{"type": "json_object"},
		"temperature":     0.2,
		"max_tokens":      200,
	}

	data, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+e.cfg.APIKeys.OpenAI)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	r.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		r.Error = fmt.Sprintf("openai HTTP %d: %s", resp.StatusCode, string(raw))
		return r
	}

	var out struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		r.Error = "failed to parse openai response"
		return r
	}

	return parseAIJSON(out.Choices[0].Message.Content, "openai", r.LatencyMs)
}

// parseAIJSON parses the JSON text from any provider.
func parseAIJSON(text, provider string, latencyMs int64) ProviderResult {
	r := ProviderResult{Provider: provider, LatencyMs: latencyMs}

	text = strings.TrimSpace(text)
	// Strip markdown code fences if present
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var parsed struct {
		Direction  string  `json:"direction"`
		Confidence float64 `json:"confidence"`
		RiskScore  float64 `json:"risk_score"`
		Reason     string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		r.Error = fmt.Sprintf("json parse error: %s | raw: %s", err.Error(), text)
		return r
	}

	dir := strings.ToLower(strings.TrimSpace(parsed.Direction))
	if dir != "buy" && dir != "sell" && dir != "hold" {
		dir = "hold"
	}

	r.Direction = dir
	r.Confidence = math.Round(clamp(parsed.Confidence, 0, 1)*1000) / 1000
	r.RiskScore = math.Round(clamp(parsed.RiskScore, 0, 1)*1000) / 1000
	r.Reason = parsed.Reason
	return r
}

// scoreResult computes composite score: high confidence + low risk.
func scoreResult(r ProviderResult) float64 {
	if r.Error != "" {
		return 0
	}
	dirBonus := 0.0
	if r.Direction == "buy" || r.Direction == "sell" {
		dirBonus = 0.05
	}
	return math.Round((r.Confidence*0.65+(1-r.RiskScore)*0.30+dirBonus)*1000) / 1000
}

func buildPrompt(market, context string) string {
	ctx := context
	if ctx == "" {
		ctx = "No extra context provided."
	}
	return fmt.Sprintf(
		`Analyze this prediction market: "%s"\n\nContext: %s\n\nBased on market sentiment, probability trends, and risk factors, provide your trading recommendation.`,
		market, ctx,
	)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// saveResult stores the consensus result to SQLite.
func (e *AIEngine) saveResult(userID string, r *ConsensusResult) {
	key := fmt.Sprintf("ai_%s_%d", r.Market, r.AnalyzedAt.UnixMilli())
	content := fmt.Sprintf("Provider=%s dir=%s conf=%.2f score=%.3f consensus=%s",
		r.Best.Provider, r.Best.Direction, r.Best.Confidence, r.Best.Score, r.Consensus)
	meta := map[string]interface{}{
		"market":     r.Market,
		"provider":   r.Best.Provider,
		"direction":  r.Best.Direction,
		"confidence": r.Best.Confidence,
		"score":      r.Best.Score,
		"consensus":  r.Consensus,
	}
	if err := e.mem.Remember(userID, "context", key, content, meta); err != nil {
		slog.Warn("Failed to save AI result", "error", err)
	}
}
