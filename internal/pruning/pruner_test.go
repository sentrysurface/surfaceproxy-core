package pruning

import (
	"strings"
	"testing"

	"github.com/sentrysurface/surface-proxy/internal/config"
)

func TestPruner(t *testing.T) {
	cfg := config.PruningConfig{
		OutputFormat: "markdown",
		StripTags:    []string{"script", "style"},
	}

	p := NewPruner(cfg)

	htmlContent := `
		<html>
			<head>
				<script>console.log("hello")</script>
				<style>body { color: red; }</style>
			</head>
			<body>
				<h1>Welcome to SurfaceProxy</h1>
				<p>This is a low-latency proxy engine.</p>
				<button id="submit-btn" class="btn">Click Me</button>
			</body>
		</html>
	`

	pruned, err := p.Prune([]byte(htmlContent))
	if err != nil {
		t.Fatal(err)
	}

	resStr := string(pruned)

	if !strings.Contains(resStr, "Welcome to SurfaceProxy") {
		t.Errorf("expected Welcome to SurfaceProxy in pruned content, got: %s", resStr)
	}

	if strings.Contains(resStr, "console.log") {
		t.Error("expected script contents to be stripped, but found console.log")
	}

	if !strings.Contains(resStr, "[button id=\"submit-btn\"]") {
		t.Errorf("expected interactive button tag format, got: %s", resStr)
	}
}
