"""
SurfaceProxy + Browser-Use: Advanced Task Agent

Demonstrates:
- Firewall allowlist scoped to a single session
- Multi-step research task (navigate, read, click, form fill)
- Printing token savings after each step
"""

import asyncio
import httpx
from browser_use import Agent, Browser, BrowserConfig
from langchain_anthropic import ChatAnthropic


async def print_savings():
    """Fetch live telemetry from SurfaceProxy dashboard and print savings."""
    try:
        async with httpx.AsyncClient() as client:
            resp = await client.get("http://localhost:8080/api/status", timeout=2)
            data = resp.json()
            print(f"\n{'─'*50}")
            print(f"  🗜️  Tokens saved so far: {data.get('tokens_reduced', 0):,}")
            print(f"  💰  Dollars saved:        ${data.get('dollars_saved', 0):.6f}")
            print(f"  📉  Reduction:            {data.get('reduction_pct', 0):.1f}%")
            print(f"{'─'*50}\n")
    except Exception:
        pass


async def main():
    browser = Browser(
        config=BrowserConfig(
            # Allowlist locked to only the domains this agent needs
            cdp_url="ws://localhost:8443/v1/session?allowlist=*.python.org,*.pypi.org",
        )
    )

    llm = ChatAnthropic(model="claude-3-5-sonnet-20241022")

    agent = Agent(
        task=(
            "1. Go to https://pypi.org/search/?q=browser+automation and list the top 5 packages.\n"
            "2. For the most popular one, go to its PyPI page and extract the latest version number.\n"
            "3. Report findings in a clean table."
        ),
        llm=llm,
        browser=browser,
    )

    result = await agent.run()
    print("\n=== Agent Result ===")
    print(result)

    await print_savings()


if __name__ == "__main__":
    asyncio.run(main())
