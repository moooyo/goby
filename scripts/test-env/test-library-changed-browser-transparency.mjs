#!/usr/bin/env node
/** Exercise native browser callbacks against owned HTTP/WS fixtures in a private network namespace. */
import fs from 'node:fs/promises';
import http from 'node:http';
import path from 'node:path';
import { createHash, randomBytes } from 'node:crypto';
import { createRequire } from 'node:module';
import { fileURLToPath, pathToFileURL } from 'node:url';

const WORK = '/opt/goby-test/exec-work-m3e';
const TOOL = WORK + '/library-changed-browser-transparency-tool-02';
const OUTPUT = WORK + '/library-changed-browser-transparency-execution-02';
const UNIT = 'goby-library-changed-browser-transparency-02.service';
const SELF = fileURLToPath(import.meta.url);
const NODE = WORK + '/client-library-changed-source44-tool-01/node';
const NODE_SHA = '3517c2df0b2f8cd7f422b4b8450ef81c6889f08eb03e281d6de9079b15e6a327';
const PLAYWRIGHT = '/opt/goby-test/inactive-dependencies-m5h/node_modules/playwright';
const BROWSER = '/root/.cache/ms-playwright/chromium_headless_shell-1243/chrome-headless-shell-linux64/chrome-headless-shell';
const FILES = ['test-library-changed-browser-transparency.mjs', 'client-browser-library-changed-reference-runtime.mjs',
  'client-browser-library-changed-reference.mjs', 'client-browser-session-proof.mjs', 'client-browser-cross-user.mjs',
  'client-browser-library-home.mjs', 'client-browser-library-changed-source55.mjs', 'client-browser-goby-fixture.mjs',
  'client-browser-special-features-fixture.mjs', 'client-library-changed-source55-fixture.mjs'];
const PINNED = {
  'client-browser-library-changed-reference-runtime.mjs': '209f72fa812647742003b50c311ee358ffc6f1279e29a2e142eec506245fed7c',
  'client-browser-library-changed-reference.mjs': '965e5e2f0d4a35ef8402b2a79660e0be114344073d282f52886733b06b1f5c5c',
  'client-browser-cross-user.mjs': '60dea05efc3091cc8574f08867d92eff8472a256747fb9a521cb667190197d4e',
  'client-browser-library-changed-source55.mjs': '39e4aa077c6007b3964469f4791174875b280d99659282a6656d77846bf485d0',
};
const REF = { port: 18197, user: 'c5f36699a54f4971a891682cd9de410f', server: 'f56dec8ff7414847873064c4be9fba74',
  target: '100', anchor: '96', library: '93' };
const GOBY = { port: 18196, user: 'ecbbe4cb82403879bc4b4f78894c5738', server: 'c7cfd76b1dee728b2bad523793a37ccb',
  target: '268051d3ca734aefcf94e245fb25ad55', anchor: '11111111111111111111111111111111', library: 'a9993591e72f0f2e7babcbf8b9c50790' };
const LIBRARIES = ['a9993591e72f0f2e7babcbf8b9c50790', '6383d20008836e137559698c29b10395',
  'a34ce665fb75421ef7551570f353d705', '57a85c1ca5b6c7ae602c587755250b2f'].map((id, index) => ({ id, name: `Owned Library ${index + 1}` }));
