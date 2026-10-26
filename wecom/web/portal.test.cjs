'use strict';

// Run with an existing Playwright installation; this file adds no dependencies:
// PLAYWRIGHT_MODULE_PATH=/path/to/node_modules/@playwright/test node --test web/portal.test.cjs
// Chromium uses a temporary profile. All network traffic is intercepted locally.
const { test, before, after } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium, expect } = require(process.env.PLAYWRIGHT_MODULE_PATH || '@playwright/test');

const ORIGIN = 'https://wecom-portal.test';
const WEBHOOK_PATH = '/webhooks/wecom/interactions';
const INTERACTION_ID = 'message-20261026-001';
const USER_ID = 'zhang.san';
const STATE = 'a'.repeat(64);
const STORAGE_KEY = 'wecom-interaction-oauth:' + WEBHOOK_PATH + ':' + INTERACTION_ID;
const WECHAT_USER_AGENT = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 MicroMessenger/8.0.65';
const HTML = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8').replaceAll('{{NONCE}}', 'portal-test-nonce');
const CSP = "default-src 'none'; script-src 'nonce-portal-test-nonce'; style-src 'nonce-portal-test-nonce'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'";
const SCREENSHOT_DIRECTORY = process.env.PORTAL_SCREENSHOT_DIR || '/tmp';
let browser;

before(async () => {
  browser = await chromium.launch({ headless: true });
});

after(async () => {
  if (browser) await browser.close();
});

const DEFAULT_BUTTONS = [
  { id: 'approve', text: '同意发布', value: { decision: 'approve', secret: 'button-value-private' } },
  { id: 'request-changes', text: '请修改后再提交', value: { decision: 'changes' } },
  { id: 'remind-later', text: '稍后提醒我' }
];

function result(revision = 0, overrides = {}) {
  return {
    ticket: 'ticket-' + revision,
    user_id: USER_ID,
    interaction: {
      id: INTERACTION_ID,
      request_id: 'RELEASE-20261026-0812',
      title: '产品更新 · 发布确认',
      content: '新版本已完成测试，请查看本次发布内容。\n选择下方操作，将由工作流继续处理。',
      buttons: DEFAULT_BUTTONS,
      context: { internal_reference: 'message-context-private' },
      revision,
      created_at: '2026-10-26T08:12:00+08:00',
      expires_at: '2099-10-26T20:12:00+08:00',
      updated_at: revision ? '2026-10-26T08:15:00+08:00' : '',
      actor_user_id: revision ? USER_ID : '',
      ...overrides
    }
  };
}

