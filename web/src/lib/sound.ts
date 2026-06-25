/* ============================================================
   moviepickarr — pick-reveal sound effect engine.

   One jingle, played when the slot-machine reel starts on a FRESH pick (see
   PickReel). The jingle is SYNTHESIZED at runtime with the native Web Audio API
   (no library, no audio file — nothing to license or bundle): a decelerating
   click train (a Wheel-of-Fortune flapper — fast ticks while spinning, slowing
   as it settles to a stop on the reveal). The deceleration itself is the payoff
   — no reveal stinger.

   The AudioProvider owns the on/off preference (mirrored to localStorage) and
   the one-time autoplay "unlock" (an AudioContext resume on the first gesture),
   so SSE-driven clients that didn't click Pick can still play once the visitor
   has interacted with the page at all.
   ============================================================ */

/** localStorage key for the on/off preference. Opt-out: anything but "off" = on. */
const STORAGE_KEY = "mp-sound";
/** localStorage key for the 0..1 playback volume. */
const VOLUME_KEY = "mp-volume";
const DEFAULT_VOLUME = 0.5;

/** When the wheel settles, in seconds from jingle start. Tuned to the reel: the
 *  slot spin runs for `--dur-spin` (6.5s, see DESIGN.md §7), so the final click
 *  lands just as the reveal settles on the picked movie. */
const REVEAL_AT = 6.4;
/** Tiny tail after the final click — total ≈ REVEAL_AT + this. */
const TAIL_S = 0.15;

let unlocked = false;
/** True while a full jingle is sounding. Drives the popover play/stop button. */
let playing = false;
/** Fires when the jingle's tail finishes, to flip `playing` back off. */
let endTimer: number | null = null;
const playListeners = new Set<(p: boolean) => void>();

// Audio graph (lazily built). clicks → clickFilter → playGain → comp → master → out.
let ctx: AudioContext | null = null;
let masterGain: GainNode | null = null;
let playGain: GainNode | null = null;
let clickFilter: BiquadFilterNode | null = null;
/** Reusable white-noise buffer — every click plays a short slice of it. */
let noiseBuf: AudioBuffer | null = null;
/** In-flight click sources, so a stop can cancel the rest of the train. */
let voices: AudioBufferSourceNode[] = [];

function clamp01(n: number): number {
  if (!Number.isFinite(n)) return DEFAULT_VOLUME;
  return Math.min(1, Math.max(0, n));
}

/** Current playback volume (0..1), from localStorage, default 0.5. */
export function getVolume(): number {
  if (typeof window === "undefined") return DEFAULT_VOLUME;
  const raw = localStorage.getItem(VOLUME_KEY);
  return raw === null ? DEFAULT_VOLUME : clamp01(parseFloat(raw));
}

/** Persist the playback volume and apply it live (so a playing jingle adjusts). */
export function setVolume(v: number): void {
  if (typeof window === "undefined") return;
  const vol = clamp01(v);
  localStorage.setItem(VOLUME_KEY, String(vol));
  if (masterGain && ctx) masterGain.gain.setTargetAtTime(vol, ctx.currentTime, 0.02);
}