const BEFORE = 'Owned Movie Before', AFTER = 'Owned Movie After', ANCHOR = 'Owned Movie Anchor';
const MESSAGE_IDS = ['1'.padStart(32, '0'), '2'.padStart(32, '0')];
const sha = value => createHash('sha256').update(value).digest('hex');
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
function need(value, code) { if (!value) throw new Error('transparency_' + code); }
async function bounded(promise, ms, code) {
  let timer;
  try { return await Promise.race([promise, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('transparency_' + code)), ms); })]); }
  finally { clearTimeout(timer); }
}
async function until(predicate, ms, code) {
  const end = Date.now() + ms;
  while (Date.now() < end) { if (await predicate()) return; await delay(25); }
  throw new Error('transparency_' + code);
}
function safeFailure(error) {
  return /^transparency_[a-z_]+$/.test(error?.message ?? '') ? error.message : 'transparency_operation_failed';
}
async function writeJSON(name, value) {
  need(/^[a-z0-9-]+\.json$/.test(name), 'output_name');
  const bytes = Buffer.from(JSON.stringify(value, null, 2) + '\n');
  const handle = await fs.open(OUTPUT + '/' + name, 'wx', 0o600);
  try { await handle.writeFile(bytes); await handle.sync(); } finally { await handle.close(); }
  return { path: OUTPUT + '/' + name, sha256: sha(bytes) };
}
async function isolation() {
  need(process.platform === 'linux' && process.getuid?.() === 0, 'remote_linux_required');
  need(SELF === TOOL + '/' + FILES[0] && process.argv.length === 2, 'entrypoint');
  need(!process.env.NODE_OPTIONS && !process.env.DEBUG && !process.env.PWDEBUG &&
    process.env.PLAYWRIGHT_BROWSERS_PATH === '/root/.cache/ms-playwright', 'environment');
  need(/^[0-9a-f]{32}$/.test(process.env.INVOCATION_ID ?? ''), 'invocation');
  const cgroup = (await fs.readFile('/proc/self/cgroup', 'utf8')).trim();
  need(cgroup === '0::/system.slice/' + UNIT, 'unit');
  const network = await fs.readlink('/proc/self/ns/net'), hostNetwork = await fs.readlink('/proc/1/ns/net');
  need(/^net:\[\d+\]$/.test(network) && network !== hostNetwork, 'private_network');
  return { unit: UNIT, invocation_id: process.env.INVOCATION_ID, pid: process.pid, cgroup,
    network_namespace: network, host_network_namespace: hostNetwork, private_network: true };
}
async function inputs() {
  const boundary = await isolation();
  for (const directory of [TOOL, OUTPUT]) {
    const info = await fs.lstat(directory);
    need(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0 && (info.mode & 0o777) === 0o700, 'directory_identity');
  }
  const manifestBytes = await fs.readFile(TOOL + '/manifest.json'), manifest = JSON.parse(manifestBytes);
  need(manifest.unit === UNIT && manifest.output === OUTPUT && manifest.tool === TOOL &&
    JSON.stringify(Object.keys(manifest.sources).sort()) === JSON.stringify([...FILES].sort()), 'manifest');
  for (const name of FILES) {
    const filename = TOOL + '/' + name, info = await fs.lstat(filename);
    need(info.isFile() && !info.isSymbolicLink() && info.uid === 0 && info.nlink === 1 && (info.mode & 0o022) === 0, 'source_file');
    const digest = sha(await fs.readFile(filename));
    need(digest === manifest.sources[name] && (!PINNED[name] || digest === PINNED[name]), 'source_hash');
  }
  need(await fs.readlink('/proc/self/exe') === NODE && sha(await fs.readFile(NODE)) === NODE_SHA, 'node_identity');
  need(manifest.browser.path === BROWSER && sha(await fs.readFile(BROWSER)) === manifest.browser.sha256, 'browser_identity');
  const packageBytes = await fs.readFile(PLAYWRIGHT + '/package.json');
  need(sha(packageBytes) === manifest.playwright.sha256 && JSON.parse(packageBytes).version === '1.63.0', 'playwright_identity');
  return { boundary, manifest, manifest_sha256: sha(manifestBytes) };
}

/** This script is the application under test, not an injected WebSocket wrapper. */
function ownedPage(config) {
  const form = document.querySelector('form'), app = document.getElementById('application');
  const title = document.getElementById('target-title'), signOut = document.getElementById('sign-out');
  const state = { ready: false, events: [], updates: [], errors: [], closed: null, pending: 0, ticks: 0,
    mutations: [], focus_events: 0, selection_events: 0, scroll_events: 0 };
  window.ownedTransparency = state;
  let token, socket, ticker, observer;
  const headers = () => ({ 'X-Emby-Token': token });
  function record(handler, event, receiver) {
    const parsed = JSON.parse(event.data);
    state.events.push({ handler, message_id: parsed.MessageId, data_type: typeof event.data,
      native_message_event: event instanceof MessageEvent, constructor_matches: event.constructor === MessageEvent,
      trusted: event.isTrusted, type: event.type, this_is_socket: receiver === socket, target_is_socket: event.target === socket,
      current_target_is_socket: event.currentTarget === socket, native_socket: socket instanceof WebSocket,
      origin_matches: event.origin === new URL(socket.url).origin, bubbles: event.bubbles, cancelable: event.cancelable,
      default_prevented: event.defaultPrevented, data_matches: parsed.MessageType === 'LibraryChanged' &&
        parsed.Data.ItemsUpdated.length === 1 && parsed.Data.ItemsUpdated[0] === config.target });
    return parsed;
  }
  async function refresh(handler, message) {
    state.pending += 1;
    try {
      const url = '/Users/' + config.user + '/Items/' + config.target + '?Handler=' + handler + '&MessageId=' + message.MessageId;
      const response = await fetch(url, { headers: headers(), cache: 'no-store', credentials: 'omit' });
      if (response.status !== 200) throw new Error('owned_response_status');
      const dto = await response.json();
      if (dto.Id !== config.target || dto.Type !== 'Movie' || dto.TransparencyMessageId !== message.MessageId) throw new Error('owned_response_identity');
      title.textContent = dto.Name;
      state.updates.push({ handler, message_id: message.MessageId, name: dto.Name, response_consumed: true });
    } catch { state.errors.push('callback_fetch_failed'); }
    finally { state.pending -= 1; }
  }
  form.addEventListener('submit', async event => {
    event.preventDefault();
    try {
      const body = new URLSearchParams({ Username: form.querySelector('input[type=text]').value, Pw: form.querySelector('input[type=password]').value });
      const response = await fetch('/Users/AuthenticateByName', { method: 'POST', body,
        headers: { 'X-Emby-Client': config.client, 'X-Emby-Device-Name': config.device_name,
          'X-Emby-Device-Id': config.device_id, 'X-Emby-Client-Version': '1.0' } });
      if (response.status !== 200) throw new Error('owned_login');
      token = (await response.json()).AccessToken;
      form.hidden = true; app.hidden = false;
      location.hash = '!/videos?serverId=' + config.server + '&parentId=' + config.library;
      const views = await fetch('/Users/' + config.user + '/Views', { headers: headers() });
      if (views.status !== 200) throw new Error('owned_views');
      await views.json();
      socket = new WebSocket('ws://127.0.0.1:' + config.port + '/embywebsocket?api_key=' + token);
      socket.onmessage = function (event) {
        try { const message = record('property', event, this); if (message.MessageId.endsWith('1')) void refresh('property', message); }
        catch { state.errors.push('property_callback_failed'); }
      };
      socket.addEventListener('message', function (event) {
        try { const message = record('listener', event, this); if (message.MessageId.endsWith('2')) void refresh('listener', message); }
        catch { state.errors.push('listener_callback_failed'); }
      });
      socket.addEventListener('close', event => {
        state.closed = { code: event.code, clean: event.wasClean, native_close_event: event instanceof CloseEvent };
        console.info('OWNED_NATIVE_CLOSE ' + JSON.stringify(state.closed));
      });
      socket.addEventListener('error', () => { state.errors.push('native_socket_error'); });
      socket.addEventListener('open', () => {
        observer = new MutationObserver(records => {
          for (const row of records) state.mutations.push({ type: row.type, target: row.target.id });
        });
        observer.observe(app, { attributes: true, childList: true, characterData: true, subtree: true });
        document.addEventListener('focusin', () => { state.focus_events += 1; });
        document.addEventListener('focusout', () => { state.focus_events += 1; });
        document.addEventListener('selectionchange', () => { state.selection_events += 1; });
        window.addEventListener('scroll', () => { state.scroll_events += 1; });
        ticker = setInterval(() => { state.ticks += 1; }, 50);
        state.ready = true;
      });
    } catch { state.errors.push('login_failed'); }
  });
  signOut.addEventListener('click', async () => {
    try {
      observer?.disconnect(); clearInterval(ticker);
      const response = await fetch('/Sessions/Logout', { method: 'POST', headers: headers() });
      if (response.status !== 204) throw new Error('owned_logout');
      const closed = new Promise((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error('owned_close_timeout')), 3000);
        socket.addEventListener('close', () => { clearTimeout(timer); resolve(); }, { once: true });
      });
      socket.close(1000, 'owned logout'); await closed;
      app.hidden = true; form.hidden = false;
    } catch { state.errors.push('logout_failed'); }
  });
}

