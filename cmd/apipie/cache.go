package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

// CacheEntry represents a cached display name for a model
type CacheEntry struct {
	ModelID         string
	DescriptionHash string
	DisplayName     string
	CreatedAt       time.Time
}

// Cache manages the SQLite database for caching LLM-generated display names
type Cache struct {
	db *sql.DB
}

// NewCache creates a new cache instance and initializes the database
func NewCache(dbPath string) (*Cache, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	cache := &Cache{db: db}
	if err := cache.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return cache, nil
}

// Close closes the database connection
func (c *Cache) Close() error {
	return c.db.Close()
}

// initSchema creates the cache table if it doesn't exist
func (c *Cache) initSchema() error {
	query := `
	CREATE TABLE IF NOT EXISTS display_name_cache (
		model_id TEXT NOT NULL,
		description_hash TEXT NOT NULL,
		display_name TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		PRIMARY KEY (model_id, description_hash)
	);
	
	CREATE INDEX IF NOT EXISTS idx_model_id ON display_name_cache(model_id);
	CREATE INDEX IF NOT EXISTS idx_created_at ON display_name_cache(created_at);
	
	CREATE TABLE IF NOT EXISTS reasoning_effort_cache (
		description_hash TEXT NOT NULL PRIMARY KEY,
		has_reasoning_effort BOOLEAN NOT NULL,
		created_at DATETIME NOT NULL
	);
	
	CREATE INDEX IF NOT EXISTS idx_reasoning_created_at ON reasoning_effort_cache(created_at);
	`

	_, err := c.db.Exec(query)
	return err
}



// hashDescription creates a SHA256 hash of the model description (legacy function)
// This allows us to detect when descriptions change and invalidate cache
func hashDescription(description string) string {
	hash := sha256.Sum256([]byte(description))
	return fmt.Sprintf("%x", hash)
}

// Get retrieves a cached display name for a model
// Returns empty string if not found or metadata has changed
func (c *Cache) Get(model Model) string {
	metadataHash := hashModelMetadata(model)
	
	var displayName string
	query := `SELECT display_name FROM display_name_cache 
			  WHERE model_id = ? AND description_hash = ?`
	
	err := c.db.QueryRow(query, model.ID, metadataHash).Scan(&displayName)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("Cache get error for model %s: %v", model.ID, err)
		}
		return ""
	}
	
	return displayName
}

// Set stores a display name in the cache
func (c *Cache) Set(model Model, displayName string) error {
	metadataHash := hashModelMetadata(model)
	
	query := `INSERT OR REPLACE INTO display_name_cache 
			  (model_id, description_hash, display_name, created_at) 
			  VALUES (?, ?, ?, ?)`
	
	_, err := c.db.Exec(query, model.ID, metadataHash, displayName, time.Now())
	if err != nil {
		return fmt.Errorf("failed to cache display name for model %s: %w", model.ID, err)
	}
	
	return nil
}

// GetStats returns cache statistics
func (c *Cache) GetStats() (int, error) {
	var count int
	err := c.db.QueryRow("SELECT COUNT(*) FROM display_name_cache").Scan(&count)
	return count, err
}

// CleanOldEntries removes cache entries older than the specified duration
// This helps keep the cache size manageable
func (c *Cache) CleanOldEntries(maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge)
	
	// Clean display name cache
	query := `DELETE FROM display_name_cache WHERE created_at < ?`
	result, err := c.db.Exec(query, cutoff)
	if err != nil {
		return fmt.Errorf("failed to clean old display name entries: %w", err)
	}
	
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		log.Printf("Cleaned %d old display name cache entries", rowsAffected)
	}
	
	// Clean reasoning effort cache
	query = `DELETE FROM reasoning_effort_cache WHERE created_at < ?`
	result, err = c.db.Exec(query, cutoff)
	if err != nil {
		return fmt.Errorf("failed to clean old reasoning effort entries: %w", err)
	}
	
	rowsAffected, _ = result.RowsAffected()
	if rowsAffected > 0 {
		log.Printf("Cleaned %d old reasoning effort cache entries", rowsAffected)
	}
	
	return nil
}

// GetReasoningEffort retrieves cached reasoning effort analysis for a description
func (c *Cache) GetReasoningEffort(description string) (bool, bool) {
	if description == "" {
		return false, false
	}
	
	hash := hashDescription(description)
	
	var hasEffort bool
	err := c.db.QueryRow(
		"SELECT has_reasoning_effort FROM reasoning_effort_cache WHERE description_hash = ?",
		hash,
	).Scan(&hasEffort)
	
	if err != nil {
		return false, false // Cache miss
	}
	
	return hasEffort, true // Cache hit
}

// SetReasoningEffort stores reasoning effort analysis result in cache
func (c *Cache) SetReasoningEffort(description string, hasEffort bool) error {
	if description == "" {
		return nil
	}
	
	hash := hashDescription(description)
	
	_, err := c.db.Exec(
		"INSERT OR REPLACE INTO reasoning_effort_cache (description_hash, has_reasoning_effort, created_at) VALUES (?, ?, ?)",
		hash, hasEffort, time.Now(),
	)
	
	if err != nil {
		return fmt.Errorf("failed to cache reasoning effort: %w", err)
	}
	
	return nil
}