async function fixture(t, options = {}) {
  const context = await browser.newContext({
    viewport: { width: 390, height: 844 },
    deviceScaleFactor: 1,
    isMobile: true,
    hasTouch: true,
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    reducedMotion: 'reduce',
    ...(options.userAgent ? { userAgent: options.userAgent } : {})
  });
  const page = await context.newPage();
  t.after(async () => {
    await page.unrouteAll({ behavior: 'ignoreErrors' });
    await context.close();
  });
  const calls = [];
  const pageErrors = [];
  const unexpectedRequests = [];
  const authorizations = [];
  const loginObservations = [];
  let loginNumber = 0;
  let actionNumber = 0;
  let exchangeObservation;

  page.on('pageerror', (error) => pageErrors.push(error.message));
  await page.addInitScript(({ key, stored }) => {
    if (stored !== null) sessionStorage.setItem(key, stored);
  }, { key: STORAGE_KEY, stored: options.storedState === undefined ? STATE : options.storedState });

  await page.route('**/*', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.origin === 'https://open.weixin.qq.com' && url.pathname === '/connect/oauth2/authorize' && request.isNavigationRequest()) {
      authorizations.push({ url: request.url() });
      await route.fulfill({ status: 200, contentType: 'text/html', body: '<!doctype html><title>Mock WeChat OAuth</title>' });
      return;
    }
    if (url.origin !== ORIGIN || url.pathname !== WEBHOOK_PATH) {
      unexpectedRequests.push(request.url());
      await route.abort('blockedbyclient');
      return;
    }
    if (request.method() === 'GET') {
      await route.fulfill({ status: 200, contentType: 'text/html; charset=utf-8', headers: { 'Content-Security-Policy': CSP }, body: HTML });
      return;
    }
    const operation = url.searchParams.get('op');
    const body = request.postDataJSON();
    calls.push({ operation, body });
    assert.equal(request.method(), 'POST');
    assert.match(request.headers()['content-type'], /^application\/json/);
    assert.equal(body.interaction_id, INTERACTION_ID);
    let reply;
    if (operation === 'login') {
      loginNumber += 1;
      loginObservations.push({ stored: await page.evaluate((key) => sessionStorage.getItem(key), STORAGE_KEY) });
      if (options.onLogin) {
        const response = await options.onLogin({ body, number: loginNumber, route });
        if (response === null) return;
        reply = response;
      } else {
        const authorize = new URL('https://open.weixin.qq.com/connect/oauth2/authorize');
        authorize.search = new URLSearchParams({ appid: 'test-corp', redirect_uri: ORIGIN + WEBHOOK_PATH + '?interaction=' + INTERACTION_ID, response_type: 'code', scope: 'snsapi_base', state: body.state });
        authorize.hash = 'wechat_redirect';
        reply = { authorize_url: authorize.href };
      }
    } else if (operation === 'exchange') {
      exchangeObservation = {
        url: page.url(),
        stored: await page.evaluate((key) => sessionStorage.getItem(key), STORAGE_KEY)
      };
      reply = options.exchangeResult || result();
    } else if (operation === 'action') {
      actionNumber += 1;
      if (options.onAction) {
        const response = await options.onAction({ body, number: actionNumber, route });
        if (response === null) return;
        reply = response;
      } else {
        reply = { ...result(actionNumber, { last_action_id: body.action_id }), event_id: 'event-' + actionNumber, event_status: 'submitted' };
      }
    } else {
      throw new Error('Unexpected API operation: ' + operation);
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(reply) });
  });

  const search = new URLSearchParams(options.entryParams || (options.entry ? { interaction: INTERACTION_ID } : { interaction: INTERACTION_ID, code: 'oauth-code', state: options.callbackState || STATE }));
  await page.goto(ORIGIN + WEBHOOK_PATH + '?' + search, { waitUntil: 'domcontentloaded' });
  return { page, calls, pageErrors, unexpectedRequests, authorizations, loginObservations, observation: () => exchangeObservation };
}

function assertCleanBrowser(env) {
  assert.deepEqual(env.pageErrors, [], 'Browser JavaScript must not throw');
  assert.deepEqual(env.unexpectedRequests, [], 'The page must not load external resources');
}

async function screenshot(page, state) {
  await page.screenshot({ path: path.join(SCREENSHOT_DIRECTORY, 'wecom-portal-' + state + '.png'), fullPage: true });
}

for (const [client, userAgent] of [['WeChat', WECHAT_USER_AGENT], ['WeCom', WECHAT_USER_AGENT + ' wxwork/4.1.36']]) {
test('A ' + client + ' message entry automatically starts silent OAuth with a random browser-bound state', async (t) => {
  const env = await fixture(t, { entry: true, storedState: null, userAgent });
  await expect.poll(() => env.authorizations.length, { timeout: 1500 }).toBe(1);
  assert.equal(env.calls.length, 1);
  const { operation, body } = env.calls[0];
  assert.equal(operation, 'login');
  assert.match(body.state, /^[a-f0-9]{64}$/);
  assert.notEqual(body.state, STATE);
  assert.equal(env.loginObservations[0].stored, body.state);
  const target = new URL(env.authorizations[0].url);
  assert.equal(target.searchParams.get('scope'), 'snsapi_base');
  assert.equal(target.searchParams.get('state'), body.state);
  assert.equal(target.searchParams.has('user_id'), false);
  assertCleanBrowser(env);
});
}

