// Independent authored-pixel oracle for external bitmap playback acceptance.
// The alphabet and timing below describe the inputs, not a production decoder's
// output. Both IDX languages contain the same Latin glyphs; their clocks differ.
// Read pixels at native video dimensions after a presented-frame barrier, and
// pass the actual source media time rather than the requested seek target.

const TICKS_PER_SECOND = 10_000_000;
const TEXT = 'Hello world';
const SCALE = 2;
const GLYPH_WIDTH = 130;
const GLYPH_HEIGHT = 14;
const GLYPH_PIXEL_COUNT = 492;
const FRAME_WIDTH = 320;
const FRAME_HEIGHT = 192;
const BACKGROUND_RGB = Object.freeze([20, 37, 59]);

const ALPHABET = Object.freeze({
  H: ['10001', '10001', '10001', '11111', '10001', '10001', '10001'],
  e: ['00000', '00000', '01110', '10001', '11111', '10000', '01111'],
  l: ['01100', '00100', '00100', '00100', '00100', '00100', '01110'],
  o: ['00000', '00000', '01110', '10001', '10001', '10001', '01110'],
  w: ['00000', '00000', '10001', '10001', '10101', '10101', '01010'],
  r: ['00000', '00000', '10110', '11001', '10000', '10000', '10000'],
  d: ['00001', '00001', '01101', '10011', '10001', '10001', '01111'],
  ' ': ['00000', '00000', '00000', '00000', '00000', '00000', '00000'],
});

const RECTANGLES = Object.freeze({
  top: Object.freeze({ id: 'top', x: 21, y: 40, width: GLYPH_WIDTH, height: GLYPH_HEIGHT }),
  bottom: Object.freeze({ id: 'bottom', x: 93, y: 100, width: GLYPH_WIDTH, height: GLYPH_HEIGHT }),
});

function freezeIntervals(intervals) {
  return Object.freeze(intervals.map(interval => Object.freeze(interval)));
}

// These are source-clock half-open intervals, including the IDX delays.
// The lower SUP object is forced. DVD has one optional SPU per interval.
export const AUTHORED_BITMAP_INTERVALS = Object.freeze({
  sup: freezeIntervals([
    { startTicks: 10_240_000, endTicks: 40_960_000, rectangle: 'top', forced: false },
    { startTicks: 25_600_000, endTicks: 56_320_000, rectangle: 'bottom', forced: true },
  ]),
  en: freezeIntervals([
    { startTicks: 30_000_000, endTicks: 60_720_000, rectangle: 'top', forced: false },
    { startTicks: 100_000_000, endTicks: 130_720_000, rectangle: 'top', forced: false },
  ]),
  zh: freezeIntervals([
    { startTicks: 10_000_000, endTicks: 40_720_000, rectangle: 'top', forced: false },
  ]),
  off: freezeIntervals([]),
});

// Fix tolerances before examining candidate output. Bright foreground is far
// above the authored dark background. Count modest codec edge errors without
// accepting a filled rectangle, a missing character, or a misplaced full glyph.
export const BITMAP_PIXEL_THRESHOLDS = Object.freeze({
  whiteChannelMinimum: 185,
  foregroundCoverageMinimum: 0.90,
  characterCoverageMinimum: 0.80,
  transparentLightLeakMaximum: 0.03,
  inactiveLightLeakMaximum: 0.03,
  outsideLightLeakMaximum: 0.003,
  backgroundMeanChannelErrorMaximum: 12,
});

/** Return a row-major boolean source mask. This does not decode subtitle bytes. */
export function authoredGlyphMask() {
  const mask = Array(GLYPH_WIDTH * GLYPH_HEIGHT).fill(false);
  for (const [position, character] of [...TEXT].entries()) {
    const rows = ALPHABET[character];
    if (!rows || rows.length !== 7 || rows.some(row => !/^[01]{5}$/.test(row))) {
      throw new Error('The authored alphabet is incomplete.');
    }
    for (let row = 0; row < rows.length; row++) {
      for (let column = 0; column < rows[row].length; column++) {
        if (rows[row][column] !== '1') continue;
        for (let y = 0; y < SCALE; y++) {
          for (let x = 0; x < SCALE; x++) {
            mask[(row * SCALE + y) * GLYPH_WIDTH + (position * 6 + column) * SCALE + x] = true;
          }
        }
      }
    }
  }
  const count = mask.reduce((total, foreground) => total + Number(foreground), 0);
  // There are 123 authored cells, each expanded to four pixels. In particular,
  // the fourth row of "r" is 11001 and contains three foreground cells.
  if (count !== GLYPH_PIXEL_COUNT || count === 0 || count === mask.length) {
    throw new Error('The authored glyph must contain exactly 492 nonempty foreground pixels.');
  }
  return { text: TEXT, width: GLYPH_WIDTH, height: GLYPH_HEIGHT, mask, count };
}

