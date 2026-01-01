package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Cache is an interface for caching database query results
type Cache interface {
	// Get retrieves a value from the cache
	Get(key string) ([]byte, bool)
	// Set stores a value in the cache with an optional TTL
	Set(key string, value []byte, ttl time.Duration)
	// SetWithTags stores a value in the cache with tags for targeted invalidation
	SetWithTags(key string, value []byte, ttl time.Duration, tags []string)
	// Delete removes a value from the cache
	Delete(key string)
	// DeleteByTag removes all values with a specific tag
	DeleteByTag(tag string)
	// Clear removes all values from the cache
	Clear()
}

// cacheEntry represents a cached value with expiration and tags
type cacheEntry struct {
	value     []byte
	expiresAt time.Time
	tags      []string // tags for targeted invalidation (e.g., table names)
}

// MemoryCache is an in-memory cache implementation with TTL support
type MemoryCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	// tagIndex maps tags to cache keys for efficient tag-based deletion
	tagIndex map[string]map[string]struct{}
	// Default TTL for entries without explicit TTL
	defaultTTL time.Duration
	// Cleanup interval for expired entries
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
}

// NewMemoryCache creates a new in-memory cache
// defaultTTL: default time-to-live for cache entries (0 = no expiration)
// cleanupInterval: how often to clean up expired entries (0 = no cleanup)
func NewMemoryCache(defaultTTL, cleanupInterval time.Duration) *MemoryCache {
	cache := &MemoryCache{
		entries:         make(map[string]*cacheEntry),
		tagIndex:        make(map[string]map[string]struct{}),
		defaultTTL:      defaultTTL,
		cleanupInterval: cleanupInterval,
		stopCleanup:     make(chan struct{}),
	}

	// Start cleanup goroutine if cleanup interval is set
	if cleanupInterval > 0 {
		go cache.cleanup()
	}

	return cache
}

// Get retrieves a value from the cache
func (c *MemoryCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[key]
	if !exists {
		return nil, false
	}

	// Check if entry has expired
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		// Entry expired, but don't delete here (cleanup goroutine will handle it)
		return nil, false
	}

	return entry.value, true
}

// Set stores a value in the cache with an optional TTL (no tags)
func (c *MemoryCache) Set(key string, value []byte, ttl time.Duration) {
	c.SetWithTags(key, value, ttl, nil)
}

// SetWithTags stores a value in the cache with tags for targeted invalidation
func (c *MemoryCache) SetWithTags(key string, value []byte, ttl time.Duration, tags []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove old entry's tags from index if it exists
	if oldEntry, exists := c.entries[key]; exists {
		for _, tag := range oldEntry.tags {
			if keys, ok := c.tagIndex[tag]; ok {
				delete(keys, key)
				if len(keys) == 0 {
					delete(c.tagIndex, tag)
				}
			}
		}
	}

	entry := &cacheEntry{
		value: value,
		tags:  tags,
	}

	// Use provided TTL, or default TTL, or no expiration
	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	} else if c.defaultTTL > 0 {
		entry.expiresAt = time.Now().Add(c.defaultTTL)
	}

	c.entries[key] = entry

	// Add entry to tag index
	for _, tag := range tags {
		if c.tagIndex[tag] == nil {
			c.tagIndex[tag] = make(map[string]struct{})
		}
		c.tagIndex[tag][key] = struct{}{}
	}
}

// Delete removes a value from the cache
func (c *MemoryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove from tag index
	if entry, exists := c.entries[key]; exists {
		for _, tag := range entry.tags {
			if keys, ok := c.tagIndex[tag]; ok {
				delete(keys, key)
				if len(keys) == 0 {
					delete(c.tagIndex, tag)
				}
			}
		}
	}

	delete(c.entries, key)
}

// DeleteByTag removes all cache entries with the given tag
func (c *MemoryCache) DeleteByTag(tag string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	keys, exists := c.tagIndex[tag]
	if !exists {
		return
	}

	// Delete all entries with this tag
	for key := range keys {
		if entry, ok := c.entries[key]; ok {
			// Remove this entry from all its tags' indexes
			for _, t := range entry.tags {
				if t != tag {
					if tagKeys, ok := c.tagIndex[t]; ok {
						delete(tagKeys, key)
						if len(tagKeys) == 0 {
							delete(c.tagIndex, t)
						}
					}
				}
			}
			delete(c.entries, key)
		}
	}

	// Remove the tag from the index
	delete(c.tagIndex, tag)
}

// Clear removes all values from the cache
func (c *MemoryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*cacheEntry)
	c.tagIndex = make(map[string]map[string]struct{})
}

