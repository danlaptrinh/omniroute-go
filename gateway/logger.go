package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

const DefaultMaxLogEntries = 300

// GatewayLogger manages an in-memory ring buffer of request and error logs.
type GatewayLogger struct {
	mu         sync.RWMutex
	maxEntries int
	entries    []GatewayLogEntry
}

var GlobalLogger = NewGatewayLogger(DefaultMaxLogEntries)

// NewGatewayLogger initializes a new GatewayLogger with the specified capacity.
func NewGatewayLogger(maxEntries int) *GatewayLogger {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxLogEntries
	}
	return &GatewayLogger{
		maxEntries: maxEntries,
		entries:    make([]GatewayLogEntry, 0, maxEntries),
	}
}

// RecordLogEntry adds an entry to the ring buffer.
func (l *GatewayLogger) RecordLogEntry(entry GatewayLogEntry) {
	if entry.ID == "" {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		entry.ID = "log-" + hex.EncodeToString(b)
	}
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if !entry.IsError && (entry.StatusCode >= 400 || entry.ErrorMessage != "") {
		entry.IsError = true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.entries) >= l.maxEntries {
		copy(l.entries, l.entries[1:])
		l.entries[len(l.entries)-1] = entry
	} else {
		l.entries = append(l.entries, entry)
	}
}

// GetRecentLogs returns up to limit logs in reverse chronological order with optional filters.
func (l *GatewayLogger) GetRecentLogs(limit int, providerFilter string, errorsOnly bool) []GatewayLogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	provFilter := strings.ToLower(strings.TrimSpace(providerFilter))
	result := make([]GatewayLogEntry, 0)

	// Traverse in reverse chronological order (newest first)
	for i := len(l.entries) - 1; i >= 0; i-- {
		entry := l.entries[i]
		isErr := entry.IsError || entry.StatusCode >= 400 || entry.ErrorMessage != ""
		if errorsOnly && !isErr {
			continue
		}
		if provFilter != "" && provFilter != "all" && !strings.EqualFold(string(entry.Provider), provFilter) {
			continue
		}
		result = append(result, entry)
		if limit > 0 && len(result) >= limit {
			break
		}
	}

	return result
}

// ClearLogs empties the log buffer.
func (l *GatewayLogger) ClearLogs() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = make([]GatewayLogEntry, 0, l.maxEntries)
}

// Len returns the current number of log entries in the ring buffer.
func (l *GatewayLogger) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.entries)
}

// RecordLogEntry records a log entry to the GlobalLogger singleton.
func RecordLogEntry(entry GatewayLogEntry) {
	GlobalLogger.RecordLogEntry(entry)
}

// GetRecentLogs retrieves logs from the GlobalLogger singleton.
func GetRecentLogs(limit int, providerFilter string, errorsOnly bool) []GatewayLogEntry {
	return GlobalLogger.GetRecentLogs(limit, providerFilter, errorsOnly)
}

// ClearLogs resets the GlobalLogger singleton.
func ClearLogs() {
	GlobalLogger.ClearLogs()
}