test('Silent OAuth shows identity verification while loading, fails once, and permits a fresh manual retry', async (t) => {
  let release;
  const blocked = new Promise((resolve) => { release = resolve; });
  const env = await fixture(t, {
    entry: true, storedState: null, userAgent: WECHAT_USER_AGENT,
    onLogin: async ({ number, route }) => {
      if (number === 1) await blocked;
      await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: '服务暂时不可用' }) });
      return null;
    }
  });
  t.after(release);
  await expect(env.page.locator('#auth-loading')).toBeVisible({ timeout: 1500 });
  await expect(env.page.locator('#auth-title')).toHaveText('正在识别微信身份');
  await expect(env.page.locator('#login-button')).toBeHidden();
  release();
  await expect(env.page.locator('#auth-title')).toHaveText('暂时无法验证身份');
  await expect(env.page.locator('#login-button')).toBeVisible();
  await expect(env.page.locator('#login-button')).toBeEnabled();
  await env.page.waitForTimeout(100);
  assert.equal(env.calls.length, 1, 'A failed automatic login must not retry on its own');
  assert.equal(await env.page.evaluate((key) => sessionStorage.getItem(key), STORAGE_KEY), null);
  await env.page.locator('#login-button').click();
  await expect.poll(() => env.calls.length).toBe(2);
  await expect(env.page.locator('#login-button')).toBeEnabled();
  assert.notEqual(env.calls[0].body.state, env.calls[1].body.state);
  assert.equal(env.authorizations.length, 0);
  assertCleanBrowser(env);
});

test('A non-WeChat entry does not automatically authorize and explains where to open it', async (t) => {
  const env = await fixture(t, { entry: true, storedState: null });
  await expect(env.page.locator('#auth-description')).toContainText('微信');
  await expect(env.page.locator('#login-button')).toBeVisible();
  await expect(env.page.locator('#login-button')).toBeEnabled();
  assert.equal(env.calls.length, 0);
  assert.equal(env.authorizations.length, 0);
  assertCleanBrowser(env);
});

test('An OAuth error callback in WeChat stays on a manual retry page without restarting authorization', async (t) => {
  const env = await fixture(t, { userAgent: WECHAT_USER_AGENT, entryParams: { interaction: INTERACTION_ID, error: 'access_denied' } });
  await expect(env.page.locator('#auth-title')).toHaveText('请重新验证身份');
  await expect(env.page.locator('#login-button')).toBeEnabled();
  assert.equal(new URL(env.page.url()).searchParams.has('error'), false);
  assert.equal(env.calls.length, 0);
  assert.equal(env.authorizations.length, 0);
  assertCleanBrowser(env);
});

test('An ambiguous message entry never automatically starts OAuth', async (t) => {
  const env = await fixture(t, { userAgent: WECHAT_USER_AGENT, entryParams: [['interaction', INTERACTION_ID], ['interaction', 'another-message']] });
  await expect(env.page.locator('#auth-title')).toHaveText('消息链接不完整');
  await expect(env.page.locator('#login-button')).toBeHidden();
  assert.equal(env.calls.length, 0);
  assert.equal(env.authorizations.length, 0);
  assertCleanBrowser(env);
});

test('Returning from OAuth with Back does not immediately restart authorization', async (t) => {
  const env = await fixture(t, { entry: true, storedState: null, userAgent: WECHAT_USER_AGENT });
  await expect(env.page).toHaveURL(/^https:\/\/open\.weixin\.qq\.com\/connect\/oauth2\/authorize/);
  await env.page.goBack({ waitUntil: 'domcontentloaded' });
  await expect(env.page.locator('#auth-title')).toHaveText('请重新验证身份');
  await expect(env.page.locator('#login-button')).toBeEnabled();
  assert.equal(env.calls.length, 1);
  assert.equal(env.authorizations.length, 1);
  assertCleanBrowser(env);
});

