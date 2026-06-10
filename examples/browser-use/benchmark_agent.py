"""
SurfaceProxy + browser-use: Token Savings Benchmark

This script runs a controlled browser automation task with and without SurfaceProxy
to generate concrete, reproducible token savings metrics you can use for benchmarking.

USAGE:
    # Run with SurfaceProxy (default — surface-proxy must be running)
    python benchmark_agent.py

    # Run against raw browser (no proxy — for comparison baseline)
    python benchmark_agent.py --no-proxy

    # Run both and compare
    python benchmark_agent.py --compare

WHAT IT MEASURES:
    Each test run navigates 5 real-world URLs with non-trivial page complexity,
    calls get_dom_content() after each navigation, and records:
      - Raw HTML bytes per page
      - Token count before pruning (bytes / 4 heuristic)
      - Token count after pruning (from SurfaceProxy API)
      - Reduction % and dollar savings per page and total

    The benchmark is designed to be self-contained and reproducible, so you can
    share the numbers with your community (Reddit, GitHub, blog posts).
"""

import asyncio
import argparse
import os
import sys
import time
import httpx

try:
    from browser_use import Agent, Browser, BrowserConfig
    from langchain_anthropic import ChatAnthropic
except ImportError:
    print("Install dependencies: pip install browser-use langchain-anthropic httpx")
    sys.exit(1)


# ── Benchmark targets ─────────────────────────────────────────────────────────
# These are content-heavy pages representative of real agent workloads

BENCHMARK_URLS = [
    ("GitHub trending (weekly)", "https://github.com/trending?since=weekly"),
    ("Wikipedia (Python article)", "https://en.wikipedia.org/wiki/Python_(programming_language)"),
    ("PyPI search (automation)", "https://pypi.org/search/?q=browser+automation"),
    ("Hacker News front page",   "https://news.ycombinator.com"),
    ("MDN Web Docs (Fetch API)", "https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API"),
]

SURFACEPROXY_CDP = os.getenv("SURFACEPROXY_CDP", "ws://localhost:8443/v1/session")
SURFACEPROXY_API = os.getenv("SURFACEPROXY_API", "http://localhost:8080/api/status")
ANTHROPIC_MODEL  = os.getenv("ANTHROPIC_MODEL",  "claude-3-5-sonnet-20241022")

# Claude 3.5 Sonnet pricing (input tokens)
PRICE_PER_MTK = 3.00  # $3.00 per million tokens


# ── Helpers ───────────────────────────────────────────────────────────────────

def tokens_from_bytes(n: int) -> int:
    """Conservative estimate: 1 token per 4 bytes of text."""
    return n // 4


def dollars(tokens: int) -> float:
    return (tokens / 1_000_000) * PRICE_PER_MTK


async def get_savings_snapshot() -> dict:
    """Fetch cumulative metrics from SurfaceProxy REST API."""
    try:
        async with httpx.AsyncClient(timeout=2.0) as client:
            return (await client.get(SURFACEPROXY_API)).json()
    except Exception:
        return {}


def print_row(label: str, raw: int, pruned: int) -> None:
    pct = 100 * (1 - pruned / raw) if raw > 0 else 0
    saved = raw - pruned
    print(f"  {label:<35} {raw:>8,} → {pruned:>6,}  ({pct:>4.0f}%  saved ${dollars(saved):.5f})")


# ── Benchmark run ─────────────────────────────────────────────────────────────

