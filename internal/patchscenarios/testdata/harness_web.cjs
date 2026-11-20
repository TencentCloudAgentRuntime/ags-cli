// Embedded by TestHarnessWebLive. Run with Node and Playwright 1.64.1; the
// credential-bearing input arrives only on stdin. No cookies are saved to disk.
const fs = require('node:fs');
const assert = require('node:assert/strict');

async function navigateWithNativeSocket(page, address, report) {
  const network = await page.context().newCDPSession(page);
  const sockets = new Map();
  await network.send('Network.enable');
  report.WebSocketStatus = 0;
  network.on('Network.webSocketCreated', event => sockets.set(event.requestId, new URL(event.url).pathname));
  let timer;
  const connected = new Promise((resolve, reject) => {
    timer = setTimeout(() => reject(new Error('Native WebSocket readiness timed out')), 90000);
    network.on('Network.webSocketHandshakeResponseReceived', event => {
      if (sockets.get(event.requestId) !== '/api/remote.mux') return;
      report.WebSocketStatus = event.response.status;
      if (event.response.status === 101) resolve();
      else reject(new Error('Native WebSocket handshake failed'));
    });
  });
  try {
    const [response] = await Promise.all([
      page.goto(address, { waitUntil: 'domcontentloaded', timeout: 90000 }),
      connected,
    ]);
    return response;
  } finally {
    clearTimeout(timer);
  }
}

(async () => {
  const report = { Passed: false, Phase: 'input' };
  let browser;
  try {
    const { chromium } = require('playwright');
    assert.equal(require('playwright/package.json').version, '1.64.1');
    const entry = new URL(JSON.parse(fs.readFileSync(0, 'utf8')).accessURL);
    assert.equal(entry.protocol, 'https:');
    assert.ok(entry.searchParams.get('token'));
    assert.ok(entry.searchParams.get('affinity_id'));
    const safeURL = entry.origin + '/';
    report.Phase = 'launch';
    browser = await chromium.launch({ headless: true });
    const authenticated = await browser.newContext();
    const page = await authenticated.newPage();
    page.setDefaultTimeout(90000);
    const redirects = [];
    page.on('response', response => {
      if (response.request().isNavigationRequest() && response.status() === 307) redirects.push(307);
    });
    report.Phase = 'login';
    // No reload/retry here: the CLI's readiness wait must have warmed the
    // instance used by this exact browser URL.
    const response = await navigateWithNativeSocket(page, entry.href, report);
    report.PageStatus = response.status();
    assert.equal(report.PageStatus, 200);
    assert.ok(redirects.includes(307));
    report.TokenRemoved = !new URL(page.url()).searchParams.has('token');
    assert.ok(report.TokenRemoved);
    const cookies = await authenticated.cookies(entry.origin);
    const auth = cookies.find(cookie => cookie.name === 'tencentags-deployment-token');
    const affinity = cookies.find(cookie => cookie.name === 'tencentags-deployment-affinity-id');
    report.SecureCookies = [auth, affinity].every(cookie => cookie?.secure && cookie.httpOnly && cookie.sameSite === 'Lax');
    assert.ok(report.SecureCookies);
    assert.equal(decodeURIComponent(affinity.value.replace(/\+/g, ' ')), entry.searchParams.get('affinity_id'));

    report.Phase = 'anonymous';
    const anonymous = await browser.newContext();
    const denied = await (await anonymous.newPage()).goto(safeURL, { waitUntil: 'domcontentloaded', timeout: 30000 });
    report.AnonymousStatus = denied.status();
    assert.equal(report.AnonymousStatus, 401);
    await anonymous.close();

    report.Phase = 'preview-notice';
    // Complete the first-login UI in the fresh profile before taking its
    // storage state. The notice appears asynchronously on this first visit.
    const notice = page.getByRole('dialog', { name: 'Preview Notice', exact: true });
    await notice.waitFor({ state: 'visible' });
    await notice.getByRole('button', { name: 'Continue', exact: true }).click();
    await notice.waitFor({ state: 'hidden' });

    report.Phase = 'workspace-path';
    const choose = page.getByRole('button', { name: 'Choose workspace', exact: true });
    await choose.click();
    const dialog = page.getByRole('dialog', { name: 'Select Workspace Directory', exact: true });
    // The picker initially opens at Home, not the filesystem root. Navigate
    // to the existing /tmp and wait for the path editor to close and the
    // confirmed breadcrumb to appear before selecting it.
    await dialog.getByRole('button', { name: 'Edit path', exact: true }).click();
    const path = dialog.locator('input');
    await path.fill('/tmp');
    await path.press('Enter');
    await path.waitFor({ state: 'hidden' });
    // The tree also contains a tmp button; the first match is the breadcrumb.
    await dialog.getByRole('button', { name: 'tmp', exact: true }).first().waitFor({ state: 'visible' });
    report.Phase = 'workspace-selection';
    await dialog.getByRole('button', { name: 'Open', exact: true }).click();
    await dialog.waitFor({ state: 'hidden' });
    report.Phase = 'composer';
    const editor = page.locator('[role="textbox"][contenteditable="true"]');
    await editor.waitFor({ state: 'visible' });

    report.Phase = 'model-reply';
    const marker = 'AGR_DSH_WEB_OK';
    await editor.fill(`Reply with exactly ${marker} and nothing else. Do not use tools.`);
    await page.getByRole('button', { name: 'Send message', exact: true }).click();
    // Match a standalone rendered answer, not the user's longer prompt.
    await page.locator('p').filter({ hasText: new RegExp(`^${marker}$`) }).last().waitFor({ state: 'visible', timeout: 180000 });
    report.Reply = true;
    assert.equal(report.WebSocketStatus, 101);

    report.Phase = 'cookie-reentry';
    const state = await authenticated.storageState();
    await authenticated.close();
    const reentry = await browser.newContext({ storageState: state });
    const revisited = await navigateWithNativeSocket(await reentry.newPage(), safeURL, report);
    report.CookieReentry = revisited.status() === 200 && !revisited.request().redirectedFrom();
    assert.ok(report.CookieReentry);
    assert.equal(report.WebSocketStatus, 101);
    report.Phase = 'completed';
    report.Passed = true;
  } catch {
    // Browser exceptions may contain credentials or private page contents.
    process.exitCode = 1;
  } finally {
    if (browser) await browser.close();
    process.stdout.write(JSON.stringify(report) + '\n');
  }
})().catch(() => { process.stderr.write('Native Web E2E runner failed\n'); process.exitCode = 1; });