test('A restored page requires manual verification instead of returning immediately to OAuth', async (t) => {
  const env = await fixture(t, { userAgent: WECHAT_USER_AGENT });
  await expect(env.page.locator('#interaction-panel')).toBeVisible();
  await env.page.evaluate(() => dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true })));
  await expect(env.page.locator('#auth-title')).toHaveText('请重新验证身份');
  await expect(env.page.locator('#interaction-panel')).toBeHidden();
  await expect(env.page.locator('#login-button')).toBeVisible();
  await expect(env.page.locator('#login-button')).toBeEnabled();
  assert.deepEqual(env.calls.map((call) => call.operation), ['exchange']);
  assert.equal(env.authorizations.length, 0);
  assertCleanBrowser(env);
});

test('OAuth state mismatch never exchanges a code or displays message content', async (t) => {
  const env = await fixture(t, { storedState: 'b'.repeat(64), userAgent: WECHAT_USER_AGENT });
  await expect(env.page.locator('#auth-title')).toHaveText('请重新验证身份');
  await expect(env.page.locator('#interaction-panel')).toBeHidden();
  await expect(env.page.locator('#login-button')).toBeEnabled();
  assert.equal(env.calls.length, 0);
  const url = new URL(env.page.url());
  assert.equal(url.searchParams.has('code'), false);
  assert.equal(url.searchParams.has('state'), false);
  assertCleanBrowser(env);
});

test('OAuth callback consumes state and cleans the URL before exchange; message fits a mobile screen', async (t) => {
  const env = await fixture(t, { userAgent: WECHAT_USER_AGENT });
  await expect(env.page.locator('#interaction-panel')).toBeVisible();
  await expect(env.page.locator('#actions button')).toHaveCount(3);
  await expect(env.page.locator('#current-user')).toHaveText(USER_ID);
  await expect(env.page.getByRole('button', { name: '同意发布', exact: true })).toBeEnabled();
  const observation = env.observation();
  assert.equal(observation.stored, null);
  assert.equal(new URL(observation.url).searchParams.has('code'), false);
  assert.equal(new URL(observation.url).searchParams.has('state'), false);
  assert.deepEqual(env.calls, [{ operation: 'exchange', body: { interaction_id: INTERACTION_ID, state: STATE, code: 'oauth-code' } }]);
  assert.equal(await env.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'No horizontal overflow at 390px');
  const persisted = await env.page.evaluate(() => ({ session: { ...sessionStorage }, local: { ...localStorage } }));
  assert.deepEqual(persisted, { session: {}, local: {} }, 'No OAuth state or action ticket persists after exchange');
  await screenshot(env.page, 'generic');
  assertCleanBrowser(env);
});

test('Untrusted message and button text stays literal and private payloads never reach the DOM', async (t) => {
  const title = '<img src=x onerror="window.portalXss=true">';
  const content = '<svg onload="window.portalXss=true"></svg>\n<script>window.portalXss=true</script>';
  const buttonText = '<img src=x onerror="window.portalXss=true">提交';
  const env = await fixture(t, { exchangeResult: result(0, { title, content, buttons: [{ id: '__proto__', text: buttonText, value: { secret: 'button-value-private' } }] }) });
  await expect(env.page.locator('#interaction-title')).toHaveText(title);
  await expect(env.page.locator('#content')).toHaveText(content);
  await expect(env.page.locator('#actions button')).toHaveText(buttonText);
  assert.equal(await env.page.locator('#interaction-panel img, #interaction-panel svg, #interaction-panel script').count(), 0);
  assert.equal(await env.page.evaluate(() => window.portalXss), undefined);
  const text = await env.page.locator('body').innerText();
  assert.equal(text.includes('button-value-private'), false);
  assert.equal(text.includes('message-context-private'), false);
  await env.page.locator('#actions button').click();
  await expect.poll(() => env.calls.filter((call) => call.operation === 'action').length).toBe(1);
  assert.deepEqual(env.calls.at(-1).body, { interaction_id: INTERACTION_ID, ticket: 'ticket-0', action_id: '__proto__' });
  assertCleanBrowser(env);
});

