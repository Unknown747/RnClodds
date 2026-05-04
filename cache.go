package main

import (
	"sync"
	"time"
)

type cacheEntry struct {
	value     interface{}
	expiresAt time.Time
}

// Cache is a simple thread-safe in-memory TTL cache.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	stopCh  chan struct{}
}

func NewCache() *Cache {
	c := &Cache{
		entries: make(map[string]cacheEntry),
		stopCh:  make(chan struct{}),
	}
	go c.evict()
	return c
}

func (c *Cache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{value: value, expiresAt: time.Now().Add(ttl)}
}

func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.value, true
}

func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Stop halts the background eviction goroutine.
func (c *Cache) Stop() {
	select {
	case <-c.stopCh:
		// already stopped
	default:
		close(c.stopCh)
	}
}

// evict removes expired entries every 60 seconds until Stop() is called.
func (c *Cache) evict() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			c.mu.Lock()
			for k, e := range c.entries {
				if now.After(e.expiresAt) {
					delete(c.entries, k)
				}
			}
			c.mu.Unlock()
		case <-c.stopCh:
			return
		}
	}
}
