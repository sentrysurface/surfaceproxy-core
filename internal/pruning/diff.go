package pruning

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
)

type DiffEngine struct {
	mu     sync.RWMutex
	states map[string]string // maps target URL or page key to SHA256 content hash
	bodies map[string]string // maps target URL or page key to full content text for diffing
}

func NewDiffEngine() *DiffEngine {
	return &DiffEngine{
		states: make(map[string]string),
		bodies: make(map[string]string),
	}
}

// ComputeDiff checks if content has changed. If it has, it computes a structural line diff and updates the store.
// If it hasn't, it returns a nil byte slice and false.
func (de *DiffEngine) ComputeDiff(pageKey string, newContent []byte) ([]byte, bool) {
	de.mu.Lock()
	defer de.mu.Unlock()

	hash := sha256.Sum256(newContent)
	hashStr := hex.EncodeToString(hash[:])

	oldHash, exists := de.states[pageKey]
	if exists && oldHash == hashStr {
		return nil, false
	}

	newBody := string(newContent)
	oldBody := de.bodies[pageKey]

	de.states[pageKey] = hashStr
	de.bodies[pageKey] = newBody

	if !exists {
		// First time seeing this page, return the full content
		return newContent, true
	}

	// Compute a simple structural line-diff
	diff := de.lineDiff(oldBody, newBody)
	return []byte(diff), true
}

func (de *DiffEngine) Clear(pageKey string) {
	de.mu.Lock()
	defer de.mu.Unlock()
	delete(de.states, pageKey)
	delete(de.bodies, pageKey)
}

func (de *DiffEngine) lineDiff(oldStr, newStr string) string {
	oldLines := strings.Split(oldStr, "\n")
	newLines := strings.Split(newStr, "\n")

	oldMap := make(map[string]bool)
	for _, line := range oldLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			oldMap[trimmed] = true
		}
	}

	buf := GetBuffer()
	defer PutBuffer(buf)

	for _, line := range newLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !oldMap[trimmed] {
			buf.WriteString("+ ")
			buf.WriteString(line)
			buf.WriteString("\n")
		} else {
			buf.WriteString("  ")
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	}

	return buf.String()
}
