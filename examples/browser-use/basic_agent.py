"""
SurfaceProxy + browser-use: Drop-In CDP Integration

THE ONE-LINE CHANGE:
Replace `Browser()` with `Browser(config=BrowserConfig(cdp_url="ws://localhost:8443/v1/session"))`
and immediately reduce your LLM token bill by 60-90%.

HOW IT WORKS:
SurfaceProxy is a local Go binary that intercepts Chrome DevTools Protocol traffic.
When browser-use calls get_dom_content(), the raw HTML never reaches your agent.
SurfaceProxy strips scripts, styles, SVG, hidden elements, and layout noise,
then returns clean semantic Markdown. Your agent reads signal, not whitespace.

PREREQUISITES:
    pip install browser-use langchain-anthropic httpx
    # Install and start SurfaceProxy:
    curl -sSL https://raw.githubusercontent.com/sentrysurface/surfaceproxy-core/main/scripts/install.sh | sh
    surface-proxy &

TYPICAL SAVINGS (Claude 3.5 Sonnet, $3/MTok input):
    GitHub trending page:   8,400 → 620 tokens  (93% reduction, saves ~$0.023/call)
    Wikipedia article:     12,000 → 1,800 tokens (85% reduction, saves ~$0.030/call)
    PyPI search page:       6,200 → 480 tokens  (92% reduction, saves ~$0.017/call)
"""

import asyncio
import os
import httpx
from browser_use import Agent, Browser, BrowserConfig
from langchain_anthropic import ChatAnthropic


# ── Configuration ─────────────────────────────────────────────────────────────

SURFACEPROXY_CDP = os.getenv(
    "SURFACEPROXY_CDP",
    "ws://localhost:8443/v1/session"
)

SURFACEPROXY_API = os.getenv(
    "SURFACEPROXY_API",
    "http://localhost:8080/api/status"
)


# ── Savings display ───────────────────────────────────────────────────────────

async def print_savings(label: str = "") -> None:
    """Query SurfaceProxy's REST API and print live token savings."""
    try:
        async with httpx.AsyncClient(timeout=2.0) as client:
            data = (await client.get(SURFACEPROXY_API)).json()

        tokens_in  = data.get("total_raw_tokens", 0)
        tokens_out = data.get("total_pruned_tokens", 0)
        saved      = data.get("tokens_reduced", 0)
        pct        = data.get("reduction_pct", 0.0)
        dollars    = data.get("dollars_saved", 0.0)

        print(f"\n{'─' * 58}")
        if label:
            print(f"  📊  {label}")
        print(f"  📥  Raw HTML tokens:   {tokens_in:>10,}")
        print(f"  📤  Pruned MD tokens:  {tokens_out:>10,}")
        print(f"  🗜️   Tokens saved:      {saved:>10,}  ({pct:.1f}% reduction)")
        print(f"  💰  Dollars saved:     ${dollars:>11.6f}")
        print(f"{'─' * 58}\n")
    except Exception as exc:
        print(f"  [savings] Could not reach SurfaceProxy API: {exc}")


# ── Agent task ────────────────────────────────────────────────────────────────

async def run_agent() -> None:
    """
    Run a multi-step research task via browser-use through SurfaceProxy.

    The only change from a standard browser-use setup is the cdp_url.
    All DOM retrieval is automatically compressed — no agent code changes needed.
    """
    print("Connecting browser-use to SurfaceProxy...")
    print(f"  CDP endpoint: {SURFACEPROXY_CDP}\n")

    # ── THE ONE-LINE CHANGE ────────────────────────────────────────────────
    browser = Browser(
        config=BrowserConfig(
            # Route all CDP traffic through SurfaceProxy.
            # The ?allowlist= query param restricts this session to specific domains,
            # preventing accidental navigation to unintended sites.
            cdp_url=f"{SURFACEPROXY_CDP}?allowlist=*.github.com,*.wikipedia.org,*.pypi.org"
        )
    )
    # ── END CHANGE ─────────────────────────────────────────────────────────

    llm = ChatAnthropic(model="claude-3-5-sonnet-20241022")

    agent = Agent(
        task=(
            "Complete these three research steps:\n"
            "1. Go to https://github.com/trending?since=daily and list the top 5 trending repos "
            "   with their star count and primary language.\n"
            "2. Go to the GitHub page of the #1 trending repo and extract its README summary "
            "   (first paragraph only).\n"
            "3. Search https://pypi.org/search/?q=llm+browser for Python packages related to "
            "   browser automation with LLMs. List the top 3 results.\n"
            "Present your findings in a structured report."
        ),
        llm=llm,
        browser=browser,
    )

    print("Starting agent run...")
    result = await agent.run()

    print("\n" + "=" * 58)
    print("AGENT RESULT")
    print("=" * 58)
    print(result)

    await print_savings("Final savings for this agent run")


# ── Entry point ───────────────────────────────────────────────────────────────

if __name__ == "__main__":
    asyncio.run(run_agent())
