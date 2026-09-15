import { createHash, randomUUID } from 'node:crypto';
import { existsSync, lstatSync, mkdirSync, readFileSync, readdirSync, realpathSync, unlinkSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, isAbsolute, join, relative, resolve, sep } from 'node:path';
import type { Plugin, ResolvedConfig } from 'vite';

type Digest = { bytes: number; sha256: string };
type FilePin = Digest & { path: string; realPath: string };
type PackagePin = { name: string; version: string; manifest: FilePin };
type ParsedModule = { file: FilePin | null; package: PackagePin | null; parsedCode: Digest | null };

const maximumFileBytes = 16 * 1024 * 1024;
const maximumInputBytes = 256 * 1024 * 1024;
const maximumReportBytes = 16 * 1024 * 1024;

function need(value: unknown, message: string): asserts value {
  if (!value) throw new Error(`Frontend contributions: ${message}`);
}

function digest(value: string | Uint8Array): Digest {
  const bytes = typeof value === 'string' ? Buffer.from(value, 'utf8') : value;
  return { bytes: bytes.byteLength, sha256: createHash('sha256').update(bytes).digest('hex') };
}

function within(path: string, parent: string): boolean {
  const name = relative(parent, path);
  return name === '' || (!isAbsolute(name) && name !== '..' && !name.startsWith(`..${sep}`));
}

function readPin(path: string): FilePin {
  const realPath = realpathSync(path);
  const before = lstatSync(realPath);
  need(before.isFile() && before.size <= maximumFileBytes, `unsupported input file: ${path}`);
  const bytes = readFileSync(realPath);
  const after = lstatSync(realPath);
  need(realpathSync(path) === realPath && bytes.length === before.size
    && ['dev', 'ino', 'size', 'mtimeMs', 'ctimeMs'].every((key) =>
      before[key as keyof typeof before] === after[key as keyof typeof after]), `file changed while reading: ${path}`);
  return { path, realPath, ...digest(bytes) };
}

