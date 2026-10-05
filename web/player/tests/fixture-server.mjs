import { createServer } from 'node:http';
import { createReadStream, existsSync, statSync } from 'node:fs';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { dirname, extname, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createCatalogue, createUser, LIBRARIES, TOKEN, USER_ID } from './fixture-data.mjs';

const manualDemo = process.env.GOBY_FIXTURE_DEMO === '1' ? await import('./manual-demo.mjs') : undefined;

const directory = dirname(fileURLToPath(import.meta.url));
const appDirectory = resolve(directory, '..');
const artifactDirectory = resolve(appDirectory, '../../.artifacts/player-acceptance');
const handoffDirectory = resolve(process.env.GOBY_HANDOFF_DIR ?? 'D:/Code/design_handoff_goby_player');
const port = Number(process.env.GOBY_FIXTURE_PORT ?? 4174);
const cachedImages = new Map();
const unknownRequests = [];
let state;

function reset() {
  const catalogue = createCatalogue();
  state = { ...catalogue, user: createUser(), events: [], requests: [], failures: {}, empty: false, display: {}, preferenceRevision: 1, unknownRequests };
  manualDemo?.configureManualDemo(state);
}
reset();

function json(response, value, status = 200) {
  response.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store' });
  response.end(JSON.stringify(value));
}

async function bodyOf(request) {
  let text = '';
  for await (const chunk of request) text += chunk;
  if (!text) return {};
  try { return JSON.parse(text); } catch { return Object.fromEntries(new URLSearchParams(text)); }
}

function queryItems(searchParams, candidates = state.items) {
  let items = state.empty ? [] : [...candidates];
  const value = (key) => searchParams.get(key) ?? searchParams.get(key.toLowerCase());
  if (value('IncludeItemTypes')) items = items.filter((item) => value('IncludeItemTypes').split(',').includes(item.Type));
  if (value('Ids')) items = items.filter((item) => value('Ids').split(',').includes(item.Id));
  if (value('ParentId')) {
    const parent = value('ParentId');
    const library = LIBRARIES.find((entry) => entry.Id === parent);
    items = items.filter((item) => item.ParentId === parent || item.SeriesId === parent || library?.children.includes(item.SeriesId));
  }
  if (value('SearchTerm')) {
    const term = value('SearchTerm').toLocaleLowerCase();
    items = items.filter((item) => item.Name.toLocaleLowerCase().includes(term));
  }
  if (value('SeriesId')) items = items.filter((item) => item.SeriesId === value('SeriesId'));
  if (value('PersonIds')) items = items.filter((item) => item.People?.some((person) => value('PersonIds').split(',').includes(person.Id)));
  if (value('Genres')) items = items.filter((item) => item.Genres?.some((genre) => value('Genres').split('|').includes(genre)));
  if (value('GenreIds')) items = items.filter((item) => item.Genres?.some((genre) => value('GenreIds').split(',').includes(`genre-${genre}`)));
  if (value('Years')) items = items.filter((item) => value('Years').split(',').includes(String(item.ProductionYear)));
  if (value('IsPlayed')) items = items.filter((item) => Boolean(item.UserData?.Played) === (value('IsPlayed') === 'true'));
  if (value('IsFavorite') === 'true' || value('Filters')?.includes('IsFavorite')) items = items.filter((item) => item.UserData?.IsFavorite);
  if (value('Filters')?.includes('IsResumable')) items = items.filter((item) => (item.UserData?.PlaybackPositionTicks ?? 0) > 0 && !item.UserData.Played);
  if (manualDemo) items = manualDemo.filterManualDemoItems(items, searchParams, state.items);
  const sortKeys = (value('SortBy') ?? 'DateCreated').split(',').map((key) => key === 'SortName' ? 'Name' : key);
  const direction = value('SortOrder') === 'Ascending' || (!value('SortOrder') && value('SortBy')?.includes('IndexNumber')) ? 1 : -1;
  items.sort((a, b) => {
    for (const key of sortKeys) {
      const first = key === 'DatePlayed' ? a.UserData?.LastPlayedDate : a[key];
      const second = key === 'DatePlayed' ? b.UserData?.LastPlayedDate : b[key];
      const order = (typeof first === 'number' && typeof second === 'number' ? first - second : String(first ?? '').localeCompare(String(second ?? ''), 'zh-CN')) * direction;
      if (order) return order;
    }
    return 0;
  });
  const TotalRecordCount = items.length;
  const start = Number(value('StartIndex') ?? 0);
  const limit = Number(value('Limit') ?? 200);
  return { Items: items.slice(start, start + limit), TotalRecordCount };
}

