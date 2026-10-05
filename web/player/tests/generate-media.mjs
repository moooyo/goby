import { chromium } from '@playwright/test';
import { mkdir, writeFile, rename, readdir, access, unlink } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// A deterministic real VP8/Opus stream keeps playback acceptance independent of remote sample hosts.
const browser = await chromium.launch({ channel: process.env.GOBY_BROWSER_CHANNEL, headless: true });
try {
  const page = await browser.newPage();
  await page.goto('about:blank');
  const bytes = await page.evaluate(async () => {
    const canvas = document.createElement('canvas');
    canvas.width = 640;
    canvas.height = 360;
    const context = canvas.getContext('2d');
    const stream = canvas.captureStream(10);
    const audio = new AudioContext();
    const oscillator = audio.createOscillator();
    oscillator.frequency.value = 220;
    const gain = audio.createGain();
    gain.gain.value = 0.025;
    const destination = audio.createMediaStreamDestination();
    oscillator.connect(gain).connect(destination);
    oscillator.start();
    await audio.resume();
    stream.addTrack(destination.stream.getAudioTracks()[0]);
    const recorder = new MediaRecorder(stream, { mimeType: 'video/webm;codecs=vp8,opus', videoBitsPerSecond: 180_000, audioBitsPerSecond: 32_000 });
    const chunks = [];
    recorder.ondataavailable = (event) => chunks.push(event.data);
    const done = new Promise((resolve) => { recorder.onstop = resolve; });
    const started = performance.now();
    const timer = setInterval(() => {
      const elapsed = (performance.now() - started) / 1000;
      context.fillStyle = '#071c26';
      context.fillRect(0, 0, canvas.width, canvas.height);
      const gradient = context.createLinearGradient(0, 0, canvas.width, canvas.height);
      gradient.addColorStop(0, '#0c3946');
      gradient.addColorStop(1, '#071c26');
      context.fillStyle = gradient;
      context.fillRect(0, 0, canvas.width, canvas.height);
      context.fillStyle = '#d9b56f';
      context.fillRect(30, 290, elapsed / 30 * 580, 4);
      context.font = 'bold 38px sans-serif';
      context.fillStyle = '#f3f2f0';
      context.fillText('GOBY PLAYBACK FIXTURE', 30, 155);
      context.font = '22px monospace';
      context.fillText(`${elapsed.toFixed(1).padStart(4, '0')} / 30 seconds`, 30, 200);
      if (elapsed >= 30) { clearInterval(timer); recorder.stop(); }
    }, 100);
    recorder.start();
    await done;
    oscillator.stop();
    await audio.close();
    stream.getTracks().forEach((track) => track.stop());
    return Array.from(new Uint8Array(await new Blob(chunks, { type: 'video/webm' }).arrayBuffer()));
  });
  await mkdir(new URL('./assets/', import.meta.url), { recursive: true });
  await writeFile(new URL('./assets/sample.webm', import.meta.url), new Uint8Array(bytes));
  let ffmpeg = process.env.GOBY_FFMPEG;
  if (!ffmpeg && process.platform === 'win32' && process.env.LOCALAPPDATA) {
    const cache = resolve(process.env.LOCALAPPDATA, 'ms-playwright');
    const versions = (await readdir(cache)).filter((name) => name.startsWith('ffmpeg-')).sort();
    if (versions.length) ffmpeg = resolve(cache, versions.at(-1), 'ffmpeg-win64.exe');
  }
  const output = fileURLToPath(new URL('./assets/sample.webm', import.meta.url));
  const remuxed = fileURLToPath(new URL('./assets/sample-remux.webm', import.meta.url));
  execFileSync(ffmpeg ?? 'ffmpeg', ['-hide_banner', '-loglevel', 'error', '-y', '-i', output, '-c', 'copy', remuxed]);
  await access(remuxed);
  await unlink(output);
  await rename(remuxed, output);
  console.log(`Generated ${bytes.length} bytes of real VP8/Opus test media.`);
} finally {
  await browser.close();
}
