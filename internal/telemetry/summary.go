package telemetry

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorCyan   = "\033[36m"
	colorYellow = "\033[33m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

// PrintSessionSummary prints a formatted terminal ROI summary for a completed session.
// Output goes to stderr so it doesn't corrupt any stdio MCP stream.
func PrintSessionSummary(r SessionRecord, pricing Pricing, w io.Writer) {
	if w == nil {
		w = os.Stderr
	}

	if pricing.InputCostPer1K == 0 {
		pricing = DefaultPricing
	}

	saved := DollarsSaved(r.TokensReduced(), pricing)
	duration := r.DurationSeconds()

	divider := strings.Repeat("─", 56)

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s%s  SurfaceProxy Session Summary  %s%s\n", colorBold, colorCyan, colorReset, colorDim)
	fmt.Fprintln(w, divider+colorReset)

	fmt.Fprintf(w, "  %sSession ID:      %s%s%s\n", colorDim, colorReset, r.ID[:8]+"...", colorReset)
	fmt.Fprintf(w, "  %sURL:             %s%s%s\n", colorDim, colorReset, truncate(r.URL, 45), colorReset)
	fmt.Fprintf(w, "  %sDuration:        %s%.1fs%s\n", colorDim, colorReset, duration, colorReset)
	fmt.Fprintf(w, "  %sPrune ops:       %s%d%s\n", colorDim, colorReset, r.PruneCount, colorReset)
	fmt.Fprintln(w, divider)

	fmt.Fprintf(w, "  %sRaw HTML bytes:  %s%s%s\n", colorDim, colorReset, formatBytes(r.RawBytes), colorReset)
	fmt.Fprintf(w, "  %sPruned bytes:    %s%s%s\n", colorDim, colorReset, formatBytes(r.PrunedBytes), colorReset)
	fmt.Fprintf(w, "  %sRaw tokens:      %s%s%s\n", colorDim, colorReset, formatCount(r.RawTokens), colorReset)
	fmt.Fprintf(w, "  %sPruned tokens:   %s%s%s\n", colorDim, colorReset, formatCount(r.PrunedTokens), colorReset)
	fmt.Fprintln(w, divider)

	fmt.Fprintf(w, "  %s%sContext reduction:%s %s%.1f%%%s\n",
		colorBold, colorGreen, colorReset, colorGreen, r.ReductionPct(), colorReset)
	fmt.Fprintf(w, "  %s%sTokens saved:    %s %s%s%s\n",
		colorBold, colorGreen, colorReset, colorGreen, formatCount(r.TokensReduced()), colorReset)
	fmt.Fprintf(w, "  %s%sDollars saved:   %s %s$%.6f%s  (%s)\n",
		colorBold, colorYellow, colorReset, colorYellow, saved, colorReset, pricing.ModelName)
	fmt.Fprintln(w, divider)
	fmt.Fprintln(w)
}

// PrintGlobalSummary prints an aggregate summary across all sessions.
func PrintGlobalSummary(stats GlobalStats, pricing Pricing, w io.Writer) {
	if w == nil {
		w = os.Stderr
	}
	if pricing.InputCostPer1K == 0 {
		pricing = DefaultPricing
	}

	totalSaved := DollarsSaved(stats.TotalRawTokens-stats.TotalPrunedTokens, pricing)
	divider := strings.Repeat("─", 56)

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s%s  SurfaceProxy Global Summary  %s%s\n", colorBold, colorCyan, colorReset, colorDim)
	fmt.Fprintln(w, divider+colorReset)
	fmt.Fprintf(w, "  %sTotal sessions:  %s%d%s\n", colorDim, colorReset, stats.TotalSessions, colorReset)
	fmt.Fprintf(w, "  %sTotal prune ops: %s%d%s\n", colorDim, colorReset, stats.TotalPruneOps, colorReset)
	fmt.Fprintf(w, "  %sTotal raw bytes: %s%s%s\n", colorDim, colorReset, formatBytes(stats.TotalRawBytes), colorReset)
	fmt.Fprintln(w, divider)
	fmt.Fprintf(w, "  %s%sContext reduction:%s %s%.1f%%%s\n",
		colorBold, colorGreen, colorReset, colorGreen, stats.ReductionPct, colorReset)
	fmt.Fprintf(w, "  %s%sTotal $ saved:   %s %s$%.4f%s  (%s)\n",
		colorBold, colorYellow, colorReset, colorYellow, totalSaved, colorReset, pricing.ModelName)
	fmt.Fprintln(w, divider)

	// Print the definitive PLG metric
	fmt.Fprintf(w, "\n  %s%s💰 Saved Dollar Amount = Tokens Compressed × API Unit Price%s\n",
		colorBold, colorYellow, colorReset)
	fmt.Fprintf(w, "  %s%s   $%.6f = %s tokens × $%.5f / 1K%s\n\n",
		colorBold, colorYellow, totalSaved,
		formatCount(stats.TotalRawTokens-stats.TotalPrunedTokens),
		pricing.InputCostPer1K, colorReset)
}

// formatBytes returns a human-readable byte count string.
func formatBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// formatCount returns a human-readable token/number count.
func formatCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// StartTime is exported for tests that need a stable reference
var StartTime = time.Now()
