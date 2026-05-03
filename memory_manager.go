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

func (mm *MemoryManager) Remember(userID, memType, key, content string, metadata map[string]interface{}) error {
	metaBytes, _ := json.Marshal(metadata)

	if key != "" && memType == "preference" {
		var id int64
		err := mm.db.QueryRow(
			`SELECT id FROM memories WHERE userId=? AND type=? AND key=?`,
			userID, memType, key,
		).Scan(&id)

		if err == nil {
			_, err = mm.db.Exec(
				`UPDATE memories SET content=?, metadata=?, updatedAt=CURRENT_TIMESTAMP WHERE id=?`,
				content, string(metaBytes), id,
			)
			return err
		}
	}

	_, err := mm.db.Exec(
		`INSERT INTO memories (userId, type, key, content, metadata) VALUES (?,?,?,?,?)`,
		userID, memType, key, content, string(metaBytes),
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

func (mm *MemoryManager) LogDaily(userID string, date time.Time, trades int, pnl float64, notes string) error {
	key := fmt.Sprintf("journal_%s", date.Format("2006-01-02"))
	content := fmt.Sprintf("Trades: %d, P&L: $%.2f, Notes: %s", trades, pnl, notes)
	meta := map[string]interface{}{
		"type":   "journal",
		"trades": trades,
		"pnl":    pnl,
		"date":   date.Format(time.RFC3339),
	}
	return mm.Remember(userID, "note", key, content, meta)
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