function secondsToTicks(value, label, signed = false) {
  if (!Number.isFinite(value) || (!signed && value < 0)) {
    throw new TypeError(`${label} must be a finite ${signed ? '' : 'nonnegative '}number.`);
  }
  const ticks = Math.round(value * TICKS_PER_SECOND);
  if (!Number.isSafeInteger(ticks)) throw new RangeError(`${label} exceeds the exact source clock.`);
  return ticks;
}

/** Positive offsets delay subtitles. "off" explicitly expects no rectangles. */
export function expectedRectangles(kind = 'sup', sourceTimeSeconds, offsetSeconds = 0) {
  if (typeof kind !== 'string' || !Object.hasOwn(AUTHORED_BITMAP_INTERVALS, kind)) {
    throw new TypeError('kind must be sup, en, zh, or off.');
  }
  const sourceTicks = secondsToTicks(sourceTimeSeconds, 'sourceTimeSeconds');
  const offsetTicks = secondsToTicks(offsetSeconds, 'offsetSeconds', true);
  const authoredTicks = sourceTicks - offsetTicks;
  if (!Number.isSafeInteger(authoredTicks)) throw new RangeError('The offset source clock is not exact.');
  return AUTHORED_BITMAP_INTERVALS[kind]
    .filter(interval => authoredTicks >= interval.startTicks && authoredTicks < interval.endTicks)
    .map(interval => ({
      ...RECTANGLES[interval.rectangle], forced: interval.forced,
      authoredStartSeconds: interval.startTicks / TICKS_PER_SECOND,
      authoredEndSeconds: interval.endTicks / TICKS_PER_SECOND,
    }));
}

function rounded(value) {
  return Math.round(value * 1_000_000) / 1_000_000;
}

/**
 * Analyze opaque RGBA bytes from the authored 320x192 dark video.
 * rgba may be a byte typed array, Buffer, or a JSON array from drawImage().
 * The returned evidence contains counts and fractions, never the frame or mask.
 */
