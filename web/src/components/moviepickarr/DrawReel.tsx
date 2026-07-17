import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

import { type SpinDescriptor } from "@/components/moviepickarr/drawMachine";
import { reelEaseOutput, reelEaseTimeAt, spinDurationMs } from "@/components/moviepickarr/drawSpin";
import { backdropUrl, hueOf } from "@/components/moviepickarr/lib";
import { Poster } from "@/components/moviepickarr/Poster";

import type { Movie } from "@/types/Response";

import { isAudioRunning, playDrawJingle } from "@/lib/sound";

/** Roughly how many decoy tiles to scroll before the winner — enough for a long
 *  spin, while the per-draw cap keeps the DOM light (no virtualization needed). */
const TARGET_LEAD = 48;

/** Trailing tiles past the winner. Enough to overflow the viewport's right rim
 *  (~1100px max-width over ~132px tiles) so the strip never ends on the winner;
 *  the pool loops to fill them, so a small pool reads as endless as a large one. */
const TARGET_TRAIL = 6;

interface DrawReelProps {
  spin: SpinDescriptor;
  /** The reel finished (or skipped) its scroll and rests on the winner. The
   *  draw machine settles and schedules the self-heal fallback off this. */
  onScrollDone: () => void;
  /** The drawer confirmed (OK press / Escape). The machine owns reveal-once,
   *  so this needs no local guard — a duplicate send is silent. */
  onConfirm: () => void;
}

/**
 * The slot-machine draw reveal: a horizontal reel of pool-candidate posters that
 * scrolls and decelerates onto the server-chosen winner, then *settles* and waits
 * for confirmation rather than auto-closing. The drawer sees an OK button whose
 * fill counts down to the server's reveal deadline (spin.confirmMs); pressing it
 * (or the server's auto-reveal broadcast) closes the reel for everyone. Motion is
 * a JS-measured target + a CSS transition — the same "measure then transition"
 * idiom as the FLIP rails — so no animation library.
 */