function pageHTML(config) {
  const card = (id, name, titleID) => `<div class="card" data-id="${id}" data-type="Movie"><div class="cardBox"><div class="cardText"><button class="cardTextActionButton" id="${titleID}">${name}</button></div></div></div>`;
  return '<!doctype html><html lang="en"><meta charset="utf-8"><title>Owned browser transparency fixture</title>' +
    '<link rel="icon" href="data:,"><style>body{font:18px sans-serif;margin:30px}input,button{font:inherit;margin:8px;padding:8px}' +
    '.itemsContainer{display:flex;gap:20px}.card{width:300px;min-height:140px;border:1px solid #333}.cardBox,.cardText{padding:12px}[hidden]{display:none!important}</style>' +
    '<form><label>Username<input type="text"></label><label>Password<input type="password"></label><button type="submit">Sign In</button></form>' +
    `<main id="application" hidden><button id="sign-out">Sign Out</button><div class="itemsContainer" data-id="${config.library}">` +
    card(config.target, BEFORE, 'target-title') + card(config.anchor, ANCHOR, 'anchor-title') + '</div></main>' +
    '<script>(' + ownedPage.toString() + ')(' + JSON.stringify(config) + ');</script></html>';
}
function wireFrame(payload, opcode = 1) {
  const bytes = Buffer.isBuffer(payload) ? payload : Buffer.from(payload);
  need(bytes.length <= 65535, 'fixture_frame_size');
  const header = Buffer.alloc(bytes.length < 126 ? 2 : 4);
  header[0] = 0x80 | opcode; header[1] = bytes.length < 126 ? bytes.length : 126;
  if (bytes.length >= 126) header.writeUInt16BE(bytes.length, 2);
  return Buffer.concat([header, bytes]);
}