test('Custom buttons rotate the ticket and remain usable without a business state machine', async (t) => {
  const env = await fixture(t, { exchangeResult: result(4, { actor_user_id: 'li.si', last_action_id: 'request-changes' }) });
  await expect(env.page.locator('#actor')).toHaveText('li.si');
  await expect(env.page.locator('#last-action')).toHaveText('请修改后再提交');
  await expect(env.page.locator('#actions button')).toHaveText(DEFAULT_BUTTONS.map((button) => button.text));
  await env.page.getByRole('button', { name: '同意发布', exact: true }).click();
  await expect(env.page.locator('#notice-text')).toHaveText('操作已提交。后续处理以业务系统或工作流通知为准。');
  await expect(env.page.locator('#last-action')).toHaveText('同意发布');
  for (const button of DEFAULT_BUTTONS) await expect(env.page.getByRole('button', { name: button.text, exact: true })).toBeEnabled();
  assert.ok((await env.page.locator('#notice').boundingBox()).height < 110, 'HTML indentation must not create blank lines in the notice');
  await env.page.getByRole('button', { name: '稍后提醒我', exact: true }).click();
  await expect(env.page.locator('#last-action')).toHaveText('稍后提醒我');
  assert.deepEqual(env.calls.filter((call) => call.operation === 'action').map((call) => call.body), [
    { interaction_id: INTERACTION_ID, ticket: 'ticket-4', action_id: 'approve' },
    { interaction_id: INTERACTION_ID, ticket: 'ticket-1', action_id: 'remind-later' }
  ]);
  await expect(env.page.getByRole('button', { name: '同意发布', exact: true })).toBeEnabled();
  await screenshot(env.page, 'submitted');
  assertCleanBrowser(env);
});

