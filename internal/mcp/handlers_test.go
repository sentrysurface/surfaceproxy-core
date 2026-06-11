package mcp

import (
	"testing"
)

func TestToolManifest(t *testing.T) {
	manifest := ToolManifest()
	expectedTools := []string{
		"browse", "getDOM", "click", "type", "screenshot",
		"openBrowserPage", "navigatePage", "readPage", "screenshotPage",
		"clickElement", "hoverElement", "dragElement", "typeInPage",
		"handleDialog", "runPlaywrightCode",
	}

	found := make(map[string]bool)
	for _, tool := range manifest {
		found[tool.Name] = true
	}

	for _, expected := range expectedTools {
		if !found[expected] {
			t.Errorf("expected tool %s not found in manifest", expected)
		}
	}
}