async function fixture(config, account, token) {
  await isolation();
  const sockets = new Set(), websocket = new Set(), ledger = [], socketFacts = [];
  const html = pageHTML(config);
  let active = false, currentName = BEFORE, currentID = null, requestNumber = 0;
  function reply(response, status, content = null, type = 'application/json') {
    const body = content === null ? Buffer.alloc(0) : Buffer.from(type === 'application/json' ? JSON.stringify(content) : content);
    response.writeHead(status, { 'Content-Type': type, 'Content-Length': String(body.length), 'Cache-Control': 'no-store' }); response.end(body);
  }
  const server = http.createServer(async (request, response) => {
    const row = { index: ++requestNumber, method: request.method, route: null, kind: null, status: null };
    ledger.push(row);
    try {
      const url = new URL(request.url, 'http://127.0.0.1:' + config.port), route = url.pathname.replace(/^\/emby(?=\/)/, ''); row.route = route;
      const chunks = []; let length = 0;
      for await (const chunk of request) { length += chunk.length; need(length <= 8192, 'fixture_request_size'); chunks.push(chunk); }
      const body = Buffer.concat(chunks);
      if (route === '/web/index.html' && request.method === 'GET') { row.kind = 'owned_html'; row.status = 200; reply(response, 200, html, 'text/html'); return; }
      if (route === '/Users/AuthenticateByName' && request.method === 'POST') {
        const values = new URLSearchParams(body.toString());
        need(values.size === 2 && values.get('Username') === account.username && values.get('Pw') === account.password && !active, 'fixture_login_body');
        need(request.headers['x-emby-client'] === config.client && request.headers['x-emby-device-name'] === config.device_name &&
          request.headers['x-emby-device-id'] === config.device_id && request.headers['x-emby-client-version'] === '1.0', 'fixture_login_metadata');
        active = true; row.kind = 'login'; row.status = 200;
        reply(response, 200, { AccessToken: token, ServerId: config.server,
          User: { Id: config.user, Name: account.username, ServerId: config.server, HasPassword: true, Policy: { IsAdministrator: false, IsDisabled: false } },
          SessionInfo: { Id: randomBytes(16).toString('hex'), UserId: config.user, UserName: account.username, ServerId: config.server,
            Client: config.client, DeviceName: config.device_name, DeviceId: config.device_id, ApplicationVersion: '1.0', LastActivityDate: new Date().toISOString() } }); return;
      }
      const supplied = request.headers['x-emby-token'];
      need(supplied === token, 'fixture_token');
      if (route === '/System/Info') { row.kind = 'logout_proof'; row.status = active ? 200 : 401; reply(response, row.status); return; }
      need(active, 'fixture_active_session');
      if (route === '/Users/' + config.user + '/Views' && request.method === 'GET') {
        row.kind = 'views'; row.status = 200; reply(response, 200, { Items: LIBRARIES.map(value => ({ Id: value.id, Name: value.name })), TotalRecordCount: 4 }); return;
      }
      if (route === '/Users/' + config.user + '/Items/' + config.target && request.method === 'GET') {
        need(MESSAGE_IDS.includes(url.searchParams.get('MessageId')) && url.searchParams.get('MessageId') === currentID &&
          ['property', 'listener'].includes(url.searchParams.get('Handler')), 'fixture_callback_query');
        row.kind = 'callback_get'; row.message_id = currentID; row.handler = url.searchParams.get('Handler'); row.status = 200;
        const dto = { Id: config.target, Name: currentName, Type: 'Movie', IsFolder: false, ParentId: config.library, TransparencyMessageId: currentID };
        row.response = dto; row.response_sha256 = sha(JSON.stringify(dto)); reply(response, 200, dto); return;
      }
      if (route === '/Sessions/Logout' && request.method === 'POST') {
        need(body.length === 0, 'fixture_logout_body'); active = false; row.kind = 'logout'; row.status = 204; reply(response, 204); return;
      }
      throw new Error('transparency_fixture_route');
    } catch (error) { row.failure = safeFailure(error); row.status = 500; reply(response, 500); }
  });
  server.requestTimeout = 10000; server.headersTimeout = 5000;
  server.on('connection', socket => { sockets.add(socket); socket.once('close', () => sockets.delete(socket)); });
  server.on('upgrade', (request, socket, head) => {
    let pending = Buffer.from(head), closed = false;
    const facts = { opened: false, received_close: false, sent_close: false, closed: false, errors: [] }; socketFacts.push(facts);
    try {
      const url = new URL(request.url, 'http://127.0.0.1:' + config.port);
      need(active && url.pathname === '/embywebsocket' && url.searchParams.get('api_key') === token &&
        request.headers.origin === 'http://127.0.0.1:' + config.port && request.headers['sec-websocket-version'] === '13', 'fixture_upgrade');
      const key = request.headers['sec-websocket-key']; need(typeof key === 'string', 'fixture_upgrade_key');
      const accept = createHash('sha1').update(key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64');
      socket.write('HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ' + accept + '\r\n\r\n');
      websocket.add(socket); facts.opened = true;
      const read = chunk => {
        try {
          pending = Buffer.concat([pending, chunk]); need(pending.length <= 65536, 'fixture_client_frame_limit');
          while (pending.length >= 2) {
            const opcode = pending[0] & 15; let length = pending[1] & 127, offset = 2;
            need((pending[0] & 0xf0) === 0x80 && (pending[1] & 128) !== 0 && [8, 9, 10].includes(opcode), 'fixture_client_frame');
            if (length === 126) { if (pending.length < 4) return; length = pending.readUInt16BE(2); offset = 4; }
            need(length <= 125, 'fixture_control_size'); if (pending.length < offset + 4 + length) return;
            const mask = pending.subarray(offset, offset + 4), payload = Buffer.from(pending.subarray(offset + 4, offset + 4 + length));
            for (let index = 0; index < payload.length; index++) payload[index] ^= mask[index % 4];
            pending = Buffer.from(pending.subarray(offset + 4 + length));
            if (opcode === 8) {
              need(!closed && payload.length >= 2 && payload.readUInt16BE(0) === 1000, 'fixture_close');
              closed = true; facts.received_close = true; facts.sent_close = true; socket.write(wireFrame(payload, 8));
            } else if (opcode === 9) socket.write(wireFrame(payload, 10));
          }
        } catch (error) { facts.errors.push(safeFailure(error)); socket.destroy(); }
      };
      socket.on('data', read); socket.on('end', () => socket.end()); read(Buffer.alloc(0));
    } catch (error) { facts.errors.push(safeFailure(error)); socket.destroy(); }
    socket.on('error', () => { facts.errors.push('fixture_socket_error'); });
    socket.once('close', () => { facts.closed = true; websocket.delete(socket); });
  });
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(config.port, '127.0.0.1', resolve); });
  return { server, sockets, ledger, socketFacts, html_sha256: sha(html),
    async emit(index) {
      need(active && websocket.size === 1, 'fixture_one_socket'); currentID = MESSAGE_IDS[index]; currentName = index === 0 ? AFTER : BEFORE;
      const message = { MessageType: 'LibraryChanged', MessageId: currentID,
        Data: { FoldersAddedTo: [], FoldersRemovedFrom: [], ItemsAdded: [], ItemsRemoved: [], ItemsUpdated: [config.target], CollectionFolders: [], IsEmpty: false } };
      const bytes = Buffer.from(JSON.stringify(message));
      const socket = [...websocket][0]; await new Promise((resolve, reject) => socket.write(wireFrame(bytes), error => error ? reject(error) : resolve()));
      return { message_id: currentID, body_sha256: sha(bytes), body_bytes: bytes.length, name: currentName };
    },
    async close() {
      for (const socket of sockets) socket.destroy();
      await bounded(new Promise(resolve => server.close(resolve)), 3000, 'fixture_close_timeout');
      await until(() => sockets.size === 0 && websocket.size === 0, 2000, 'fixture_socket_cleanup');
      return { server_listening: server.listening, sockets_remaining: sockets.size, websocket_remaining: websocket.size, session_revoked: !active };
    } };
}

