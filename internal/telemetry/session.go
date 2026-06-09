package telemetry

import (
	"sync"
	"time"
)

// SessionRecord holds the raw telemetry for a single proxy or MCP session.
type SessionRecord struct {
	ID          string
	StartedAt   time.Time
	EndedAt     *time.Time
	URL         string // last navigated URL

	// Byte accounting — populated by the pruner on every Prune() call
	RawBytes    int64 // total HTML bytes received
	PrunedBytes int64 // total pruned output bytes delivered to agent
	PruneCount  int64 // number of prune operations performed

	// Token estimates (using the 4-bytes-per-token approximation)
	RawTokens    int64
	PrunedTokens int64
}

// TokensReduced returns how many tokens were saved for this session.
func (r *SessionRecord) TokensReduced() int64 {
	return r.RawTokens - r.PrunedTokens
}

// ReductionPct returns the percentage token reduction (0–100).
func (r *SessionRecord) ReductionPct() float64 {
	if r.RawTokens == 0 {
		return 0
	}
	return float64(r.TokensReduced()) / float64(r.RawTokens) * 100
}

// DurationSeconds returns the session duration in seconds, or elapsed time if still running.
func (r *SessionRecord) DurationSeconds() float64 {
	end := time.Now()
	if r.EndedAt != nil {
		end = *r.EndedAt
	}
	return end.Sub(r.StartedAt).Seconds()
}

// Snapshot returns a copy of the record safe to read without holding a lock.
func (r *SessionRecord) Snapshot() SessionRecord {
	return *r
}

// sessionEntry wraps a SessionRecord with its own lock.
type sessionEntry struct {
	mu     sync.RWMutex
	record SessionRecord
}