test('One to eight long custom buttons fit a narrow mobile screen', async (t) => {
  const buttons = Array.from({ length: 8 }, (_, index) => ({ id: 'action-' + index, text: '自定义操作' + (index + 1) + '：查看本次发布说明及审核意见' }));
  const env = await fixture(t, { exchangeResult: result(0, { buttons }) });
  await env.page.setViewportSize({ width: 320, height: 844 });
  await expect(env.page.locator('#actions button')).toHaveText(buttons.map((button) => button.text));
  assert.equal(await env.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  await screenshot(env.page, 'eight-buttons');
  assertCleanBrowser(env);
});

test('A legacy snapshot without buttons is visible but has no actions', async (t) => {
  const reply = result();
  delete reply.interaction.buttons;
  delete reply.ticket;
  const env = await fixture(t, { exchangeResult: reply });
  await expect(env.page.locator('#interaction-panel')).toBeVisible();
  await expect(env.page.locator('#actions button')).toHaveCount(0);
  await expect(env.page.locator('#action-hint')).toContainText('没有可用操作');
  assert.equal(env.calls.filter((call) => call.operation === 'action').length, 0);
  assertCleanBrowser(env);
});

test('Malformed or duplicate button definitions fail closed', async (t) => {
  const env = await fixture(t, { exchangeResult: result(0, { buttons: [{ id: 'approve', text: '同意' }, { id: 'approve', text: '拒绝' }] }) });
  await expect(env.page.locator('#auth-title')).toHaveText('未能完成身份验证');
  await expect(env.page.locator('#interaction-panel')).toBeHidden();
  await expect(env.page.locator('#login-button')).toBeEnabled();
  assertCleanBrowser(env);
});

test('An expired message remains readable and disables actions', async (t) => {
  const env = await fixture(t, { exchangeResult: result(0, { expires_at: '2000-01-01T00:00:00Z' }) });
  await expect(env.page.locator('#interaction-panel')).toBeVisible();
  for (const button of DEFAULT_BUTTONS) await expect(env.page.getByRole('button', { name: button.text, exact: true })).toBeDisabled();
  await expect(env.page.locator('#action-hint')).toContainText('超过有效期');
  assertCleanBrowser(env);
});

test('A failed action retries exactly the same ticket and action and distinguishes pending delivery', async (t) => {
  const env = await fixture(t, {
    onAction: async ({ number, route }) => {
      if (number === 1) { await route.abort('connectionfailed'); return null; }
      return { ...result(1, { last_action_id: 'approve' }), event_id: 'event-approve', event_status: 'pending' };
    }
  });
  await env.page.getByRole('button', { name: '同意发布', exact: true }).click();
  await expect(env.page.locator('#retry-button')).toBeVisible();
  await expect(env.page.locator('#retry-button')).toBeEnabled();
  for (const button of DEFAULT_BUTTONS) await expect(env.page.getByRole('button', { name: button.text, exact: true })).toBeDisabled();
  await env.page.locator('#retry-button').click();
  await expect(env.page.locator('#last-action')).toHaveText('同意发布');
  await expect(env.page.locator('#notice-text')).toHaveText('操作已提交，通知待同步。后续处理以业务系统或工作流通知为准。');
  await expect(env.page.locator('#retry-button')).toBeHidden();
  await expect(env.page.getByRole('button', { name: '同意发布', exact: true })).toBeEnabled();
  const attempts = env.calls.filter((call) => call.operation === 'action');
  assert.equal(attempts.length, 2);
  assert.deepEqual(attempts[0].body, { interaction_id: INTERACTION_ID, ticket: 'ticket-0', action_id: 'approve' });
  assert.deepEqual(attempts[1].body, attempts[0].body);
  assertCleanBrowser(env);
});

for (const status of [403, 409, 410]) {
  test('An action response ' + status + ' invalidates its ticket and permits manual reauthorization', async (t) => {
    const env = await fixture(t, {
      onAction: async ({ route }) => {
        await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ error: '操作凭证已失效，请重新验证身份。' }) });
        return null;
      }
    });
    await env.page.getByRole('button', { name: '同意发布', exact: true }).click();
    await expect(env.page.locator('#reauth-button')).toBeVisible();
    await expect(env.page.locator('#retry-button')).toBeHidden();
    for (const button of DEFAULT_BUTTONS) await expect(env.page.getByRole('button', { name: button.text, exact: true })).toBeDisabled();
    await env.page.locator('#reauth-button').click();
    await expect.poll(() => env.authorizations.length).toBe(1);
    assert.deepEqual(env.calls.map((call) => call.operation), ['exchange', 'action', 'login']);
    assertCleanBrowser(env);
  });
}

test('A timed-out action locks other buttons and retries the original ticket after an uncertain result', async (t) => {
  const env = await fixture(t, {
    onAction: async ({ number }) => number === 1 ? null : { ...result(1, { last_action_id: 'approve' }), event_id: 'event-timeout', event_status: 'submitted' }
  });
  await expect(env.page.getByRole('button', { name: '同意发布', exact: true })).toBeEnabled();
  await env.page.clock.install();
  await env.page.getByRole('button', { name: '同意发布', exact: true }).click();
  await expect.poll(() => env.calls.filter((call) => call.operation === 'action').length).toBe(1);
  await env.page.clock.runFor(20001);
  await expect(env.page.locator('#notice-text')).toContainText('请求超时');
  await expect(env.page.locator('#retry-button')).toBeEnabled();
  await env.page.locator('#retry-button').click();
  await expect(env.page.locator('#last-action')).toHaveText('同意发布');
  const attempts = env.calls.filter((call) => call.operation === 'action');
  assert.deepEqual(attempts[1].body, attempts[0].body);
  assertCleanBrowser(env);
});