// Stop stops the cleanup goroutine
func (c *MemoryCache) Stop() {
	close(c.stopCleanup)
}

// cleanup periodically removes expired entries
func (c *MemoryCache) cleanup() {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			for key, entry := range c.entries {
				if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
					// Remove from tag index
					for _, tag := range entry.tags {
						if keys, ok := c.tagIndex[tag]; ok {
							delete(keys, key)
							if len(keys) == 0 {
								delete(c.tagIndex, tag)
							}
						}
					}
					delete(c.entries, key)
				}
			}
			c.mu.Unlock()
		case <-c.stopCleanup:
			return
		}
	}
}

// generateCacheKey creates a cache key from query and arguments
func generateCacheKey(query string, args ...interface{}) string {
	// Normalize query
	normalizedQuery := normalizeQuery(query)

	// Serialize arguments to JSON for consistent key generation
	argsJSON, err := json.Marshal(args)
	if err != nil {
		// Fallback: use fmt.Sprintf if JSON marshaling fails
		argsStr := fmt.Sprintf("%v", args)
		combined := normalizedQuery + "|" + argsStr
		hash := sha256.Sum256([]byte(combined))
		return hex.EncodeToString(hash[:])
	}

	// Combine query and args, then hash
	combined := normalizedQuery + "|" + string(argsJSON)
	hash := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(hash[:])
}

// isReadOnlyQuery checks if a query is read-only (SELECT)
func isReadOnlyQuery(query string) bool {
	// Simple check: if query starts with SELECT (case-insensitive, ignoring whitespace)
	query = trimWhitespace(query)
	return len(query) >= 6 && (query[0:6] == "SELECT" || query[0:6] == "select" || query[0:6] == "Select")
}

// trimWhitespace removes leading/trailing whitespace
func trimWhitespace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// extractTablesFromSelect extracts table names from a SELECT query
// Returns a slice of table names found in FROM and JOIN clauses
func extractTablesFromSelect(query string) []string {
	tables := make(map[string]struct{})
	upper := toUpperASCII(query)

	// Find tables after FROM
	fromIdx := findKeyword(upper, "FROM")
	if fromIdx != -1 {
		extractTableNames(query, fromIdx+4, tables)
	}

	// Find tables after JOIN (handles INNER JOIN, LEFT JOIN, etc.)
	idx := 0
	for {
		joinIdx := findKeywordFrom(upper, "JOIN", idx)
		if joinIdx == -1 {
			break
		}
		extractTableNames(query, joinIdx+4, tables)
		idx = joinIdx + 4
	}

	result := make([]string, 0, len(tables))
	for table := range tables {
		result = append(result, table)
	}
	return result
}

// extractTableFromWrite extracts the table name from INSERT, UPDATE, or DELETE query
func extractTableFromWrite(query string) string {
	upper := toUpperASCII(query)
	trimmed := trimWhitespace(upper)

	// INSERT INTO table_name
	if len(trimmed) >= 6 && trimmed[0:6] == "INSERT" {
		intoIdx := findKeyword(upper, "INTO")
		if intoIdx != -1 {
			return extractSingleTableName(query, intoIdx+4)
		}
	}

	// UPDATE table_name SET
	if len(trimmed) >= 6 && trimmed[0:6] == "UPDATE" {
		return extractSingleTableName(query, 6)
	}

	// DELETE FROM table_name
	if len(trimmed) >= 6 && trimmed[0:6] == "DELETE" {
		fromIdx := findKeyword(upper, "FROM")
		if fromIdx != -1 {
			return extractSingleTableName(query, fromIdx+4)
		}
	}

	return ""
}