function publicPage(state) {
  need(state && state.errors.length === 0 && state.pending === 0 && state.events.length === 4 && state.updates.length === 2, 'page_complete');
  for (const event of state.events) need(event.data_type === 'string' && event.type === 'message' && event.native_message_event &&
    event.constructor_matches && event.trusted && event.this_is_socket && event.target_is_socket && event.current_target_is_socket &&
    event.native_socket && event.origin_matches && event.data_matches && !event.bubbles && !event.cancelable && !event.default_prevented, 'native_event_semantics');
  need(state.events.map(value => value.handler).join(',') === 'property,listener,property,listener', 'listener_order');
  need(state.updates[0].handler === 'property' && state.updates[0].name === AFTER && state.updates[1].handler === 'listener' &&
    state.updates[1].name === BEFORE && state.updates.every(value => value.response_consumed), 'callback_updates');
  need(state.mutations.length === 2 && state.mutations.every(value => value.type === 'childList' && value.target === 'target-title'), 'page_mutations');
  return state;
}
async function runCase(definition, modules, chromium) {
  const started = Date.now(), report = { name: definition.name, mode: definition.mode, sampler_active: definition.sampler, result: 'running',
    started_at: new Date(started).toISOString(), events: [], samples: [], checkpoints: [], current_phase: 'setup', cleanup: null, failure: null };
  const config = { ...(definition.mode === 'goby' ? GOBY : REF), client: 'Owned Transparency Browser', device_name: 'Owned Device',
    device_id: 'owned-transparency-' + randomBytes(12).toString('hex') };
  const account = { slot: 'B', id: config.user, username: 'Owned Transparency Viewer', password: randomBytes(32).toString('hex'), credentialsSHA: sha('owned-only') };
  const token = randomBytes(32).toString('hex'), password = account.password, actorReport = {}, resourceFactories = new Set();
  let upstream, actor, browser, context, page, collector, login, sampling = null, stopSampling = false, sampleFailure = null, aborted = false;
  const active = () => need(!aborted && Date.now() - started < 58000, 'case_aborted');
  const pin = async () => { await isolation(); need(Date.now() - started < 58000, 'case_deadline'); return true; };
  const acquire = (create, close) => {
    active();
    const operation = Promise.resolve().then(create).then(async resource => {
      if (aborted) { await bounded(Promise.resolve().then(() => close(resource)), 2000, 'late_resource_close').catch(() => {}); active(); }
      return resource;
    });
    resourceFactories.add(operation); operation.then(() => resourceFactories.delete(operation), () => resourceFactories.delete(operation));
    return operation;
  };
  const readPage = () => page.evaluate(() => JSON.parse(JSON.stringify(window.ownedTransparency)));
  async function checkpoint(stage) {
    const state = await readPage(), title = page.locator('#target-title');
    const value = { stage, elapsed_ms: Date.now() - started, page: state,
      visible_title: { visible: await title.isVisible(), text: await title.innerText() },
      sent_messages: report.events, upstream: upstream.ledger, websockets: upstream.socketFacts };
    need(!JSON.stringify(value).includes(token) && !JSON.stringify(value).includes(password), 'checkpoint_secret');
    report.page = state; report.checkpoints.push(await writeJSON(definition.name + '-' + stage + '.json', value));
    return state;
  }
  async function visibleTitle(expected) {
    const control = page.locator('#target-title');
    need(await control.isVisible() && await control.innerText() === expected, 'visible_title');
    (report.visible_titles ??= []).push(expected);
  }
  async function work() {
    upstream = await acquire(() => fixture(config, account, token), value => value.close()); report.html_sha256 = upstream.html_sha256;
    if (definition.mode === 'baseline') {
      await isolation(); browser = await acquire(() => chromium.launch({ executablePath: BROWSER, headless: true, timeout: 15000,
        args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-background-networking'] }), value => value.close());
      context = await acquire(() => browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', serviceWorkers: 'allow' }), value => value.close());
      page = await acquire(() => context.newPage(), value => value.close()); await page.goto('http://127.0.0.1:' + config.port + '/web/index.html', { waitUntil: 'domcontentloaded', timeout: 10000 }); active();
      await page.locator('input[type=text]').fill(account.username); await page.locator('input[type=password]').fill(account.password);
      await page.getByRole('button', { name: 'Sign In', exact: true }).click();
    } else if (definition.mode === 'reference') {
      actor = await acquire(() => modules.reference.createReferenceBrowserActor({ account, existingDeviceIDs: [], pin, observer: {}, report: actorReport,
        onLogin: ({ token: observed, proof }) => { need(observed === token && proof.request_metadata_matches, 'reference_login_proof'); } }), value => value.close());
      page = actor.page; context = actor.context; await actor.loginUI(); active(); actor.setPhase('transparency');
    } else {
      const input = { target: { id: config.target, name: BEFORE, library_id: config.library }, expected_libraries: LIBRARIES };
      login = new modules.home.HomeViewsObserver({ account, expected: LIBRARIES, existingDevices: [], existingSessions: [], existingTokens: [],
        serverId: config.server, started, scope: 'permission',
        onProven: ({ token: observed, proof }) => { need(observed === token, 'goby_login_token'); actor.token = observed; actor.proven = true;
          actor.report.token_fingerprint = proof.token_sha256; actor.report.ordinary_authority_confirmed = true; },
        persistLogin: async value => { need(value.token === token, 'goby_private_login'); return { path: 'owned-memory-only', sha256: sha(JSON.stringify(value.proof)) }; } });
      collector = new modules.source55.LibraryChangedObserver({ input, login });
      actor = modules.cross.createLibraryChangedSource55BrowserActor({ account, pin, report: actorReport, observer: collector.hooks() });
      collector.setActor(actor); actor.libraryChangedTargetName = BEFORE;
      actor.authorizeSocket = async () => { await modules.home.waitHomeLoginProof(login); need(actor.token === token, 'goby_socket_authority'); };
      await actor.open(); active(); page = actor.page; context = actor.context; await modules.home.waitHomeLoginProof(login); collector.attachPage(); collector.phase = 'transparency';
      page.on('console', message => {
        if (message.type() !== 'info' || !message.text().startsWith('OWNED_NATIVE_CLOSE ')) return;
        try {
          const value = JSON.parse(message.text().slice('OWNED_NATIVE_CLOSE '.length));
          if (Object.keys(value).sort().join(',') === 'clean,code,native_close_event') report.native_close = value;
        } catch { /* A malformed owned close record cannot satisfy the final assertion. */ }
      });
    }
    await until(async () => { active(); return (await readPage()).ready; }, 8000, 'page_ready');
    if (actor) await actor.settled();
    report.current_phase = 'before'; await checkpoint('before');
    report.observation_started_at = new Date().toISOString();
    if (definition.sampler) {
      sampling = (async () => {
        while (!stopSampling) {
          const sample = await modules.driver.observeReferenceDOM(page, { anchor: { name: ANCHOR } }, AFTER, null, actor.documentID);
          need(sample.structural_match && sample.visible_cards === 2 && sample.visible_title_buttons === 2 && sample.visible_card_containers === 1 &&
            sample.anchor_title_count === 1 && [BEFORE, AFTER].includes(sample.observed_title), 'sampler_structure');
          report.samples.push({ elapsed_ms: Date.now() - started, title: sample.observed_title, anchor: sample.observed_anchor_title,
            structural_match: sample.structural_match, cards: sample.visible_cards, owners: sample.visible_card_containers });
          need(report.samples.length <= 24, 'sample_limit'); await delay(500);
        }
      })().catch(error => { sampleFailure = safeFailure(error); });
    }
    await delay(750); active(); report.current_phase = 'forward'; report.events.push(await upstream.emit(0));
    await until(async () => { active(); return (await readPage()).updates.length === 1; }, 6000, 'first_callback');
    await checkpoint('forward');
    await visibleTitle(AFTER);
    await delay(1000); active(); report.current_phase = 'restored'; report.events.push(await upstream.emit(1));
    await until(async () => { active(); return (await readPage()).updates.length === 2; }, 6000, 'second_callback');
    await checkpoint('restored');
    await visibleTitle(BEFORE);
    await delay(750); active(); stopSampling = true; if (sampling) await sampling;
    need(!sampleFailure, 'sampler_failed');
    if (definition.sampler) need(report.samples.length >= 5 && report.samples.some(value => value.title === AFTER) &&
      report.samples[0].title === BEFORE && report.samples.at(-1).title === BEFORE, 'sampler_transitions');
    report.current_phase = 'assertions'; report.page = await checkpoint('observed'); publicPage(report.page);
    const requests = upstream.ledger.filter(value => value.kind === 'callback_get');
    need(requests.length === 2 && requests.every((value, index) => value.message_id === MESSAGE_IDS[index] && value.status === 200 &&
      value.handler === (index === 0 ? 'property' : 'listener')), 'upstream_callbacks');
    need(upstream.ledger.every(value => !value.failure), 'upstream_failure');
    if (definition.mode === 'reference') {
      await actor.settled(); const snapshot = actor.snapshot();
      const physical = snapshot.http.physical.filter(value => value.route === '/Users/' + config.user + '/Items/' + config.target);
      const frames = snapshot.http.frames.filter(value => value.route === '/Users/' + config.user + '/Items/' + config.target);
      const wires = snapshot.events.physical.filter(value => value.direction === 'server' && value.json?.MessageType === 'LibraryChanged');
      const received = snapshot.events.browser.filter(value => value.direction === 'server' && value.json?.MessageType === 'LibraryChanged');
      need(physical.length === 2 && frames.length === 2 && wires.length === 2 && received.length === 2, 'reference_capture_counts');
      for (let index = 0; index < 2; index++) need(physical[index].completed && frames[index].completed && physical[index].status === 200 && frames[index].status === 200 &&
        wires[index].forwarded && wires[index].body_sha256 === received[index].body_sha256 && wires[index].json.MessageId === MESSAGE_IDS[index] &&
        received[index].json.MessageId === MESSAGE_IDS[index] && physical[index].request_sequence > received[index].sequence &&
        frames[index].request_sequence > received[index].sequence, 'reference_ordered_capture');
      need(actorReport.observer_errors === 0 && actorReport.failures.length === 0 && (actorReport.page_error_count ?? 0) === 0, 'reference_actor_errors');
      report.production_evidence = { physical_http: physical, frame_http: frames, physical_messages: wires, browser_messages: received };
    } else if (definition.mode === 'goby') {
      await actor.settled(); await actor.proxyIdle(); collector.assertStable();
      need(collector.physical.length === 2 && collector.frames.length === 2 && collector.wireMessages.length === 2 && collector.browserMessages.length === 2, 'goby_capture_counts');
      for (let index = 0; index < 2; index++) need(collector.physical[index].completed && collector.frames[index].finished &&
        collector.physical[index].status === 200 && collector.frames[index].status === 200 && collector.wireMessages[index].forwarded &&
        collector.wireMessages[index].message.body_sha256 === collector.browserMessages[index].message.body_sha256 &&
        collector.physical[index].request_sequence > collector.browserMessages[index].sequence &&
        collector.frames[index].request_sequence > collector.browserMessages[index].sequence, 'goby_ordered_capture');
      report.production_evidence = { physical_http: collector.physical, frame_http: collector.frames,
        physical_messages: collector.wireMessages, browser_messages: collector.browserMessages };
    }
    report.current_phase = 'logout';
    if (definition.mode === 'baseline') {
      await page.getByRole('button', { name: 'Sign Out', exact: true }).click();
      await page.locator('input[type=password]').waitFor({ state: 'visible', timeout: 5000 });
      report.native_close = (await readPage()).closed;
    } else if (definition.mode === 'reference') {
      await actor.logoutUI(); report.native_close = (await readPage()).closed;
    }
    active(); report.result = 'passed';
  }
  try { await bounded(work(), 38000, 'case_timeout'); }
  catch (error) {
    aborted = true; report.failure = safeFailure(error); report.result = 'failed';
    page ??= actor?.page; context ??= actor?.context;
    if (page && !page.isClosed()) await bounded(checkpoint('failure'), 1000, 'failure_page_snapshot').catch(() => {});
  }
  finally {
    stopSampling = true;
    try {
      await bounded((async () => {
        if (sampling) await sampling;
        if (actor) {
          await actor.close();
          report.checkpoints.push(await writeJSON(definition.name + '-closed.json', { native_close: report.native_close ?? null,
            actor_closed: actorReport.closed, cleanup_failures: actorReport.cleanup_failures, session_proof: actorReport.session_proof,
            upstream: upstream?.ledger ?? [], websockets: upstream?.socketFacts ?? [] }));
          need(actorReport.closed && actorReport.cleanup_failures.length === 0, 'actor_cleanup');
          need(actorReport.session_proof?.outcome === 'all_observed_logout_tokens_rejected', 'logout_proof');
          report.actor_cleanup = { closed: actorReport.closed, failures: actorReport.cleanup_failures, proof: actorReport.session_proof,
            websocket: actorReport.websocket, home_closure: actorReport.home_closure ?? null };
        } else {
          if (page && !page.isClosed() && await page.getByRole('button', { name: 'Sign Out', exact: true }).isVisible()) {
            await page.getByRole('button', { name: 'Sign Out', exact: true }).click({ timeout: 3000 });
            await page.locator('input[type=password]').waitFor({ state: 'visible', timeout: 4000 });
            report.native_close = (await readPage()).closed;
          }
          await context?.close(); await browser?.close();
          report.checkpoints.push(await writeJSON(definition.name + '-closed.json', { native_close: report.native_close ?? null,
            browser_connected: browser?.isConnected() ?? false, upstream: upstream?.ledger ?? [], websockets: upstream?.socketFacts ?? [] }));
        }
      })(), 10000, 'browser_cleanup_timeout');
    } catch (error) {
      report.result = 'failed'; report.failure ??= safeFailure(error);
      await bounded(Promise.allSettled([context?.close(), context?.browser()?.close(), browser?.close(), actor?.context?.close(),
        actor?.context?.browser()?.close(), actor?.browser?.close()]), 1500, 'forced_browser_close').catch(() => {});
    }
    if (resourceFactories.size) await bounded(Promise.allSettled([...resourceFactories]), 500, 'resource_factories_pending').catch(() => {});
    report.pending_resource_factories = resourceFactories.size;
    collector?.dispose();
    if (upstream) {
      try { report.cleanup = await upstream.close(); }
      catch (error) { report.result = 'failed'; report.failure ??= safeFailure(error); }
      report.upstream = { requests: upstream.ledger, websockets: upstream.socketFacts };
    }
    if (report.result === 'passed') {
      try {
        need(report.native_close?.code === 1000 && report.native_close.clean === true && report.native_close.native_close_event === true, 'native_close');
        need(report.cleanup && !report.cleanup.server_listening && report.cleanup.sockets_remaining === 0 &&
          report.cleanup.websocket_remaining === 0 && report.cleanup.session_revoked, 'fixture_cleanup');
        need(Date.now() - started <= 60000, 'case_total_limit');
      } catch (error) { report.result = 'failed'; report.failure = safeFailure(error); }
    }
    report.elapsed_ms = Date.now() - started; report.completed_at = new Date().toISOString();
    if (JSON.stringify(report).includes(token) || JSON.stringify(report).includes(password)) {
      report.result = 'failed'; report.failure = 'transparency_public_secret'; delete report.production_evidence; delete report.actor_cleanup;
    }
    await writeJSON(definition.name + '-private-actor.json', actorReport);
    report.artifact = await writeJSON(definition.name + '.json', report);
  }
  return report;
}

async function main() {
  process.umask(0o077);
  const started = Date.now(), input = await inputs();
  const report = { marker: 'goby-library-changed-browser-transparency-v1', result: 'running', started_at: new Date(started).toISOString(),
    isolation: input.boundary, manifest_sha256: input.manifest_sha256, sources: input.manifest.sources,
    owned_page_logic_sha256: sha(ownedPage.toString()), limits: { cases: 4, case_ms: 60000, total_ms: 300000, outer_ms: 360000 },
    original_client_used: false, business_http: false, fixture_http: true, acceptance_workflow_executed: false,
    library_changed_client_acceptance: false, cases: [], failure: null };
  await writeJSON('started.json', report);
  const module = name => import(pathToFileURL(TOOL + '/' + name).href);
  const modules = { reference: await module('client-browser-library-changed-reference-runtime.mjs'),
    driver: await module('client-browser-library-changed-reference.mjs'), cross: await module('client-browser-cross-user.mjs'),
    home: await module('client-browser-library-home.mjs'), source55: await module('client-browser-library-changed-source55.mjs') };
  const { chromium } = createRequire(import.meta.url)(PLAYWRIGHT);
  try {
    for (const definition of [{ name: 'baseline', mode: 'baseline', sampler: false },
      { name: 'reference', mode: 'reference', sampler: false }, { name: 'reference-sampled', mode: 'reference', sampler: true },
      { name: 'goby-v7-actor', mode: 'goby', sampler: false }]) {
      need(Date.now() - started < 235000, 'total_admission');
      const result = await runCase(definition, modules, chromium);
      report.cases.push(result);
      if (result.result !== 'passed') throw new Error('transparency_case_failed');
    }
    const projection = value => ({ events: value.page.events, updates: value.page.updates, mutations: value.page.mutations, visible_titles: value.visible_titles });
    const baseline = JSON.stringify(projection(report.cases[0]));
    need(report.cases.every(value => JSON.stringify(projection(value)) === baseline), 'baseline_comparison');
    need(Date.now() - started <= 300000, 'total_limit'); report.result = 'passed'; report.baseline_equivalence = true;
    report.baseline_equivalence_scope = 'Synchronous native message semantics, listener order, callback response consumption, visible title transitions, and application DOM mutations';
    report.incidental_page_telemetry = report.cases.map(value => ({ name: value.name, ticks: value.page.ticks, focus_events: value.page.focus_events,
      selection_events: value.page.selection_events, scroll_events: value.page.scroll_events }));
  } catch (error) { report.result = 'failed'; report.failure = safeFailure(error); }
  report.completed_at = new Date().toISOString(); report.elapsed_ms = Date.now() - started;
  await writeJSON('report.json', report);
  process.stdout.write(JSON.stringify({ marker: report.marker, result: report.result, cases: report.cases.map(value => ({ name: value.name, result: value.result,
    elapsed_ms: value.elapsed_ms, failure: value.failure })), failure: report.failure }) + '\n');
  process.exit(report.result === 'passed' ? 0 : 1);
}

main().catch(async error => {
  const failure = safeFailure(error);
  if (SELF === TOOL + '/' + FILES[0]) await writeJSON('fatal.json', { result: 'failed', failure, completed_at: new Date().toISOString() }).catch(() => {});
  process.stderr.write(failure + '\n'); process.exit(1);
});
