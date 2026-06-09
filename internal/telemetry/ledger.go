// Package telemetry provides a thread-safe in-memory session ledger that tracks
// per-session token usage, DOM reduction metrics, and dollar savings.
package telemetry

import (
	"sync"
	"sync/atomic"
	"time"
)

const bytesPerToken = 4 // conservative 4-byte-per-token approximation

// Ledger is the global, thread-safe in-memory store for all session telemetry.
// It is designed for concurrent writes from many goroutines with minimal contention.
type Ledger struct {
	mu       sync.RWMutex
	sessions map[string]*sessionEntry

	// Global counters updated atomically for lock-free hot path reads
	totalRawBytes    atomic.Int64
	totalPrunedBytes atomic.Int64
	totalPruneCount  atomic.Int64
	totalSessions    atomic.Int64
	activeSessions   atomic.Int64
}

// NewLedger creates a new empty telemetry ledger.
func NewLedger() *Ledger {
	return &Ledger{
		sessions: make(map[string]*sessionEntry),
	}
}

// OpenSession registers a new session in the ledger.
func (l *Ledger) OpenSession(id, url string) {
	entry := &sessionEntry{
		record: SessionRecord{
			ID:        id,
			StartedAt: time.Now(),
			URL:       url,
		},
	}
	l.mu.Lock()
	l.sessions[id] = entry
	l.mu.Unlock()
	l.totalSessions.Add(1)
	l.activeSessions.Add(1)
}

// RecordPrune records the outcome of a single Prune() call for a session.
// rawBytes is the input HTML size; prunedBytes is the output size.
func (l *Ledger) RecordPrune(sessionID string, rawBytes, prunedBytes int) {
	rawTokens := int64(rawBytes) / bytesPerToken
	prunedTokens := int64(prunedBytes) / bytesPerToken

	// Update global atomic counters on the hot path — no lock needed
	l.totalRawBytes.Add(int64(rawBytes))
	l.totalPrunedBytes.Add(int64(prunedBytes))
	l.totalPruneCount.Add(1)

	// Update session-specific record under a per-session lock
	l.mu.RLock()
	entry, ok := l.sessions[sessionID]
	l.mu.RUnlock()
	if !ok {
		return
	}

	entry.mu.Lock()
	entry.record.RawBytes += int64(rawBytes)
	entry.record.PrunedBytes += int64(prunedBytes)
	entry.record.RawTokens += rawTokens
	entry.record.PrunedTokens += prunedTokens
	entry.record.PruneCount++
	entry.mu.Unlock()
}

// UpdateSessionURL updates the current URL for a session (called after navigation).
func (l *Ledger) UpdateSessionURL(sessionID, url string) {
	l.mu.RLock()
	entry, ok := l.sessions[sessionID]
	l.mu.RUnlock()
	if !ok {
		return
	}
	entry.mu.Lock()
	entry.record.URL = url
	entry.mu.Unlock()
}

// CloseSession marks a session as ended and removes it from the active set.
// Returns the final SessionRecord for summary printing.
func (l *Ledger) CloseSession(sessionID string) (SessionRecord, bool) {
	l.mu.RLock()
	entry, ok := l.sessions[sessionID]
	l.mu.RUnlock()
	if !ok {
		return SessionRecord{}, false
	}

	now := time.Now()
	entry.mu.Lock()
	entry.record.EndedAt = &now
	snap := entry.record.Snapshot()
	entry.mu.Unlock()

	l.activeSessions.Add(-1)
	return snap, true
}

// GetSession returns a snapshot of a single session record.
func (l *Ledger) GetSession(sessionID string) (SessionRecord, bool) {
	l.mu.RLock()
	entry, ok := l.sessions[sessionID]
	l.mu.RUnlock()
	if !ok {
		return SessionRecord{}, false
	}
	entry.mu.RLock()
	defer entry.mu.RUnlock()
	return entry.record.Snapshot(), true
}

// AllSessions returns snapshots of all known sessions (active and closed).
func (l *Ledger) AllSessions() []SessionRecord {
	l.mu.RLock()
	defer l.mu.RUnlock()
	records := make([]SessionRecord, 0, len(l.sessions))
	for _, entry := range l.sessions {
		entry.mu.RLock()
		records = append(records, entry.record.Snapshot())
		entry.mu.RUnlock()
	}
	return records
}

// GlobalStats returns a lightweight aggregate view of the ledger.
type GlobalStats struct {
	TotalSessions   int64
	ActiveSessions  int64
	TotalRawBytes   int64
	TotalPrunedBytes int64
	TotalPruneOps   int64
	TotalRawTokens  int64
	TotalPrunedTokens int64
	ReductionPct    float64
}

func (l *Ledger) GlobalStats() GlobalStats {
	raw := l.totalRawBytes.Load()
	pruned := l.totalPrunedBytes.Load()

	rawTokens := raw / bytesPerToken
	prunedTokens := pruned / bytesPerToken

	var reductionPct float64
	if rawTokens > 0 {
		reductionPct = float64(rawTokens-prunedTokens) / float64(rawTokens) * 100
	}

	return GlobalStats{
		TotalSessions:     l.totalSessions.Load(),
		ActiveSessions:    l.activeSessions.Load(),
		TotalRawBytes:     raw,
		TotalPrunedBytes:  pruned,
		TotalPruneOps:     l.totalPruneCount.Load(),
		TotalRawTokens:    rawTokens,
		TotalPrunedTokens: prunedTokens,
		ReductionPct:      reductionPct,
	}
}