export function analyzeBitmapFrame({ rgba, width, height, kind = 'sup', timeSeconds, offsetSeconds = 0 } = {}) {
  const failures = [];
  const summary = {
    format: 'goby-bitmap-playback-pixels-v1', width, height, kind, timeSeconds, offsetSeconds,
    glyph: { text: TEXT, width: GLYPH_WIDTH, height: GLYPH_HEIGHT, foregroundPixels: GLYPH_PIXEL_COUNT },
    thresholds: { ...BITMAP_PIXEL_THRESHOLDS }, expectedRectangles: [], regions: [],
  };
  const finish = () => ({ ...summary, passed: failures.length === 0, failures });
  try {
    summary.expectedRectangles = expectedRectangles(kind, timeSeconds, offsetSeconds);
  } catch (error) {
    failures.push(error.message);
    return finish();
  }
  if (width !== FRAME_WIDTH || height !== FRAME_HEIGHT) {
    failures.push('The oracle requires native 320x192 pixels; it never infers a coordinate transform.');
    return finish();
  }
  if (!(Array.isArray(rgba) || ArrayBuffer.isView(rgba)) || rgba.length !== width * height * 4) {
    failures.push('rgba must contain exactly 245760 channel bytes for the native frame.');
    return finish();
  }

  const white = new Uint8Array(width * height);
  let nonOpaquePixels = 0, whitePixels = 0;
  for (let pixel = 0; pixel < white.length; pixel++) {
    const offset = pixel * 4;
    for (let channel = 0; channel < 4; channel++) {
      const value = rgba[offset + channel];
      if (!Number.isInteger(value) || value < 0 || value > 255) {
        failures.push('rgba contains a channel value outside the byte range.');
        return finish();
      }
    }
    if (rgba[offset + 3] !== 255) nonOpaquePixels++;
    white[pixel] = Number(Math.min(rgba[offset], rgba[offset + 1], rgba[offset + 2]) >= BITMAP_PIXEL_THRESHOLDS.whiteChannelMinimum);
    whitePixels += white[pixel];
  }
  summary.whitePixels = whitePixels;
  summary.nonOpaquePixels = nonOpaquePixels;
  if (nonOpaquePixels) failures.push('The decoded video frame must be fully opaque.');

  const glyph = authoredGlyphMask();
  const insideRectangle = new Uint8Array(width * height);
  for (const rectangle of Object.values(RECTANGLES)) {
    const active = summary.expectedRectangles.find(expected => expected.id === rectangle.id);
    const characters = [...TEXT].map((character, index) => ({ character, index, foregroundPixels: 0, whitePixels: 0 }));
    let foregroundWhite = 0, transparentWhite = 0;
    for (let y = 0; y < rectangle.height; y++) {
      for (let x = 0; x < rectangle.width; x++) {
        const frameIndex = (rectangle.y + y) * width + rectangle.x + x;
        insideRectangle[frameIndex] = 1;
        if (glyph.mask[y * rectangle.width + x]) {
          foregroundWhite += white[frameIndex];
          const character = characters[Math.floor(x / (6 * SCALE))];
          character.foregroundPixels++;
          character.whitePixels += white[frameIndex];
        } else {
          transparentWhite += white[frameIndex];
        }
      }
    }
    const transparentPixels = glyph.mask.length - glyph.count;
    const foregroundCoverage = foregroundWhite / glyph.count;
    const transparentLightLeak = transparentWhite / transparentPixels;
    const rectangleWhite = foregroundWhite + transparentWhite;
    const rectangleLightLeak = rectangleWhite / glyph.mask.length;
    const visibleCharacters = characters.filter(character => character.foregroundPixels > 0);
    const minimumCharacterCoverage = Math.min(...visibleCharacters.map(character => character.whitePixels / character.foregroundPixels));
    summary.regions.push({
      ...rectangle, expectedActive: Boolean(active), expectedForced: active?.forced ?? null,
      foregroundPixels: glyph.count, foregroundWhite, foregroundCoverage: rounded(foregroundCoverage),
      transparentPixels, transparentWhite, transparentLightLeak: rounded(transparentLightLeak),
      rectanglePixels: glyph.mask.length, rectangleWhite, rectangleLightLeak: rounded(rectangleLightLeak),
      minimumCharacterCoverage: rounded(minimumCharacterCoverage),
    });
    if (active) {
      if (foregroundCoverage < BITMAP_PIXEL_THRESHOLDS.foregroundCoverageMinimum) failures.push(`${rectangle.id}: foreground coverage is below 0.90.`);
      if (transparentLightLeak > BITMAP_PIXEL_THRESHOLDS.transparentLightLeakMaximum) failures.push(`${rectangle.id}: transparent-hole light leakage exceeds 0.03.`);
      for (const character of visibleCharacters) {
        if (character.whitePixels / character.foregroundPixels < BITMAP_PIXEL_THRESHOLDS.characterCoverageMinimum) {
          failures.push(`${rectangle.id}: character ${character.index} (${character.character}) coverage is below 0.80.`);
        }
      }
    } else if (rectangleLightLeak > BITMAP_PIXEL_THRESHOLDS.inactiveLightLeakMaximum) {
      failures.push(`${rectangle.id}: inactive rectangle light leakage exceeds 0.03.`);
    }
  }

  let outsidePixels = 0, outsideWhite = 0;
  const outsideRGB = [0, 0, 0];
  for (let pixel = 0; pixel < white.length; pixel++) {
    if (insideRectangle[pixel]) continue;
    outsidePixels++;
    outsideWhite += white[pixel];
    for (let channel = 0; channel < 3; channel++) outsideRGB[channel] += rgba[pixel * 4 + channel];
  }
  const meanRGB = outsideRGB.map(total => total / outsidePixels);
  const meanChannelErrors = meanRGB.map((value, channel) => Math.abs(value - BACKGROUND_RGB[channel]));
  const outsideLightLeak = outsideWhite / outsidePixels;
  summary.background = {
    expectedRGB: [...BACKGROUND_RGB], outsidePixels, outsideWhite, outsideLightLeak: rounded(outsideLightLeak),
    meanRGB: meanRGB.map(rounded), meanChannelErrors: meanChannelErrors.map(rounded),
  };
  if (outsideLightLeak > BITMAP_PIXEL_THRESHOLDS.outsideLightLeakMaximum) failures.push('White pixels outside both authored rectangles exceed 0.003.');
  if (meanChannelErrors.some(error => error > BITMAP_PIXEL_THRESHOLDS.backgroundMeanChannelErrorMaximum)) {
    failures.push('The background mean differs from authored RGB (20,37,59) by more than 12 in a channel.');
  }
  return finish();
}
