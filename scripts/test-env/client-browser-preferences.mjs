#!/usr/bin/env node
/** Observe one reversible display preference change in the original client UI. */

import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawn } from 'node:child_process';

export async function runPreferencesWorkflow({ page, context, report, snapshot, target, redactSecrets, preferenceMode = 'round-trip' }) {
  if (!['round-trip', 'retain-for-restart', 'verify-restart-restore'].includes(preferenceMode)) {
    throw new Error('Unsupported display preference workflow mode.');
  }
  const result = report.preferences = { phase: 'open_settings', outcome: 'in_progress',
    mode: preferenceMode, writes: [], restored: false,
    scope: preferenceMode === 'round-trip' ? 'One display preference, changed and restored through the original UI'
      : 'One owned display preference retained or verified across an externally recorded process restart; all changes use the original UI' };
  const pending = new WeakMap();
  const safeValues = new Set(['UserSettings', 'DisplayPreferences', 'theme', 'Theme']);
  const sensitive = /password|passwd|token|api.?key|authorization|user.?id|device.?id|server.?id|session.?id|credential|secret/i;
  const displayKey = /theme|backdrop|displaymode|accentcolor|animation|darkmode|lightmode|color[sS]cheme|genre.*limit/i;
  let selected = null;
  let original = null;
  let changed = false;

  function safeBody(value, key = '', depth = 0) {
    if (depth > 6) return '{depth limit}';
    if (Array.isArray(value)) return value.slice(0, 64).map(item => safeBody(item, key, depth + 1));
    if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).slice(0, 64)
      .filter(([name]) => /^[A-Za-z][A-Za-z0-9_.-]{0,95}$/.test(name) && !sensitive.test(name))
      .map(([name, content]) => [name, safeBody(content, name, depth + 1)]));
    if (typeof value === 'string' && (safeValues.has(value) || /^[A-Za-z][A-Za-z0-9_.-]{0,95}$/.test(value) && displayKey.test(value))) return value;
    if (displayKey.test(key) && (typeof value === 'boolean' || typeof value === 'number' ||
        typeof value === 'string' && /^[A-Za-z0-9 _#.,-]{0,128}$/.test(value))) return value;
    if (value === null) return null;
    return `{redacted ${typeof value}}`;
  }

  function matches(request) {
    const url = new URL(request.url());
    return request.method() === 'POST' && url.origin === target.origin &&
      /^(?:\/emby)?\/(?:UserSettings\/[^/]+(?:\/Partial)?|Users\/[^/]+\/Configuration(?:\/Partial)?|DisplayPreferences\/[^/]+|Users\/[^/]+\/TypedSettings\/[^/]+)\/?$/i.test(url.pathname);
  }

  context.on('request', request => {
    if (!matches(request) || result.writes.length >= 16) return;
    const url = new URL(request.url());
    const route = url.pathname.replace(/(\/UserSettings\/|\/Users\/|\/DisplayPreferences\/)[^/]+/i, '$1{account-or-preference-id}');
    const headers = request.headers();
    const entry = { phase: result.phase, route, content_type: headers['content-type'] ?? null,
      header_names: Object.keys(headers).slice(0, 64).sort(),
      query_field_names: [...new Set(url.searchParams.keys())].slice(0, 64)
        .map(name => /^[A-Za-z][A-Za-z0-9_.-]{0,95}$/.test(name) ? name : '{field}'),
      status: null };
    const bytes = request.postDataBuffer();
    if (bytes && bytes.length <= 65536) {
      try {
        if (entry.content_type?.startsWith('application/x-www-form-urlencoded')) {
          const parsed = Object.fromEntries(new URLSearchParams(bytes.toString('utf8')));
          entry.body_encoding = 'form';
          entry.safe_body = safeBody(parsed);
        } else {
          let parsed = JSON.parse(bytes.toString('utf8'));
          entry.body_encoding = 'json';
          entry.json_top_level_type = Array.isArray(parsed) ? 'array' : typeof parsed;
          let layers = 1;
          while (typeof parsed === 'string' && layers < 3 && /^[\[{]/.test(parsed.trim())) {
            parsed = JSON.parse(parsed);
            layers += 1;
          }
          entry.json_encoding_layers = layers;
          entry.safe_body = safeBody(parsed);
        }
      } catch { entry.body_encoding = 'not_a_supported_json_or_form_shape'; }
    } else entry.body_encoding = bytes ? 'body_exceeds_limit' : 'empty';
    result.writes.push(redactSecrets(entry));
    pending.set(request, result.writes[result.writes.length - 1]);
  });
  context.on('response', response => {
    const entry = pending.get(response.request());
    if (entry) entry.status = response.status();
  });

  async function inspect(name) {
    await snapshot(name);
    result.last_display_controls = await page.evaluate(() =>
      [...document.querySelectorAll('select,input[type="checkbox"],button,a')]
        .filter(element => element.getClientRects().length).slice(0, 140).map(element => ({
          tag: element.tagName.toLowerCase(), type: element.getAttribute('type'),
          class: typeof element.className === 'string' ? element.className : null,
          label: element.getAttribute('aria-label'), title: element.getAttribute('title'),
          labels: [...(element.labels ?? [])].map(label => label.innerText.trim()),
          text: (element.innerText ?? '').trim().slice(0, 160),
          options: element.tagName === 'SELECT' ? [...element.options].slice(0, 32).map(option => ({
            text: option.text, value: option.value, selected: option.selected, disabled: option.disabled })) : undefined,
        })));
  }

  async function saveIfNeeded(previous) {
    await page.waitForTimeout(500);
    if (result.writes.length === previous) {
      const save = page.getByRole('button', { name: /^Save$/i }).filter({ visible: true });
      if (await save.count() === 1) await save.click({ timeout: 8000 });
    }
    for (let attempt = 0; attempt < 30; attempt += 1) {
      if (result.writes.length > previous && result.writes.slice(previous).every(entry => entry.status !== null)) return;
      await page.waitForTimeout(100);
    }
  }

  async function signOut() {
    result.phase = 'sign_out';
    const settings = page.getByRole('button', { name: 'Settings', exact: true }).filter({ visible: true });
    if (await settings.count() !== 1) throw new Error('The original account menu is unavailable.');
    await settings.click({ timeout: 8000 });
    const signOut = page.getByRole('button', { name: /\bSign Out\b/i }).filter({ visible: true });
    await signOut.waitFor({ state: 'visible', timeout: 10000 });
    const logoutResponse = page.waitForResponse(response => response.request().method() === 'POST' &&
      new URL(response.url()).origin === target.origin && /^(?:\/emby)?\/Sessions\/Logout\/?$/i.test(new URL(response.url()).pathname),
      { timeout: 10000 }).catch(() => null);
    report.logout.attempted = true;
    await signOut.click({ timeout: 8000 });
    const response = await logoutResponse;
    if (!response || response.status() < 200 || response.status() >= 300) throw new Error('The original client logout was not accepted.');
    await Promise.any([
      page.getByText('Manual Login', { exact: true }).waitFor({ state: 'visible', timeout: 10000 }),
      page.locator('form:has(input[type="password"]:visible)').waitFor({ state: 'visible', timeout: 10000 }),
    ]);
    report.logout = { attempted: true, result: 'ui_logout_http_accepted_and_login_view_visible',
      http_status: response.status(), owned_session: 'ui_logout_observed' };
    await snapshot('preferences-signed-out');
  }

  try {
    const settings = page.getByRole('button', { name: 'Settings', exact: true }).filter({ visible: true });
    await settings.waitFor({ state: 'visible', timeout: 10000 });
    await settings.click({ timeout: 8000 });
    const appSettings = page.getByRole('button', { name: /\bApp Settings\b/i }).filter({ visible: true });
    await appSettings.click({ timeout: 10000 });
    await page.waitForTimeout(500);
    await inspect('preferences-app-settings');
    result.phase = 'display_settings';
    const display = page.getByText('Display', { exact: true }).filter({ visible: true });
    if (await display.count() === 1) {
      const action = display.locator('xpath=ancestor-or-self::*[self::button or self::a][1]');
      await action.click({ timeout: 8000 });
      await page.waitForTimeout(500);
    }
    await inspect('preferences-display-before');
    const selects = page.locator('select:visible');
    const candidates = [];
    for (let index = 0; index < await selects.count(); index += 1) {
      const candidate = selects.nth(index);
      const details = await candidate.evaluate(element => ({
        description: [...(element.labels ?? [])].map(label => label.innerText).join('\n'),
        options: [...element.options].map(option => ({ value: option.value, text: option.text, disabled: option.disabled })),
      }));
      if (/^Genre display limit(?:\n|$)/.test(details.description)) candidates.push({ candidate, details });
    }
    if (candidates.length !== 1) throw new Error('A unique original UI genre display limit selector was not available.');
    selected = candidates[0].candidate;
    original = await selected.inputValue();
    if (preferenceMode !== 'round-trip') {
      const expected = preferenceMode === 'retain-for-restart' ? '1' : '2';
      const desired = preferenceMode === 'retain-for-restart' ? '2' : '1';
      result.choice = { kind: 'genre_display_limit', original, expected, desired, baseline: '1' };
      result.phase = 'verify_restart_starting_value';
      if (original !== expected || !candidates[0].details.options.some(option => option.value === desired && !option.disabled)) {
        throw new Error('The owned restart preference state differs from its recorded expectation.');
      }
      result.starting_value_verified = true;
      safeValues.add(original);
      safeValues.add(desired);
      result.phase = preferenceMode === 'retain-for-restart' ? 'retain_display_preference' : 'restore_after_restart';
      const before = result.writes.length;
      await selected.selectOption(desired);
      changed = true;
      await saveIfNeeded(before);
      await inspect(`preferences-${preferenceMode}`);
      const writes = result.writes.slice(before);
      if (await selected.inputValue() !== desired || writes.length !== 1 || writes[0].status !== 204 ||
          !/\/UserSettings\/[^/]+\/Partial$/i.test(writes[0].route) ||
          writes[0].safe_body?.genreLimitOnDetails !== desired) {
        throw new Error('The single owned restart preference write was not verified.');
      }
      result.restored = preferenceMode === 'verify-restart-restore';
      result.retained_for_restart = preferenceMode === 'retain-for-restart';
      result.outcome = result.retained_for_restart ? 'nondefault_preference_retained_for_restart'
        : 'nondefault_preference_loaded_in_fresh_browser_and_restored';
      changed = false;
      await signOut();
      result.phase = 'complete';
      return;
    }
    const alternate = candidates[0].details.options.find(option => !option.disabled && option.value !== original && option.value === '2');
    if (!alternate) throw new Error('The bounded alternate genre display limit was unavailable.');
    safeValues.add(original);
    safeValues.add(alternate.value);
    result.choice = { kind: 'genre_display_limit', label: 'Genre display limit', original, alternate: alternate.value };
    result.phase = 'change_display_preference';
    let before = result.writes.length;
    await selected.selectOption(alternate.value);
    changed = true;
    await saveIfNeeded(before);
    await inspect('preferences-display-changed');
    if (await selected.inputValue() !== alternate.value) throw new Error('The visible display selection did not change.');
    result.phase = 'restore_display_preference';
    before = result.writes.length;
    await selected.selectOption(original);
    await saveIfNeeded(before);
    await inspect('preferences-display-restored');
    if (await selected.inputValue() !== original) throw new Error('The visible display selection was not restored.');
    const writes = result.writes.filter(entry => ['change_display_preference', 'restore_display_preference'].includes(entry.phase));
    if (!writes.some(entry => entry.phase === 'change_display_preference') ||
        !writes.some(entry => entry.phase === 'restore_display_preference') ||
        writes.some(entry => entry.status === null || entry.status < 200 || entry.status >= 300)) {
      throw new Error('Both original-client preference writes were not accepted.');
    }
    result.restored = true;
    changed = false;
    result.outcome = writes.some(entry => /\/UserSettings\//i.test(entry.route))
      ? 'user_settings_display_change_and_restore_observed' : 'display_change_used_a_different_observed_route';
    await signOut();
    result.phase = 'complete';
  } catch {
    result.blocked_phase = result.phase;
    result.outcome = 'blocked_at_observed_ui_step';
    await inspect('preferences-blocked').catch(() => {});
    if (!changed) {
      try { await signOut(); } catch { result.cleanup_result = 'ui_logout_not_completed'; }
    } else result.cleanup_result = 'display_state_unconfirmed_private_session_retained';
    throw new Error('The original display preferences workflow stopped at its recorded phase.');
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  if (process.platform !== 'linux') throw new Error('Run display preferences acceptance only on the authorized Linux environment.');
  const observer = fileURLToPath(new URL('./client-browser-observe.mjs', import.meta.url));
  const child = spawn(process.execPath, [observer, ...process.argv.slice(2), '--workflow', 'preferences'], { stdio: 'inherit' });
  child.on('error', () => { process.stderr.write('The guarded observer could not start.\n'); process.exitCode = 1; });
  child.on('exit', (code, signal) => { process.exitCode = signal ? 1 : code ?? 1; });
}
