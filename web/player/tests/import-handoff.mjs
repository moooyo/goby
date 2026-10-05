import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

// Import only the supplied catalogue data, never the prototype runtime.
const handoff = process.argv[2] ?? 'D:/Code/design_handoff_goby_player/goby-core.js';
const source = await readFile(handoff, 'utf8');
const start = source.indexOf('const POOLS = ');
const end = source.indexOf('const CUES = ');
if (start < 0 || end < 0) throw new Error('The supplied handoff catalogue was not found.');
const data = vm.runInNewContext(`${source.slice(start, end)}; ({ pools: POOLS, raw: RAW })`);
const directory = fileURLToPath(new URL('./data/', import.meta.url));
await mkdir(directory, { recursive: true });
await writeFile(new URL('./data/catalogue.json', import.meta.url), `${JSON.stringify(data, null, 2)}\n`);