export function DrawReel({ spin, onScrollDone, onConfirm }: DrawReelProps) {
  const viewportRef = useRef<HTMLDivElement>(null);
  const trackRef = useRef<HTMLDivElement>(null);
  const winnerRef = useRef<HTMLDivElement>(null);
  const skipRef = useRef<HTMLButtonElement>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);
  const settledRef = useRef(false);
  const targetXRef = useRef<number | null>(null);

  // The reel has scrolled to rest on the winner and now awaits confirmation.
  const [settled, setSettled] = useState(false);

  // Built once per draw (drawnAt identity). The pool repeats in its natural
  // order for a long run, then the winner, then a few trailing tiles so the
  // winner isn't the very last cell.
  const { strip, winnerIndex } = useMemo(() => {
    const cands = spin.candidates;
    const winnerPos = cands.findIndex((m) => m.movieID === spin.winnerId);
    const winnerAt = winnerPos >= 0 ? winnerPos : cands.length - 1;
    const winner = cands[winnerAt];
    const loops = Math.max(6, Math.ceil(TARGET_LEAD / cands.length));
    const lead: Movie[] = [];
    for (let i = 0; i < loops; i++) lead.push(...cands);
    // Avoid a double-poster at the landing seam: if the lead already ends on the
    // winner, drop that tile so the winner cell isn't next to an identical copy.
    if (lead.length && lead[lead.length - 1].movieID === winner.movieID) lead.pop();
    // Trail: keep looping the pool past the winner so the strip always fills the
    // viewport's right half and reads as one endless reel — even for small pools,
    // where a fixed unique-slice would run out and leave a gap after the winner.
    // Start one slot past the winner so its right neighbour is never an identical
    // copy; the loop may repeat the winner far down the trail, which is fine.
    const trail: Movie[] = [];
    for (let i = 1; i <= TARGET_TRAIL; i++) trail.push(cands[(winnerAt + i) % cands.length]);
    return { strip: [...lead, winner, ...trail], winnerIndex: lead.length };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [spin.drawnAt]);

  // The reel has rested on the winner; show the confirm controls and tell the
  // machine (which starts the fallback countdown). Does NOT close — that
  // waits for the confirm/reveal.
  const settle = useCallback(() => {
    if (settledRef.current) return;
    settledRef.current = true;
    setSettled(true);
    onScrollDone();
  }, [onScrollDone]);

  // Skip the scroll: snap the track onto the winner and settle (still awaits
  // confirmation — skipping fast-forwards the animation, it doesn't reveal).
  const skip = useCallback(() => {
    const track = trackRef.current;
    if (track && targetXRef.current != null) {
      track.style.transition = "none";
      track.style.transform = `translate3d(${targetXRef.current}px, 0, 0)`;
    }
    settle();
  }, [settle]);

  // Warm the winner's backdrop while the reel spins, so the Hero's decode-then-
  // commit handoff paints in one frame instead of waiting on the network.
  useEffect(() => {
    const winner = spin.candidates.find((m) => m.movieID === spin.winnerId);
    const url = winner?.backdropPath ? backdropUrl(winner.backdropPath) : null;
    if (url) new Image().src = url;
  }, [spin]);

  // Keyboard: while scrolling, Escape skips ahead; once settled, Escape confirms
  // (the drawer only — spectators can't close the reel for everyone).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      if (!settledRef.current) skip();
      else if (spin.mine) onConfirm();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [skip, onConfirm, spin.mine]);

  // Move focus to whichever control is live, for keyboard users.
  useEffect(() => {
    if (settled) {
      if (spin.mine) confirmRef.current?.focus();
    } else {
      skipRef.current?.focus();
    }
  }, [settled, spin.mine]);

  // The SERVER owns the auto-reveal: once a draw settles it broadcasts
  // movie:revealed at the confirm deadline, so every client closes off that one
  // broadcast in lockstep — even a backgrounded, timer-throttled tab. The
  // dropped-frame fallback timer lives in the draw machine (scheduled on
  // settle), not here.

  // Measure the winner tile, draw a within-tile landing offset (jitter, so it can
  // rest near a border for excitement while the reticle still sits over it), then
  // glide the track to that offset with the reel easing — and settle on arrival.
  useLayoutEffect(() => {
    const track = trackRef.current;
    const viewport = viewportRef.current;
    const winnerEl = winnerRef.current;
    if (!track || !viewport || !winnerEl) {
      settle();
      return;
    }

    // All measurements are in the track's local (CSS px) space, where the
    // :root zoom ramp cancels out — so no effectiveZoom division is needed: the
    // translate and the tile offsets scale together under the same ancestor zoom.
    const vpCenter = viewport.clientWidth / 2;
    const winnerCenter = winnerEl.offsetLeft + winnerEl.offsetWidth / 2;
    const edgePad = Math.min(28, winnerEl.offsetWidth * 0.16);
    const reach = Math.max(0, winnerEl.offsetWidth / 2 - edgePad);
    const jitter = (Math.random() * 2 - 1) * reach;
    const targetX = vpCenter - winnerCenter + jitter;
    targetXRef.current = targetX;

    const full = spinDurationMs();
    const remaining = Math.max(150, Math.min(spin.durationMs, full));
    // Resume: enter the easing curve where it would already be, then ease the rest.
    // reelEaseOutput evaluates the actual --ease-reel cubic-bezier (not a polynomial
    // stand-in), so the resume start position matches the curve the transition glides.
    const startFrac = full > 0 ? 1 - remaining / full : 0;
    const startX = reelEaseOutput(startFrac) * targetX;

    track.style.transition = "none";
    track.style.transform = `translate3d(${startX}px, 0, 0)`;
    void track.offsetHeight; // commit the start position before transitioning
    track.style.transition = `transform ${remaining}ms var(--ease-reel)`;
    track.style.transform = `translate3d(${targetX}px, 0, 0)`;

    // Draw-sound sync: a click each time a poster gap crosses the reticle. As the
    // track slides startX→targetX, each gap reaches viewport centre at translateX =
    // vpCenter − gap; invert the reel easing per gap so the click train decelerates
    // on the exact curve the posters ride. Computed here (where the geometry lives)
    // and handed to the synth, which schedules each tick sample-accurately.
    const span = targetX - startX;
    const clickTimes: number[] = [];
    if (Math.abs(span) > 1) {
      const tiles = track.children;
      for (let i = 1; i < tiles.length; i++) {
        const prev = tiles[i - 1] as HTMLElement;
        const cur = tiles[i] as HTMLElement;
        const gapCenter = (prev.offsetLeft + prev.offsetWidth + cur.offsetLeft) / 2;
        const frac = (vpCenter - gapCenter - startX) / span;
        if (frac <= 0 || frac >= 1) continue; // gap doesn't cross during this motion
        clickTimes.push((reelEaseTimeAt(frac) * remaining) / 1000);
      }
      clickTimes.sort((a, b) => a - b);
    }
    // Fresh draw: always sound (its context resumes off the click that started it).
    // Reload-resume: only join if audio is already running — a cold reload's context
    // is suspended, and scheduling onto it would replay the clicks shifted out of sync.
    if (spin.live || isAudioRunning()) playDrawJingle(clickTimes);

    const onEnd = (e: TransitionEvent) => {
      if (e.propertyName === "transform") settle();
    };
    track.addEventListener("transitionend", onEnd);
    // Safety net if transitionend is missed (tab backgrounded, RM instant-skip).
    const timer = window.setTimeout(settle, remaining + 150);
    return () => {
      track.removeEventListener("transitionend", onEnd);
      window.clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="drawreel" role="dialog" aria-modal="true" aria-label="Drawing a random movie">
      <div className="drawreel__label eyebrow">{settled ? "Your draw" : "Drawing…"}</div>
      <div className="drawreel__viewport" ref={viewportRef}>
        <div className="drawreel__track" ref={trackRef}>
          {strip.map((m, i) => (
            <div className="drawreel__tile" key={i} ref={i === winnerIndex ? winnerRef : undefined}>
              <Poster
                title={m.title}
                hue={hueOf(m.title)}
                posterPath={m.posterPath}
                showTitle={false}
                voteAverage={m.voteAverage}
              />
            </div>
          ))}
        </div>
        <div className="drawreel__reticle" aria-hidden="true" />
      </div>

      <div className="drawreel__controls">
        {!settled ? (
          <button type="button" className="drawreel__skip" ref={skipRef} onClick={skip}>
            Skip
          </button>
        ) : spin.mine ? (
          <button type="button" className="btn btn--accent drawreel__ok" ref={confirmRef} onClick={onConfirm}>
            {/* The fill counts down to the server's reveal deadline — its
                duration comes from the spin, not the --dur-confirm token. */}
            <span
              className="drawreel__ok-fill"
              style={{ animationDuration: `${spin.confirmMs}ms` }}
              aria-hidden="true"
            />
            <span className="drawreel__ok-label">OK</span>
          </button>
        ) : (
          <div className="drawreel__waiting" role="status">
            Waiting for the drawer…
          </div>
        )}
      </div>
    </div>
  );
}