// This records the bundler's own contribution map and a private disk snapshot.
// A successful frontend command and later package asset match remain separate evidence.
export function frontendContributions(): Plugin {
  let config: ResolvedConfig;
  let outputDirectory = '';
  let reportPath = '';
  let buildId = '';
  let written = false;
  let reportDirectoryIdentity: { dev: number; ino: number } | null = null;
  let inputBytes = 0;
  const files = new Map<string, FilePin>();
  const packages = new Map<string, PackagePin | null>();
  const parsed = new Map<string, ParsedModule>();
  let tools: Record<string, PackagePin> = {};

  function remember(path: string): FilePin {
    const previous = files.get(path);
    if (previous) return previous;
    const pin = readPin(path);
    inputBytes += pin.bytes;
    need(inputBytes <= maximumInputBytes, 'input snapshot budget exceeded');
    files.set(path, pin);
    return pin;
  }

  function packageFor(path: string): PackagePin | null {
    let directory = dirname(path);
    const visited: string[] = [];
    let result: PackagePin | null = null;
    for (;;) {
      if (packages.has(directory)) {
        result = packages.get(directory) ?? null;
        break;
      }
      visited.push(directory);
      const candidate = join(directory, 'package.json');
      if (existsSync(candidate)) {
        const manifest = remember(candidate);
        const bytes = readFileSync(manifest.realPath);
        need(digest(bytes).sha256 === manifest.sha256, 'package metadata changed');
        const metadata: unknown = JSON.parse(bytes.toString('utf8'));
        if (metadata && typeof metadata === 'object' && ('name' in metadata || 'version' in metadata)) {
          if ('name' in metadata && 'version' in metadata && typeof metadata.name === 'string' && metadata.name.length > 0
            && typeof metadata.version === 'string' && metadata.version.length > 0) {
            result = { name: metadata.name, version: metadata.version, manifest };
          }
          // An incomplete package identity must not inherit an enclosing package's version.
          break;
        }
      }
      const parent = dirname(directory);
      if (parent === directory) break;
      directory = parent;
    }
    for (const visitedDirectory of visited) packages.set(visitedDirectory, result);
    return result;
  }

  function physicalModule(id: string): FilePin | null {
    if (id.startsWith('\0')) return null;
    // Vite resource queries identify a transformation of the associated file.
    // The raw file and parsed code are recorded separately, never equated.
    const path = id.replace(/[?#].*$/, '');
    if (!isAbsolute(path) || !existsSync(path) || !lstatSync(realpathSync(path)).isFile()) return null;
    return remember(path);
  }

  return {
    name: 'goby:frontend-contributions',
    apply: 'build',
    configResolved(resolved) {
      config = resolved;
      need(!config.build.watch && !config.build.ssr && config.build.write, 'a written, one-shot client build is required');
      outputDirectory = resolve(config.root, config.build.outDir);
      reportPath = join(config.root, '.artifacts', 'frontend-contributions.json');
      need(!within(reportPath, outputDirectory), 'the private report must remain outside the asset directory');
    },
    buildStart() {
      files.clear();
      packages.clear();
      parsed.clear();
      inputBytes = 0;
      written = false;
      buildId = randomUUID();
      const directory = dirname(reportPath);
      mkdirSync(directory, { recursive: true, mode: 0o700 });
      need(realpathSync(directory) === directory, 'the report directory cannot have symbolic-link components');
      const directoryStat = lstatSync(directory);
      need(directoryStat.isDirectory(), 'the report parent must be a directory');
      reportDirectoryIdentity = { dev: directoryStat.dev, ino: directoryStat.ino };
      if (existsSync(reportPath)) {
        need(lstatSync(reportPath).isFile() && !lstatSync(reportPath).isSymbolicLink(), 'the old report must be a regular file');
        unlinkSync(reportPath);
      }
      for (const path of [join(config.root, 'package.json'), join(config.root, 'package-lock.json'),
        ...(config.configFile ? [config.configFile] : []), ...config.configFileDependencies]) remember(path);
      const fromRoot = createRequire(join(config.root, 'package.json'));
      const viteEntry = fromRoot.resolve('vite');
      const fromVite = createRequire(viteEntry);
      const entries = { vite: viteEntry, rolldown: fromVite.resolve('rolldown'),
        reactPlugin: fromRoot.resolve('@vitejs/plugin-react') };
      tools = {};
      for (const [name, entry] of Object.entries(entries)) {
        const packagePin = packageFor(realpathSync(entry));
        need(packagePin, `missing ${name} package identity`);
        tools[name] = packagePin;
      }
    },
    moduleParsed(info) {
      const file = physicalModule(info.id);
      parsed.set(info.id, { file, package: file ? packageFor(file.realPath) : null,
        parsedCode: typeof info.code === 'string' ? digest(info.code) : null });
    },
    writeBundle: {
      order: 'post',
      handler(options, bundle) {
        need(!written && options.dir && resolve(options.dir) === outputDirectory,
          'exactly one output directory is supported');
        const assets: Array<Digest & { name: string }> = [];
        function visit(directory: string) {
          need(realpathSync(directory) === directory, 'asset directories cannot contain symbolic links');
          for (const name of readdirSync(directory).sort()) {
            const path = join(directory, name);
            const stat = lstatSync(path);
            need(!stat.isSymbolicLink(), 'asset entries cannot be symbolic links');
            if (stat.isDirectory()) visit(path);
            else {
              const pin = readPin(path);
              assets.push({ name: relative(outputDirectory, path).split(sep).join('/'), bytes: pin.bytes, sha256: pin.sha256 });
            }
          }
        }
        visit(outputDirectory);
        assets.sort((left, right) => left.name < right.name ? -1 : left.name > right.name ? 1 : 0);
        const byName = new Map(assets.map((asset) => [asset.name, asset]));
        const chunks = [];
        for (const [name, output] of Object.entries(bundle).sort(([left], [right]) => left < right ? -1 : left > right ? 1 : 0)) {
          const asset = byName.get(name);
          const emitted = digest(output.type === 'chunk' ? output.code : output.source);
          need(asset && asset.bytes === emitted.bytes && asset.sha256 === emitted.sha256,
            `written asset differs from the bundler output: ${name}`);
          if (output.type !== 'chunk') continue;
          chunks.push({ name, facadeModuleId: output.facadeModuleId, imports: output.imports,
            dynamicImports: output.dynamicImports, moduleIds: output.moduleIds,
            modules: Object.entries(output.modules).sort(([left], [right]) => left < right ? -1 : left > right ? 1 : 0)
              .map(([id, rendered]) => {
                const source = parsed.get(id) ?? null;
                const info = this.getModuleInfo(id);
                return { id, parsed: source, origin: source?.file ? 'associated_file' : 'virtual_generated_or_unobserved',
                  importedIds: info?.importedIds ?? null, dynamicallyImportedIds: info?.dynamicallyImportedIds ?? null,
                  renderedLengthUtf16: rendered.renderedLength, renderedExports: rendered.renderedExports,
                  preFinalRenderCode: typeof rendered.code === 'string' ? digest(rendered.code) : null };
              }) });
        }
        for (const [path, before] of files) {
          need(JSON.stringify(readPin(path)) === JSON.stringify(before), `observed input changed: ${path}`);
        }
        const report = { kind: 'goby-frontend-contributions', version: 1, phase: 'bundle_written', buildId,
          capturedAt: new Date().toISOString(), buildSuccessClaimed: false,
          requiresSuccessfulBuildReceipt: true, outputDirectory,
          configuration: { root: config.root, base: config.base, mode: config.mode,
            plugins: config.plugins.map((plugin) => plugin.name), sourcemap: config.build.sourcemap,
            minify: config.build.minify, target: config.build.target },
          runtime: { node: process.versions.node, v8: process.versions.v8 }, tools,
          observedInputs: [...files.values()].sort((left, right) => left.path < right.path ? -1 : left.path > right.path ? 1 : 0),
          assets, chunks,
          boundaries: [
            'chunk.modules is the bundler-reported contribution map; module code precedes final rendering and minification.',
            'renderedLengthUtf16 counts JavaScript string units, not final emitted bytes; null code remains unknown.',
            'Associated disk files are snapshots at moduleParsed; parsed-code hashes are separate and do not prove original-byte consumption.',
            'Loaded import edges do not establish that every dependency survives in an output chunk.',
            'Virtual, generated or unobserved modules are not assigned guessed package ownership.',
            'This writeBundle observation requires a successful build-command receipt and a later exact final-asset match.',
            'No source text, environment values, complete legal approval or historical E11 provenance is included.',
          ] };
        const bytes = Buffer.from(`${JSON.stringify(report, null, 2)}\n`, 'utf8');
        need(bytes.length <= maximumReportBytes, 'report budget exceeded');
        const directory = dirname(reportPath);
        const directoryStat = lstatSync(directory);
        need(reportDirectoryIdentity !== null && directoryStat.isDirectory() && realpathSync(directory) === directory
          && directoryStat.dev === reportDirectoryIdentity.dev && directoryStat.ino === reportDirectoryIdentity.ino,
        'the report directory changed during the build');
        writeFileSync(reportPath, bytes, { flag: 'wx', mode: 0o600 });
        need(readPin(reportPath).sha256 === digest(bytes).sha256, 'written report changed');
        written = true;
      },
    },
  };
}