function sendFile(request, response, path, contentType) {
  if (!existsSync(path)) return json(response, { error: `Fixture asset not found: ${path}` }, 404);
  const size = statSync(path).size;
  const range = request.headers.range?.match(/^bytes=(\d+)-(\d*)$/);
  const headers = { 'Content-Type': contentType, 'Accept-Ranges': 'bytes', 'Cache-Control': contentType.startsWith('text/html') ? 'no-store' : 'public, max-age=3600' };
  let start = 0;
  let end = size - 1;
  if (range) {
    start = Number(range[1]);
    end = range[2] ? Math.min(Number(range[2]), size - 1) : size - 1;
    if (start >= size || end < start) { response.writeHead(416, { 'Content-Range': `bytes */${size}` }); response.end(); return; }
    headers['Content-Range'] = `bytes ${start}-${end}/${size}`;
  }
  headers['Content-Length'] = String(end - start + 1);
  response.writeHead(range ? 206 : 200, headers);
  if (request.method === 'HEAD') response.end();
  else createReadStream(path, { start, end }).pipe(response);
}

async function sendPicsum(request, response, id, width, height) {
  const key = `${id}-${width}-${height}`;
  const path = resolve(artifactDirectory, `assets/${key}.jpg`);
  if (!existsSync(path)) {
    if (!cachedImages.has(key)) cachedImages.set(key, (async () => {
      const result = await fetch(`https://picsum.photos/id/${id}/${width}/${height}`, { signal: AbortSignal.timeout(30_000) });
      if (!result.ok) throw new Error(`Image ${key} returned ${result.status}`);
      await mkdir(dirname(path), { recursive: true });
      await writeFile(path, Buffer.from(await result.arrayBuffer()));
    })());
    await cachedImages.get(key);
  }
  sendFile(request, response, path, 'image/jpeg');
}

