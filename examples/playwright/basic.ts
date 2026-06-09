import { chromium } from 'playwright';

async function fetchStatus() {
  try {
    const response = await fetch('http://localhost:8080/api/status');
    if (response.ok) {
      const data = await response.json();
      console.log('\n--- SurfaceProxy Savings Dashboard Status ---');
      console.log(`Active Sessions:   ${data.active_sessions}`);
      console.log(`Prune Operations:  ${data.prune_ops}`);
      console.log(`Raw HTML Bytes:    ${(data.bytes_in / 1024).toFixed(2)} KB`);
      console.log(`Pruned HTML Bytes: ${(data.bytes_out / 1024).toFixed(2)} KB`);
      console.log(`Savings Percentage:${data.savings_pct.toFixed(2)}%`);
      console.log(`Dollars Saved:     $${data.dollars_saved.toFixed(5)}`);
      console.log('-----------------------------------------------\n');
    }
  } catch (err: any) {
    // If the daemon is not running with UI server, this call might fail, which is okay.
    console.log('\n[INFO] SurfaceProxy UI Dashboard server not reachable or disabled.');
  }
}

async function main() {
  // We configure a session with allowlist filters
  // Navigation to URLs outside this allowlist (and global allowlist) will be blocked.
  const wsEndpoint = 'ws://localhost:8443/v1/session?allowlist=*.wikipedia.org,*.python.org';

  console.log(`Connecting Playwright client to SurfaceProxy at: ${wsEndpoint}`);

  const browser = await chromium.connectOverCDP(wsEndpoint);

  try {
    // Connect to the default context and page
    const contexts = browser.contexts();
    const context = contexts[0] || await browser.newContext();
    const pages = context.pages();
    const page = pages[0] || await context.newPage();

    // 1. Navigate to a permitted website (wikipedia.org)
    console.log('Navigating to Wikipedia (Allowed domain)...');
    await page.goto('https://en.wikipedia.org/wiki/Special:Random', {
      waitUntil: 'domcontentloaded',
    });
    const title = await page.title();
    console.log(`Successfully loaded Wikipedia! Title: "${title}"`);

    // 2. Try to navigate to a blocked website (google.com)
    console.log('\nAttempting to navigate to google.com (Blocked domain)...');
    try {
      await page.goto('https://www.google.com', {
        waitUntil: 'domcontentloaded',
        timeout: 5000,
      });
      console.log('ERROR: Navigation to google.com succeeded but should have been blocked!');
    } catch (err: any) {
      console.log(`SUCCESS: Navigation blocked as expected! Error: "${err.message}"`);
    }

  } finally {
    // Close the browser connection
    console.log('Closing browser session...');
    await browser.close();
  }

  // 3. Fetch cumulative savings from the dashboard REST API
  await fetchStatus();
}

main().catch((err) => {
  console.error('Playwright Example Error:', err);
});