// extractTableNames extracts table names starting from the given position
// and adds them to the tables map
func extractTableNames(query string, startPos int, tables map[string]struct{}) {
	upper := toUpperASCII(query)
	pos := startPos

	// Skip whitespace
	for pos < len(query) && isWhitespace(query[pos]) {
		pos++
	}

	for pos < len(query) {
		// Extract table name (stops at whitespace, comma, or keywords)
		tableStart := pos
		for pos < len(query) && !isWhitespace(query[pos]) && query[pos] != ',' && query[pos] != '(' && query[pos] != ')' {
			pos++
		}

		if pos > tableStart {
			tableName := toLowerASCII(query[tableStart:pos])
			// Skip SQL keywords and aliases
			if !isSQLKeyword(tableName) && tableName != "" {
				tables[tableName] = struct{}{}
			}
		}

		// Skip whitespace
		for pos < len(query) && isWhitespace(query[pos]) {
			pos++
		}

		// Check for alias (AS keyword or direct identifier)
		if pos < len(query) {
			// Check for AS keyword
			if pos+2 < len(query) && (upper[pos:pos+2] == "AS" || upper[pos:pos+2] == "as") {
				pos += 2
				// Skip whitespace after AS
				for pos < len(query) && isWhitespace(query[pos]) {
					pos++
				}
				// Skip the alias
				for pos < len(query) && !isWhitespace(query[pos]) && query[pos] != ',' && query[pos] != ')' {
					pos++
				}
			} else {
				// Check if next token is an identifier (alias without AS)
				nextTokenStart := pos
				for pos < len(query) && !isWhitespace(query[pos]) && query[pos] != ',' && query[pos] != ')' {
					pos++
				}
				if pos > nextTokenStart {
					nextToken := toLowerASCII(query[nextTokenStart:pos])
					// If it's a keyword, we've moved past the table section
					if isSQLKeyword(nextToken) {
						break
					}
					// Otherwise it's an alias, continue
				}
			}
		}

		// Skip whitespace
		for pos < len(query) && isWhitespace(query[pos]) {
			pos++
		}

		// Check for comma (another table follows)
		if pos < len(query) && query[pos] == ',' {
			pos++
			// Skip whitespace after comma
			for pos < len(query) && isWhitespace(query[pos]) {
				pos++
			}
			continue
		}

		// No comma, check if we hit a keyword that ends the FROM/JOIN clause
		if pos+4 <= len(query) {
			nextWord := toLowerASCII(trimWhitespace(query[pos:min(pos+10, len(query))]))
			if len(nextWord) >= 4 && isSQLKeyword(nextWord[:min(len(nextWord), 10)]) {
				break
			}
		}

		break
	}
}

// extractSingleTableName extracts a single table name starting from the given position
func extractSingleTableName(query string, startPos int) string {
	pos := startPos

	// Skip whitespace
	for pos < len(query) && isWhitespace(query[pos]) {
		pos++
	}

	// Extract table name
	tableStart := pos
	for pos < len(query) && !isWhitespace(query[pos]) && query[pos] != '(' && query[pos] != ')' {
		pos++
	}

	if pos > tableStart {
		return toLowerASCII(query[tableStart:pos])
	}
	return ""
}

// findKeyword finds the position of a keyword in a string (case-insensitive)
func findKeyword(upper string, keyword string) int {
	return findKeywordFrom(upper, keyword, 0)
}

// findKeywordFrom finds the position of a keyword starting from a given position
func findKeywordFrom(upper string, keyword string, from int) int {
	keywordLen := len(keyword)
	for i := from; i <= len(upper)-keywordLen; i++ {
		if upper[i:i+keywordLen] == keyword {
			// Check that it's a word boundary (not part of a larger word)
			if (i == 0 || !isAlphaNum(upper[i-1])) && (i+keywordLen >= len(upper) || !isAlphaNum(upper[i+keywordLen])) {
				return i
			}
		}
	}
	return -1
}

// isSQLKeyword checks if a word is a SQL keyword
func isSQLKeyword(word string) bool {
	keywords := map[string]struct{}{
		"select": {}, "from": {}, "where": {}, "and": {}, "or": {},
		"join": {}, "inner": {}, "left": {}, "right": {}, "outer": {}, "cross": {},
		"on": {}, "as": {}, "order": {}, "by": {}, "group": {}, "having": {},
		"limit": {}, "offset": {}, "union": {}, "all": {}, "distinct": {},
		"insert": {}, "into": {}, "values": {}, "update": {}, "set": {},
		"delete": {}, "create": {}, "drop": {}, "alter": {}, "table": {},
		"index": {}, "using": {}, "with": {}, "case": {}, "when": {}, "then": {},
		"else": {}, "end": {}, "null": {}, "not": {}, "in": {}, "exists": {},
		"between": {}, "like": {}, "is": {}, "true": {}, "false": {},
		"returning": {}, "for": {},
	}
	_, exists := keywords[word]
	return exists
}

// isWhitespace checks if a character is whitespace
func isWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// isAlphaNum checks if a character is alphanumeric or underscore
func isAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// toUpperASCII converts a string to uppercase (ASCII only)
func toUpperASCII(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		} else {
			b[i] = c
		}
	}
	return string(b)
}

// toLowerASCII converts a string to lowercase (ASCII only)
func toLowerASCII(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		} else {
			b[i] = c
		}
	}
	return string(b)
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
