package pruning

import (
	"strings"
	"testing"
)

func TestDiffEngine(t *testing.T) {
	de := NewDiffEngine()

	key := "test-session"
	content1 := []byte("Welcome\nButton A\nInput B")
	content2 := []byte("Welcome\nButton A\nInput B")
	content3 := []byte("Welcome\nButton A\nInput C\nInput D")

	diff1, changed := de.ComputeDiff(key, content1)
	if !changed {
		t.Error("expected first call to result in changed state")
	}
	if string(diff1) != string(content1) {
		t.Errorf("expected original content, got %s", string(diff1))
	}

	_, changed = de.ComputeDiff(key, content2)
	if changed {
		t.Error("expected second call to be unchanged")
	}

	diff3, changed := de.ComputeDiff(key, content3)
	if !changed {
		t.Error("expected third call to detect change")
	}

	diffStr := string(diff3)
	if !strings.Contains(diffStr, "+ Input C") {
		t.Errorf("expected added line 'Input C', got %s", diffStr)
	}
	if !strings.Contains(diffStr, "+ Input D") {
		t.Errorf("expected added line 'Input D', got %s", diffStr)
	}
	if !strings.Contains(diffStr, "  Welcome") {
		t.Errorf("expected unchanged line 'Welcome', got %s", diffStr)
	}
}