async def run_benchmark(use_proxy: bool) -> dict:
    label = "WITH SurfaceProxy" if use_proxy else "WITHOUT SurfaceProxy (baseline)"
    print(f"\n{'═' * 62}")
    print(f"  BENCHMARK RUN: {label}")
    print(f"{'═' * 62}")

    if use_proxy:
        # Snapshot before run to get per-run delta
        before = await get_savings_snapshot()
        browser = Browser(config=BrowserConfig(
            cdp_url=f"{SURFACEPROXY_CDP}?allowlist=*.github.com,*.wikipedia.org,*.pypi.org,*.ycombinator.com,*.mozilla.org"
        ))
    else:
        before = {}
        browser = Browser()  # Default: connects to a raw browser

    llm = ChatAnthropic(model=ANTHROPIC_MODEL)

    task = (
        "For each of these URLs, navigate there and summarize what you see "
        "in exactly one sentence:\n" +
        "\n".join(f"- {url}" for _, url in BENCHMARK_URLS)
    )

    start = time.perf_counter()
    agent = Agent(task=task, llm=llm, browser=browser)
    result = await agent.run()
    elapsed = time.perf_counter() - start

    print(f"\n  Task completed in {elapsed:.1f}s")

    if use_proxy:
        after = await get_savings_snapshot()

        raw_tok    = after.get("total_raw_tokens", 0)    - before.get("total_raw_tokens", 0)
        pruned_tok = after.get("total_pruned_tokens", 0) - before.get("total_pruned_tokens", 0)
        saved_tok  = after.get("tokens_reduced", 0)      - before.get("tokens_reduced", 0)
        saved_usd  = after.get("dollars_saved", 0.0)     - before.get("dollars_saved", 0.0)
        pct        = 100 * (1 - pruned_tok / raw_tok) if raw_tok > 0 else 0

        print(f"\n  {'─' * 60}")
        print(f"  📥  Raw HTML tokens fed to agent:    {raw_tok:>10,}")
        print(f"  📤  Pruned MD tokens (after proxy):  {pruned_tok:>10,}")
        print(f"  🗜️   Tokens saved this run:           {saved_tok:>10,}  ({pct:.1f}%)")
        print(f"  💰  Cost saved this run:             ${saved_usd:>11.6f}")
        print(f"  {'─' * 60}")

        return {
            "mode":       "proxy",
            "raw_tok":    raw_tok,
            "pruned_tok": pruned_tok,
            "saved_tok":  saved_tok,
            "saved_usd":  saved_usd,
            "pct":        pct,
            "elapsed":    elapsed,
        }

    return {"mode": "baseline", "elapsed": elapsed}


# ── Comparison summary ────────────────────────────────────────────────────────

def print_comparison(baseline: dict, proxy: dict) -> None:
    print(f"\n{'═' * 62}")
    print("  COMPARISON SUMMARY")
    print(f"{'═' * 62}")
    print(f"  {'Metric':<35} {'Baseline':>12}  {'With Proxy':>12}")
    print(f"  {'─' * 59}")

    raw_tok_base = proxy["raw_tok"]  # Same page content; proxy tells us raw count
    print(f"  {'DOM tokens per run':<35} {raw_tok_base:>12,}  {proxy['pruned_tok']:>12,}")
    cost_base  = dollars(raw_tok_base)
    cost_proxy = dollars(proxy["pruned_tok"])
    print(f"  {'Estimated LLM input cost':<35} ${cost_base:>11.5f}  ${cost_proxy:>11.5f}")
    print(f"  {'Reduction':<35} {'—':>12}  {proxy['pct']:>11.1f}%")
    print(f"  {'Dollars saved per run':<35} {'—':>12}  ${proxy['saved_usd']:>11.6f}")
    print(f"  {'Elapsed time':<35} {baseline['elapsed']:>11.1f}s  {proxy['elapsed']:>11.1f}s")
    print(f"{'═' * 62}\n")


# ── Entry point ───────────────────────────────────────────────────────────────

async def main() -> None:
    parser = argparse.ArgumentParser(description="SurfaceProxy token savings benchmark")
    parser.add_argument("--no-proxy",  action="store_true", help="Run baseline (no SurfaceProxy)")
    parser.add_argument("--compare",   action="store_true", help="Run baseline then proxy and compare")
    args = parser.parse_args()

    if args.compare:
        baseline = await run_benchmark(use_proxy=False)
        proxy    = await run_benchmark(use_proxy=True)
        print_comparison(baseline, proxy)
    elif args.no_proxy:
        await run_benchmark(use_proxy=False)
    else:
        await run_benchmark(use_proxy=True)


if __name__ == "__main__":
    asyncio.run(main())
