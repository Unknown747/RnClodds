package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Memory struct {
	ID        int64           `json:"id"`
	UserID    string          `json:"userId"`
	Type      string          `json:"type"`
	Key       string          `json:"key"`
	Content   string          `json:"content"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type MemoryManager struct {
	db *sql.DB
}

func NewMemoryManager(dbPath string) (*MemoryManager, error) {
	if dbPath == "" {
		dbPath = "./clodds.db"
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Improve SQLite concurrency and reliability
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	mm := &MemoryManager{db: db}
	if err := mm.initDB(); err != nil {
		return nil, fmt.Errorf("init db: %w", err)
	}
	return mm, nil
}

func (mm *MemoryManager) initDB() error {
	_, err := mm.db.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			userId    TEXT NOT NULL,
			type      TEXT CHECK(type IN ('preference','fact','note','rule','context')),
			key       TEXT,
			content   TEXT NOT NULL,
			metadata  TEXT,
			createdAt DATETIME DEFAULT CURRENT_TIMESTAMP,
			updatedAt DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return err
	}
	_, err = mm.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_memories_user_type ON memories(userId, type)
	`)
	return err
}

// Remember stores a memory entry.
// If key is non-empty: upserts (update existing row with same userId+type+key, or insert if none).
//   This prevents duplicate journal/preference entries.
// If key is empty: always inserts (used for append-only records like trading rules).
func (mm *MemoryManager) Remember(userID, memType, key, content string, metadata map[string]interface{}) error {
	metaBytes, _ := json.Marshal(metadata)

	if key != "" {
		// Update existing record if it exists
		res, err := mm.db.Exec(
			`UPDATE memories SET content=?, metadata=?, updatedAt=CURRENT_TIMESTAMP
			 WHERE userId=? AND type=? AND key=?`,
			content, string(metaBytes), userID, memType, key,
		)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			return nil // updated successfully
		}
		// No existing record — insert new
		_, err = mm.db.Exec(
			`INSERT INTO memories (userId, type, key, content, metadata) VALUES (?,?,?,?,?)`,
			userID, memType, key, content, string(metaBytes),
		)
		return err
	}

	// No key — always insert (e.g. trading rules accumulate over time)
	_, err := mm.db.Exec(
		`INSERT INTO memories (userId, type, key, content, metadata) VALUES (?,?,?,?,?)`,
		userID, memType, nil, content, string(metaBytes),
	)
	return err
}

func (mm *MemoryManager) Recall(userID, memType, key string) ([]Memory, error) {
	query := `SELECT id, userId, type, COALESCE(key,''), content, COALESCE(metadata,'{}'), createdAt, updatedAt FROM memories WHERE userId=?`
	args := []interface{}{userID}

	if memType != "" {
		query += ` AND type=?`
		args = append(args, memType)
	}
	if key != "" {
		query += ` AND key=?`
		args = append(args, key)
	}
	query += ` ORDER BY createdAt DESC`

	rows, err := mm.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		var createdAt, updatedAt string
		if err := rows.Scan(&m.ID, &m.UserID, &m.Type, &m.Key, &m.Content, &m.Metadata, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		m.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
		memories = append(memories, m)
	}
	return memories, rows.Err()
}

func (mm *MemoryManager) GetPreferences(userID string) (map[string]string, error) {
	prefs, err := mm.Recall(userID, "preference", "")
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, p := range prefs {
		if p.Key != "" {
			result[p.Key] = p.Content
		}
	}
	return result, nil
}

func (mm *MemoryManager) GetRules(userID string) ([]Memory, error) {
	return mm.Recall(userID, "rule", "")
}

// LogDaily upserts a daily journal entry, accumulating trades and PnL for the given date.
// Calling this multiple times on the same day merges the values rather than creating duplicate rows.
func (mm *MemoryManager) LogDaily(userID string, date time.Time, trades int, pnl float64, notes string) error {
	key := fmt.Sprintf("journal_%s", date.Format("2006-01-02"))

	// Read existing entry to accumulate trades and PnL
	var existingTrades int
	var existingPnL float64
	var rawMeta string
	err := mm.db.QueryRow(
		`SELECT COALESCE(metadata,'{}') FROM memories WHERE userId=? AND type='note' AND key=?`,
		userID, key,
	).Scan(&rawMeta)
	if err == nil {
		var meta map[string]interface{}
		if json.Unmarshal([]byte(rawMeta), &meta) == nil {
			if t, ok := meta["trades"].(float64); ok {
				existingTrades = int(t)
			}
			if p, ok := meta["pnl"].(float64); ok {
				existingPnL = p
			}
		}
	}

	totalTrades := existingTrades + trades
	totalPnL := existingPnL + pnl
	content := fmt.Sprintf("Trades: %d, P&L: $%.2f, Notes: %s", totalTrades, totalPnL, notes)
	meta := map[string]interface{}{
		"type":   "journal",
		"trades": totalTrades,
		"pnl":    totalPnL,
		"date":   date.Format(time.RFC3339),
	}
	return mm.Remember(userID, "note", key, content, meta)
}

// GetJournals fetches up to `limit` daily journal entries ordered chronologically.
func (mm *MemoryManager) GetJournals(userID string, limit int) ([]JournalEntry, error) {
	rows, err := mm.db.Query(
		`SELECT key, content, COALESCE(metadata,'{}') FROM memories
		 WHERE userId=? AND type='note' AND key LIKE 'journal_%'
		 ORDER BY key ASC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []JournalEntry
	for rows.Next() {
		var key, content, rawMeta string
		if err := rows.Scan(&key, &content, &rawMeta); err != nil {
			continue
		}
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(rawMeta), &meta); err != nil {
			continue
		}
		pnl, _ := meta["pnl"].(float64)
		trades := 0
		if t, ok := meta["trades"].(float64); ok {
			trades = int(t)
		}
		date := key
		if len(key) > 8 {
			date = key[8:]
		}
		entries = append(entries, JournalEntry{Date: date, Trades: trades, PnL: pnl, Notes: content})
	}
	return entries, rows.Err()
}

func (mm *MemoryManager) Forget(userID, memType, key string) (int64, error) {
	query := `DELETE FROM memories WHERE userId=? AND type=?`
	args := []interface{}{userID, memType}
	if key != "" {
		query += ` AND key=?`
		args = append(args, key)
	}
	res, err := mm.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (mm *MemoryManager) Close() {
	mm.db.Close()
}