async function handle(request, response) {
  response.setHeader('Access-Control-Allow-Origin', '*');
  response.setHeader('Access-Control-Allow-Headers', 'Content-Type, X-Emby-Authorization, X-Emby-Token, Authorization, If-Match');
  response.setHeader('Access-Control-Allow-Methods', 'GET, HEAD, POST, DELETE, PUT, PATCH, OPTIONS');
  if (request.method === 'OPTIONS') { response.writeHead(204); response.end(); return; }
  const url = new URL(request.url, `http://127.0.0.1:${port}`);
  const path = decodeURIComponent(url.pathname);
  if (path === '/__fixture/health') return json(response, { status: 'ready', port });
  if (path === '/__fixture/reset' && request.method === 'POST') { reset(); return json(response, { status: 'reset' }); }
  if (path === '/__fixture/config' && request.method === 'POST') { const body = await bodyOf(request); Object.assign(state, body); return json(response, { status: 'configured' }); }
  if (path === '/__fixture/state') return json(response, { user: state.user, events: state.events, requests: state.requests, unknownRequests: state.unknownRequests, display: state.display, items: state.items });
  if (path === '/__fixture/clip.webm') return sendFile(request, response, resolve(directory, 'assets/sample.webm'), 'video/webm');
  if (path === '/__fixture/background.mp4') return sendFile(request, response, resolve(directory, 'assets/background.mp4'), 'video/mp4');
  const picsum = path.match(/^\/__fixture\/picsum\/id\/(\d+)\/(\d+)\/(\d+)$/);
  if (picsum) return sendPicsum(request, response, ...picsum.slice(1));
  if (path.startsWith('/reference/')) {
    const name = path.slice('/reference/'.length) || 'Goby-Player-A-v3.dc.html';
    const file = resolve(handoffDirectory, name);
    if (!file.startsWith(handoffDirectory + sep)) return json(response, { error: 'Invalid reference path.' }, 403);
    const content = await readFile(file, 'utf8');
    response.writeHead(200, { 'Content-Type': name.endsWith('.js') ? 'text/javascript; charset=utf-8' : 'text/html; charset=utf-8' });
    response.end(content.replaceAll('https://picsum.photos', '/__fixture/picsum'));
    return;
  }
  const route = path.replace(/^\/emby/, '') || '/';
  if (!path.startsWith('/emby')) {
    const requested = resolve(appDirectory, 'dist', `.${path}`);
    const dist = resolve(appDirectory, 'dist');
    if (!requested.startsWith(dist + sep) && requested !== dist) return json(response, { error: 'Invalid asset path.' }, 403);
    const file = existsSync(requested) && statSync(requested).isFile() ? requested : resolve(dist, 'index.html');
    const mime = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8', '.css': 'text/css; charset=utf-8', '.woff2': 'font/woff2', '.svg': 'image/svg+xml', '.png': 'image/png', '.jpg': 'image/jpeg' }[extname(file)] ?? 'application/octet-stream';
    return sendFile(request, response, file, mime);
  }
  const body = await bodyOf(request);
  state.requests.push({ method: request.method, path: route, query: Object.fromEntries(url.searchParams), body: /Authenticate/.test(route) ? { Username: body.Username } : body });
  if (state.failures[route]) return json(response, { ResponseStatus: { ErrorCode: 'FixtureError', Message: 'The isolated fixture rejected this request.' } }, Number(state.failures[route]));
  if (route === '/System/Info/Public' || route === '/System/Info') return json(response, { Id: 'fixture-server', ServerName: 'goby.local', ProductName: 'Goby', Version: '4.9.5.0', GobyVersion: 'acceptance-fixture', LocalAddress: `http://127.0.0.1:${port}`, StartupWizardCompleted: true });
  if (route === '/System/Endpoint') return json(response, { IsLocal: true, IsInNetwork: true });
  if (route === '/Users/Public') return json(response, [state.user]);
  if (route === '/Users/AuthenticateByName') {
    if (body.Username !== 'reviewer' || (body.Pw ?? body.Password) !== 'goby-player-test') return json(response, { ResponseStatus: { ErrorCode: 'Unauthorized', Message: 'Invalid username or password.' } }, 401);
    return json(response, { AccessToken: TOKEN, User: state.user, ServerId: 'fixture-server', SessionInfo: { Id: 'fixture-session', DeviceId: 'fixture-browser' } });
  }
  const auth = [request.headers['x-emby-token'], request.headers['x-emby-authorization'], request.headers.authorization, url.searchParams.get('api_key')].join(' ');
  if (!auth.includes(TOKEN)) return json(response, { ResponseStatus: { ErrorCode: 'Unauthorized', Message: 'Authentication is required.' } }, 401);
  if (manualDemo && await manualDemo.handleManualDemo({ request, response, route, url, state, body, directory, json, sendFile, sendPicsum })) return;
  if (route === '/Sessions/Logout') return json(response, {});
  if (route === `/Users/${USER_ID}` || route === '/Users/Me') return json(response, state.user);
  if (route === `/Users/${USER_ID}/Views`) return json(response, { Items: LIBRARIES, TotalRecordCount: LIBRARIES.length });
  if (route === `/Users/${USER_ID}/ViewingStatistics`) return json(response, { EstimatedContentHours: 42, EstimatedContentTicks: '1512000000000', IsEstimate: true });
  if (route === `/Users/${USER_ID}/Configuration` || route === `/Users/${USER_ID}/Configuration/Partial` || route === `/Users/${USER_ID}/Preferences`) {
    if (request.method === 'POST' || request.method === 'PUT') { Object.assign(state.user.Configuration, body.Configuration ?? body); state.preferenceRevision += 1; }
    if (route.endsWith('Configuration')) return json(response, state.user.Configuration);
    if (route.endsWith('Partial')) { response.writeHead(200); response.end(); return; }
    return json(response, { Configuration: state.user.Configuration, Revision: String(state.preferenceRevision) });
  }
  if (route.startsWith('/DisplayPreferences/')) {
    const id = route.split('/')[2];
    state.display[id] ??= { Id: id, Client: url.searchParams.get('Client') ?? 'Goby Player', Revision: '1', CustomPrefs: {} };
    if (request.method === 'POST') { Object.assign(state.display[id], body); state.display[id].Revision = String(Number(state.display[id].Revision) + 1); response.writeHead(200); response.end(); return; }
    return json(response, state.display[id]);
  }
  if (route === `/Users/${USER_ID}/Items` || route === '/Items') return json(response, queryItems(url.searchParams));
  if (route === `/Users/${USER_ID}/Items/Latest`) {
    const query = new URLSearchParams(url.searchParams);
    if (query.get('IncludeItemTypes')?.includes('Episode') && query.get('GroupItems') === 'true') query.set('IncludeItemTypes', query.get('IncludeItemTypes').replace('Episode', 'Series'));
    return json(response, queryItems(query, state.items.filter((item) => ['Movie', 'Series'].includes(item.Type))).Items);
  }
  if (route === `/Users/${USER_ID}/Items/Resume` || route === '/Items/Resume') return json(response, queryItems(url.searchParams, state.items.filter((item) => item.UserData?.PlaybackPositionTicks > 0 && !item.UserData.Played)));
  if (route === '/Shows/NextUp') return json(response, queryItems(url.searchParams, state.items.filter((item) => item.Type === 'Episode' && item.UserData?.PlaybackPositionTicks > 0)));
  if (route === '/Genres') {
    const filters = new URLSearchParams(url.searchParams);
    filters.delete('Limit'); filters.delete('StartIndex');
    const genres = [...new Set(queryItems(filters).Items.flatMap((item) => item.Genres ?? []))];
    return json(response, { Items: genres.map((Name) => ({ Id: `genre-${Name}`, Name })), TotalRecordCount: genres.length });
  }
  if (route === '/Persons') {
    const term = url.searchParams.get('SearchTerm')?.toLocaleLowerCase() ?? '';
    const people = [...new Map(state.items.flatMap((item) => item.People ?? []).map((person) => [person.Id, person])).values()].filter((person) => person.Name.toLocaleLowerCase().includes(term));
    return json(response, { Items: people.slice(0, Number(url.searchParams.get('Limit') ?? 1000)), TotalRecordCount: people.length });
  }
  if (route === '/Search/Hints') {
    const term = (url.searchParams.get('SearchTerm') ?? '').toLocaleLowerCase();
    const people = [...new Map(state.items.flatMap(item => item.People ?? []).map(person => [person.Id, person])).values()].map(person => ({ ...person, Type: 'Person' }));
    const genres = [...new Set(state.items.flatMap(item => item.Genres ?? []))].map(Name => ({ Id: `genre-${Name}`, Name, Type: 'Genre' }));
    const candidates = [...state.items.filter(item => ['Movie', 'Series'].includes(item.Type)), ...people, ...genres];
    const matches = state.empty ? [] : candidates.filter(item => item.Name.toLocaleLowerCase().includes(term)).map(item => ({
      Id: item.Id, ItemId: item.Id, Name: item.Name, Type: item.Type, ProductionYear: item.ProductionYear,
      PrimaryImageTag: item.ImageTags?.Primary, BackdropImageTag: item.BackdropImageTags?.[0],
      GobyReference: { Kind: ['Person', 'Genre'].includes(item.Type) ? 'Entity' : 'Item', Id: item.Id },
    }));
    const start = Number(url.searchParams.get('StartIndex') ?? 0);
    return json(response, { SearchHints: matches.slice(start, start + Number(url.searchParams.get('Limit') ?? 48)), TotalRecordCount: matches.length });
  }
  if (/^\/Items\/[^/]+\/ThemeMedia$/.test(route)) return json(response, { ThemeSongsResult: { Items: [], TotalRecordCount: 0 }, ThemeVideosResult: { Items: [], TotalRecordCount: 0 } });
  if (/^\/Items\/[^/]+\/BackgroundPreview$/.test(route)) return json(response, { Available: false });
  if (/^\/Items\/[^/]+\/AudioWaveforms$/.test(route)) return json(response, { Available: false });
  if (/^\/Items\/[^/]+\/SubtitleTimelines$/.test(route)) return json(response, { Available: false });
  if (/^\/Users\/[^/]+\/Items\/[^/]+\/LocalTrailers$/.test(route)) return json(response, []);
  if (route === '/Items/Counts') return json(response, { MovieCount: state.items.filter((item) => item.Type === 'Movie').length, SeriesCount: state.items.filter((item) => item.Type === 'Series').length, EpisodeCount: state.items.filter((item) => item.Type === 'Episode').length });
  if (route === '/Items/Filters2' || route === '/Items/Filters') return json(response, { Genres: [...new Set(state.items.flatMap((item) => item.Genres ?? []))], Years: [2025, 2024, 2023, 2022, 2021, 2020, 2019] });
  const detail = route.match(new RegExp(`^/Users/${USER_ID}/Items/([^/]+)$`)) ?? route.match(/^\/Items\/([^/]+)$/);
  if (detail) return json(response, state.items.find((item) => item.Id === detail[1]) ?? { ResponseStatus: { ErrorCode: 'NotFound' } }, state.items.some((item) => item.Id === detail[1]) ? 200 : 404);
  const seasons = route.match(/^\/Shows\/([^/]+)\/Seasons$/);
  if (seasons) return json(response, queryItems(url.searchParams, state.items.filter((item) => item.Type === 'Season' && item.SeriesId === seasons[1])));
  const episodes = route.match(/^\/Shows\/([^/]+)\/Episodes$/);
  if (episodes) return json(response, queryItems(url.searchParams, state.items.filter((item) => item.Type === 'Episode' && item.SeriesId === episodes[1] && (!url.searchParams.get('SeasonId') || item.SeasonId === url.searchParams.get('SeasonId')))));
  const similar = route.match(/^\/Items\/([^/]+)\/Similar$/);
  if (similar) return json(response, queryItems(url.searchParams, state.items.filter((item) => ['Movie', 'Series'].includes(item.Type) && item.Id !== similar[1])));
  if (/^\/Items\/[^/]+\/ThumbnailSet$/.test(route)) return json(response, { AspectRatio: 16 / 9, Thumbnails: [] });
  const favorite = route.match(new RegExp(`^/Users/${USER_ID}/(FavoriteItems|PlayedItems)/([^/]+)$`));
  if (favorite) {
    const item = state.items.find((item) => item.Id === favorite[2]);
    if (!item) return json(response, { ResponseStatus: { ErrorCode: 'NotFound' } }, 404);
    const selected = request.method === 'POST';
    if (favorite[1] === 'FavoriteItems') item.UserData.IsFavorite = selected;
    else Object.assign(item.UserData, { Played: selected, PlayedPercentage: selected ? 100 : 0, PlaybackPositionTicks: 0 });
    return json(response, item.UserData);
  }
  const userData = route.match(new RegExp(`^/Users/${USER_ID}/Items/([^/]+)/UserData$`));
  if (userData) {
    const item = state.items.find((item) => item.Id === userData[1]);
    if (request.method === 'POST') Object.assign(item.UserData, body);
    return json(response, item.UserData);
  }
  const image = route.match(/^\/Items\/([^/]+)\/Images\/(Primary|Thumb|Backdrop)(?:\/\d+)?$/);
  if (image) {
    const ids = state.imageIds[image[1]];
    if (!ids) return json(response, { error: 'Image not found.' }, 404);
    const item = state.items.find((entry) => entry.Id === image[1]);
    const poster = image[2] === 'Primary' && item?.Type !== 'Episode';
    return sendPicsum(request, response, poster ? ids.primary : ids.backdrop, poster ? 400 : 1920, poster ? 600 : 1080);
  }
  const playback = route.match(/^\/Items\/([^/]+)\/PlaybackInfo$/);
  if (playback) {
    const item = state.items.find((entry) => entry.Id === playback[1]);
    if (!item?.MediaSources?.length) return json(response, { ErrorCode: 'NoCompatibleStream', MediaSources: [] });
    if (body.EnableDirectPlay === false && body.EnableDirectStream === false) return json(response, { ErrorCode: 'NoCompatibleStream', MediaSources: [] });
    const source = structuredClone(item.MediaSources[0]);
    source.RunTimeTicks = 300_000_000;
    // A returned source can advertise HLS metadata while the selected URL is direct.
    source.TranscodingSubProtocol = 'hls';
    source.DirectStreamUrl = `/emby/Videos/${item.Id}/stream.webm?MediaSourceId=${source.Id}&api_key=${TOKEN}`;
    return json(response, { PlaySessionId: `play-${item.Id}-${state.events.length}`, MediaSources: [source] });
  }
  if (/^\/Videos\/[^/]+\/stream(?:\.\w+)?$/.test(route)) return sendFile(request, response, resolve(directory, 'assets/sample.webm'), 'video/webm');
  if (/\/Subtitles\/\d+\/Stream\.vtt$/i.test(route)) {
    response.writeHead(200, { 'Content-Type': 'text/vtt; charset=utf-8' });
    response.end('WEBVTT\n\n00:00:01.000 --> 00:00:10.000\n你听到了吗？\n\n00:00:10.000 --> 00:00:20.000\n又是那个频率。\n\n00:00:20.000 --> 00:00:29.000\n它在回应我们。\n');
    return;
  }
  if (['/Sessions/Playing', '/Sessions/Playing/Progress', '/Sessions/Playing/Stopped'].includes(route)) {
    state.events.push({ route, ...body, receivedAt: new Date().toISOString() });
    const item = state.items.find((entry) => entry.Id === body.ItemId);
    if (item && Number.isFinite(body.PositionTicks)) Object.assign(item.UserData, { PlaybackPositionTicks: body.PositionTicks, LastPlayedDate: new Date().toISOString(), PlayedPercentage: Math.min(100, body.PositionTicks / 3_000_000) });
    response.writeHead(204); response.end(); return;
  }
  if (route === '/Videos/ActiveEncodings' && request.method === 'DELETE') { response.writeHead(204); response.end(); return; }
  state.unknownRequests.push({ method: request.method, route, body });
  return json(response, { ResponseStatus: { ErrorCode: 'FixtureRouteNotImplemented', Message: `${request.method} ${route}` } }, 404);
}

await mkdir(artifactDirectory, { recursive: true });
const server = createServer((request, response) => handle(request, response).catch((error) => {
  console.error(error.message);
  if (!response.headersSent) json(response, { error: error.message }, 500);
  else response.end();
}));
server.listen(port, '127.0.0.1', () => console.log(`Isolated player acceptance fixture: http://127.0.0.1:${port}`));
process.on('SIGTERM', () => server.close());
process.on('SIGINT', () => server.close());
