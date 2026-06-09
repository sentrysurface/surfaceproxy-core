"""
SurfaceProxy + Browser-Use Integration Example

This example shows how to use SurfaceProxy as the browser backend for Browser-Use,
reducing token consumption by 60-90% by serving pruned Markdown instead of raw HTML.

Prerequisites:
    pip install browser-use langchain-anthropic playwright
    playwright install chromium

    And SurfaceProxy running:
    ./surface-proxy --config surface-proxy.json
"""

import asyncio
from browser_use import Agent, Browser, BrowserConfig
from langchain_anthropic import ChatAnthropic


async def main():
    # Point Browser-Use at SurfaceProxy's CDP endpoint instead of a raw browser.
    # SurfaceProxy intercepts all DOM retrieval and returns token-optimised Markdown.
    browser = Browser(
        config=BrowserConfig(
            # SurfaceProxy's /v1/session endpoint supports per-session firewall overrides.
            # The allowlist param restricts this agent session to the specified domains.
            cdp_url="ws://localhost:8443/v1/session?allowlist=*.github.com,*.wikipedia.org",
        )
    )

    llm = ChatAnthropic(model="claude-3-5-sonnet-20241022")

    agent = Agent(
        task=(
            "Go to https://github.com/trending and find the top 3 trending Python repositories. "
            "For each one, return the repo name, star count, and a one-sentence description."
        ),
        llm=llm,
        browser=browser,
    )

    result = await agent.run()
    print("\n=== Agent Result ===")
    print(result)


if __name__ == "__main__":
    asyncio.run(main())