/** Lazily build the audio graph + the shared noise buffer. */
function ensureGraph(): boolean {
  if (typeof window === "undefined") return false;
  if (ctx) return true;
  const AC = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
  if (!AC) return false;

  ctx = new AC();
  masterGain = ctx.createGain();
  masterGain.gain.value = getVolume();
  masterGain.connect(ctx.destination);

  // Tame any summed peaks (and keep parity with the prior Tone graph).
  const comp = ctx.createDynamicsCompressor();
  comp.threshold.value = -14;
  comp.ratio.value = 12;
  comp.attack.value = 0.003;
  comp.release.value = 0.25;
  comp.connect(masterGain);

  playGain = ctx.createGain();
  playGain.gain.value = 1;
  playGain.connect(comp);

  // Wheel-flapper click tone: a resonant bandpass on a short noise burst gives a
  // crisp, woody "tick".
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

/** Schedule one click (a short noise burst with a fast attack + decay). */
function scheduleClick(time: number, vel: number): void {
  if (!ctx || !clickFilter || !noiseBuf) return;
  const src = ctx.createBufferSource();
  src.buffer = noiseBuf;
  const g = ctx.createGain();
  g.gain.setValueAtTime(0.0001, time);
  g.gain.exponentialRampToValueAtTime(vel, time + 0.0006); // snappy attack
  g.gain.exponentialRampToValueAtTime(0.0001, time + 0.02); // fast decay
  src.connect(g);
  g.connect(clickFilter);
  src.start(time);
  src.stop(time + 0.05);
  voices.push(src);
}

/** Stop every in-flight click immediately (cancels the rest of the train). */
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

/** Whether a jingle is currently sounding. */
export function isJinglePlaying(): boolean {
  return playing;
}

/** Subscribe to play/stop transitions. Returns an unsubscribe fn. */
export function onJingleChange(cb: (p: boolean) => void): () => void {
  playListeners.add(cb);
  return () => playListeners.delete(cb);
}

/** Kick off context + buffer creation early (from the AudioProvider) so the
 *  first pick's jingle starts the instant the reel does. */
export function preloadJingle(): void {
  ensureGraph();
}

/** Sound is on unless the user explicitly turned it off (opt-out, not opt-in). */
export function isSoundEnabled(): boolean {
  if (typeof window === "undefined") return false;
  return localStorage.getItem(STORAGE_KEY) !== "off";
}

export function setSoundEnabled(on: boolean): void {
  if (typeof window === "undefined") return;
  localStorage.setItem(STORAGE_KEY, on ? "on" : "off");
}

/** Satisfy the browser autoplay policy once, on the first real user gesture, by
 *  resuming the AudioContext. Later SSE-driven plays (which have no direct
 *  gesture of their own) are then allowed on the running context. */
export function unlockAudio(): void {
  if (unlocked) return;
  unlocked = true;
  if (!ensureGraph() || !ctx) return;
  if (ctx.state === "suspended") void ctx.resume();
}

/** Play the pick jingle from the top. No-op when sound is off. */
export function playPickJingle(): void {
  if (!isSoundEnabled()) return;
  if (!ensureGraph() || !ctx || !playGain) return;
  if (ctx.state === "suspended") void ctx.resume();
  killVoices();
  const now = ctx.currentTime;
  playGain.gain.cancelScheduledValues(now);
  playGain.gain.setValueAtTime(1, now); // reset after any prior fade-out

  // Wheel-of-Fortune click train: fast ticks while "spinning", spreading further
  // apart as the wheel slows (gap grows with progress²), the final tick landing
  // on the reveal. Steady volume — a flapper doesn't get quieter as it slows.
  const start = now + 0.05;
  for (let t = 0; t < REVEAL_AT - 0.02; ) {
    const p = t / REVEAL_AT; // 0..1 across the spin
    scheduleClick(start + t, 0.85);
    t += 0.03 + 0.34 * p * p; // decelerate: ~0.03s spinning → ~0.37s settling
  }

  if (endTimer !== null) window.clearTimeout(endTimer);
  setPlaying(true);
  endTimer = window.setTimeout(() => {
    endTimer = null;
    setPlaying(false);
  }, (REVEAL_AT + TAIL_S) * 1000);
}

/** Quickly fade out and stop the jingle. Used when the user SKIPS the reel and
 *  from the popover stop button. */
export function stopPickJingle(): void {
  if (endTimer !== null) {
    window.clearTimeout(endTimer);
    endTimer = null;
  }
  setPlaying(false);
  if (!ctx || !playGain) return;
  const now = ctx.currentTime;
  playGain.gain.cancelScheduledValues(now);
  playGain.gain.setValueAtTime(Math.max(playGain.gain.value, 0.0001), now);
  playGain.gain.exponentialRampToValueAtTime(0.0001, now + 0.15); // smooth fade
  for (const s of voices) {
    try {
      s.stop(now + 0.16);
    } catch {
      // already stopped
    }
  }
  voices = [];
}
