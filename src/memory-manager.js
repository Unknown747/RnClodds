/**
 * Memory Manager - Stores user preferences and trading rules
 * Based on CloddsBot memory skill documentation
 * Source: tessl.io/registry/skills/github/alsk1992/CloddsBot/memory
 */

import sqlite3 from 'sqlite3';
import { promisify } from 'util';

class MemoryManager {
  constructor(dbPath = './clodds.db') {
    this.db = new sqlite3.Database(dbPath);
    this.initDatabase();
  }

  initDatabase() {
    this.db.run(`
      CREATE TABLE IF NOT EXISTS memories (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        userId TEXT NOT NULL,
        type TEXT CHECK(type IN ('preference', 'fact', 'note', 'rule', 'context')),
        key TEXT,
        content TEXT NOT NULL,
        metadata TEXT,
        createdAt DATETIME DEFAULT CURRENT_TIMESTAMP,
        updatedAt DATETIME DEFAULT CURRENT_TIMESTAMP
      )
    `);

    this.db.run(`
      CREATE INDEX IF NOT EXISTS idx_memories_user_type 
      ON memories(userId, type)
    `);
  }

  /**
   * Store a memory
   * @param {Object} params - Memory parameters
   * @param {string} params.userId - User identifier
   * @param {string} params.type - 'preference' | 'fact' | 'note' | 'rule'
   * @param {string} params.key - Memory key (for preferences)
   * @param {string} params.content - Memory content
   * @param {object} params.metadata - Optional metadata
   */
  async remember({ userId, type, key = null, content, metadata = {} }) {
    return new Promise((resolve, reject) => {
      // Check if already exists
      if (key && type === 'preference') {
        this.db.get(
          `SELECT id FROM memories WHERE userId = ? AND type = ? AND key = ?`,
          [userId, type, key],
          (err, row) => {
            if (row) {
              // Update existing
              this.db.run(
                `UPDATE memories 
                 SET content = ?, metadata = ?, updatedAt = CURRENT_TIMESTAMP 
                 WHERE userId = ? AND type = ? AND key = ?`,
                [content, JSON.stringify(metadata), userId, type, key],
                (err) => err ? reject(err) : resolve({ updated: true, key })
              );
            } else {
              // Insert new
              this.db.run(
                `INSERT INTO memories (userId, type, key, content, metadata) 
                 VALUES (?, ?, ?, ?, ?)`,
                [userId, type, key, content, JSON.stringify(metadata)],
                (err) => err ? reject(err) : resolve({ inserted: true, key })
              );
            }
          }
        );
      } else {
        // Insert without key
        this.db.run(
          `INSERT INTO memories (userId, type, content, metadata) 
           VALUES (?, ?, ?, ?)`,
          [userId, type, content, JSON.stringify(metadata)],
          (err) => err ? reject(err) : resolve({ inserted: true })
        );
      }
    });
  }

  /**
   * Recall memories
   * @param {Object} params - Recall parameters
   * @param {string} params.userId - User identifier
   * @param {string} params.type - Filter by type (optional)
   * @param {string} params.key - Filter by key (optional)
   */
  async recall({ userId, type = null, key = null }) {
    return new Promise((resolve, reject) => {
      let query = `SELECT * FROM memories WHERE userId = ?`;
      const params = [userId];

      if (type) {
        query += ` AND type = ?`;
        params.push(type);
      }
      if (key) {
        query += ` AND key = ?`;
        params.push(key);
      }

      query += ` ORDER BY createdAt DESC`;

      this.db.all(query, params, (err, rows) => {
        if (err) reject(err);
        else resolve(rows.map(row => ({
          ...row,
          metadata: JSON.parse(row.metadata || '{}')
        })));
      });
    });
  }

  /**
   * Semantic search memory
   * @param {Object} params - Search parameters
   * @param {string} params.userId - User identifier
   * @param {string} params.query - Search query
   * @param {number} params.limit - Max results (default 5)
   */
  async semanticSearch({ userId, query, limit = 5 }) {
    // Simple keyword search for now
    // In production, implement vector embeddings
    return new Promise((resolve, reject) => {
      this.db.all(
        `SELECT * FROM memories 
         WHERE userId = ? AND (content LIKE ? OR key LIKE ? OR (metadata LIKE ?))
         ORDER BY createdAt DESC LIMIT ?`,
        [`%${userId}%`, `%${query}%`, `%${query}%`, `%${query}%`, limit],
        (err, rows) => {
          if (err) reject(err);
          else resolve(rows);
        }
      );
    });
  }

  /**
   * Delete a memory
   * @param {Object} params - Delete parameters
   * @param {string} params.userId - User identifier
   * @param {string} params.type - Memory type
   * @param {string} params.key - Memory key (optional)
   */
  async forget({ userId, type, key = null }) {
    return new Promise((resolve, reject) => {
      let query = `DELETE FROM memories WHERE userId = ? AND type = ?`;
      const params = [userId, type];

      if (key) {
        query += ` AND key = ?`;
        params.push(key);
      }

      this.db.run(query, params, function(err) {
        if (err) reject(err);
        else resolve({ deleted: this.changes });
      });
    });
  }

  /**
   * Get all user preferences
   */
  async getPreferences(userId) {
    const prefs = await this.recall({ userId, type: 'preference' });
    const result = {};
    prefs.forEach(pref => {
      if (pref.key) result[pref.key] = pref.content;
    });
    return result;
  }

  /**
   * Get all user trading rules
   */
  async getRules(userId) {
    return await this.recall({ userId, type: 'rule' });
  }

  /**
   * Log daily trading activity
   */
  async logDaily({ userId, date, trades, pnl, notes = '' }) {
    return await this.remember({
      userId,
      type: 'note',
      key: `journal_${date.toISOString().split('T')[0]}`,
      content: `Trades: ${trades}, P&L: $${pnl}, Notes: ${notes}`,
      metadata: { type: 'journal', trades, pnl, date: date.toISOString() }
    });
  }

  close() {
    this.db.close();
  }
}

export default MemoryManager;
