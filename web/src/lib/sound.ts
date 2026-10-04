/* Draw-reveal jingle: a decelerating click train synthesized with Web Audio (no
   audio file). AudioProvider owns the preference and the first-gesture unlock, so
   SSE-driven clients that did not click Draw can still play. */

/** Opt-out: anything but "off" = on. */
const STORAGE_KEY = "mp-sound";
const VOLUME_KEY = "mp-volume";
const DEFAULT_VOLUME = 0.5;

/** Fallback wheel settle time (s), for the popover preview; a draw passes exact click times. */
const REVEAL_AT = 6.4;
const TAIL_S = 0.15;
/** Keeps the first click safely in the audio future. */
const START_LEAD_S = 0.03;
/** Shifts the whole train for audio-to-compositor lag (ms, +ve = later). Tune by ear. */
const SYNC_OFFSET_MS = 0;

let unlocked = false;
let playing = false;
let endTimer: number | null = null;
const playListeners = new Set<(p: boolean) => void>();

// clicks -> clickFilter -> playGain -> comp -> master -> out.
let ctx: AudioContext | null = null;
let masterGain: GainNode | null = null;
let playGain: GainNode | null = null;
let clickFilter: BiquadFilterNode | null = null;
let noiseBuf: AudioBuffer | null = null;
let voices: AudioBufferSourceNode[] = [];

function clamp01(n: number): number {
  if (!Number.isFinite(n)) return DEFAULT_VOLUME;
  return Math.min(1, Math.max(0, n));
}

export function getVolume(): number {
  if (typeof window === "undefined") return DEFAULT_VOLUME;
  const raw = localStorage.getItem(VOLUME_KEY);
  return raw === null ? DEFAULT_VOLUME : clamp01(parseFloat(raw));
}

export function setVolume(v: number): void {
  if (typeof window === "undefined") return;
  const vol = clamp01(v);
  localStorage.setItem(VOLUME_KEY, String(vol));
  if (masterGain && ctx) masterGain.gain.setTargetAtTime(vol, ctx.currentTime, 0.02);
}

function ensureGraph(): boolean {
  if (typeof window === "undefined") return false;
  if (ctx) return true;
  const AC = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
  if (!AC) return false;

  ctx = new AC();
  masterGain = ctx.createGain();
  masterGain.gain.value = getVolume();
  masterGain.connect(ctx.destination);

  // Tames summed peaks.
  const comp = ctx.createDynamicsCompressor();
  comp.threshold.value = -14;
  comp.ratio.value = 12;
  comp.attack.value = 0.003;
  comp.release.value = 0.25;
  comp.connect(masterGain);

  playGain = ctx.createGain();
  playGain.gain.value = 1;
  playGain.connect(comp);

  // A resonant bandpass on a noise burst gives a woody "tick".
  clickFilter = ctx.createBiquadFilter();
  clickFilter.type = "bandpass";
  clickFilter.frequency.value = 2600;
  clickFilter.Q.value = 2.2;
  clickFilter.connect(playGain);

  const dur = 0.05;
  const len = Math.ceil(ctx.sampleRate * dur);
  noiseBuf = ctx.createBuffer(1, len, ctx.sampleRate);
  const data = noiseBuf.getChannelData(0);
  for (let i = 0; i < len; i++) data[i] = Math.random() * 2 - 1;

  return true;
}

function scheduleClick(time: number, vel: number): void {
  if (!ctx || !clickFilter || !noiseBuf) return;
  const src = ctx.createBufferSource();
  src.buffer = noiseBuf;
  const g = ctx.createGain();
  g.gain.setValueAtTime(0.0001, time);
  g.gain.exponentialRampToValueAtTime(vel, time + 0.0006);
  g.gain.exponentialRampToValueAtTime(0.0001, time + 0.02);
  src.connect(g);
  g.connect(clickFilter);
  src.start(time);
  src.stop(time + 0.05);
  voices.push(src);
}

function killVoices(): void {
  for (const s of voices) {
    try {
      s.stop();
    } catch {
      // already stopped
    }
  }
  voices = [];
}

function setPlaying(p: boolean): void {
  if (playing === p) return;
  playing = p;
  for (const cb of playListeners) cb(p);
}

export function isJinglePlaying(): boolean {
  return playing;
}

export function onJingleChange(cb: (p: boolean) => void): () => void {
  playListeners.add(cb);
  return () => playListeners.delete(cb);
}

/** Builds the graph early so the first draw's jingle starts with the reel. */
export function preloadJingle(): void {
  ensureGraph();
}

export function isSoundEnabled(): boolean {
  if (typeof window === "undefined") return false;
  return localStorage.getItem(STORAGE_KEY) !== "off";
}

export function setSoundEnabled(on: boolean): void {
  if (typeof window === "undefined") return;
  localStorage.setItem(STORAGE_KEY, on ? "on" : "off");
}

/** Resumes the AudioContext on the first gesture, so later SSE-driven plays pass autoplay. */
export function unlockAudio(): void {
  if (unlocked) return;
  unlocked = true;
  if (!ensureGraph() || !ctx) return;
  if (ctx.state === "suspended") void ctx.resume();
}

/** False on a suspended context, which would replay scheduled clicks out of sync on resume. */
export function isAudioRunning(): boolean {
  return !!ctx && ctx.state === "running";
}

/**
 * Plays the draw sound from the top. `clickOffsets` (s from start) are DrawReel's
 * poster-gap crossings; empty means nothing to play. Without it, the fallback wheel.
 */
export function playDrawJingle(clickOffsets?: number[]): void {
  if (!isSoundEnabled()) return;
  if (clickOffsets && clickOffsets.length === 0) return;
  if (!ensureGraph() || !ctx || !playGain) return;
  if (ctx.state === "suspended") void ctx.resume();
  killVoices();
  const now = ctx.currentTime;
  playGain.gain.cancelScheduledValues(now);
  playGain.gain.setValueAtTime(1, now); // undo a prior fade-out

  // Steady volume: a flapper does not get quieter as it slows.
  const start = now + START_LEAD_S + SYNC_OFFSET_MS / 1000;
  let lastAt: number;
  if (clickOffsets) {
    for (const off of clickOffsets) scheduleClick(start + off, 0.85);
    lastAt = clickOffsets[clickOffsets.length - 1];
  } else {
    // Ticks spread apart with progress squared; the last one lands on the reveal.
    let t = 0;
    for (; t < REVEAL_AT - 0.02; ) {
      const p = t / REVEAL_AT;
      scheduleClick(start + t, 0.85);
      t += 0.03 + 0.34 * p * p; // ~0.03s spinning to ~0.37s settling
    }
    lastAt = REVEAL_AT;
  }

  if (endTimer !== null) window.clearTimeout(endTimer);
  setPlaying(true);
  endTimer = window.setTimeout(() => {
    endTimer = null;
    setPlaying(false);
  }, (lastAt + TAIL_S) * 1000 + 60);
}

export function stopDrawJingle(): void {
  if (endTimer !== null) {
    window.clearTimeout(endTimer);
    endTimer = null;
  }
  setPlaying(false);
  if (!ctx || !playGain) return;
  const now = ctx.currentTime;
  playGain.gain.cancelScheduledValues(now);
  playGain.gain.setValueAtTime(Math.max(playGain.gain.value, 0.0001), now);
  playGain.gain.exponentialRampToValueAtTime(0.0001, now + 0.15);
  for (const s of voices) {
    try {
      s.stop(now + 0.16);
    } catch {
      // already stopped
    }
  }
  voices = [];
}
