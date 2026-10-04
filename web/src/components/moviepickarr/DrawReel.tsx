import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

import {
  confirmRemainingMs,
  type DrawPhase,
  reelResume,
  type SpinDescriptor,
} from "@/components/moviepickarr/drawMachine";
import { reelEaseOutput, reelEaseTimeAt, spinDurationMs } from "@/components/moviepickarr/drawSpin";
import { backdropUrl, hueOf } from "@/components/moviepickarr/lib";
import { Poster } from "@/components/moviepickarr/Poster";

import type { MovieTile } from "@/types/Response";

import { isAudioRunning, playDrawJingle } from "@/lib/sound";

/** Decoy tiles before the winner: a long spin, but a DOM light enough to skip virtualization. */
const TARGET_LEAD = 48;

/** Trailing tiles: enough to overflow the right rim (~1100px over ~132px tiles). */
const TARGET_TRAIL = 6;

interface DrawReelProps {
  spin: SpinDescriptor;
  /** Read at mount only (see reelResume); the reel drives its own settle after that. */
  phase: DrawPhase;
  /** Turn-gated, not by which client drew. Errs open while loading; not_next_up is the backstop. */
  canReveal: boolean;
  /** Tooltip for the disabled OK. */
  revealTip: string;
  /** The reel finished or skipped its scroll. */
  onScrollDone: () => void;
  /** OK or Escape. No local guard needed: the machine owns reveal-once. */
  onConfirm: () => void;
}

/**
 * Slot-machine reel that decelerates onto the server-chosen winner, then settles and
 * waits for the OK or the server's auto-reveal. Motion is a JS-measured target plus a
 * CSS transition, as in the FLIP rails, so no animation library.
 */
export function DrawReel({ spin, phase, canReveal, revealTip, onScrollDone, onConfirm }: DrawReelProps) {
  const viewportRef = useRef<HTMLDivElement>(null);
  const trackRef = useRef<HTMLDivElement>(null);
  const winnerRef = useRef<HTMLDivElement>(null);
  const skipRef = useRef<HTMLButtonElement>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);
  const targetXRef = useRef<number | null>(null);

  const [resume] = useState(() => reelResume(spin, phase, Date.now()));

  const settledRef = useRef(resume.settled);
  const [settled, setSettled] = useState(resume.settled);
  const [confirmMs, setConfirmMs] = useState(() => (resume.settled ? confirmRemainingMs(spin, Date.now()) : 0));

  // Built once per draw (drawnAt identity).
  const { strip, winnerIndex } = useMemo(() => {
    const cands = spin.candidates;
    const winnerPos = cands.findIndex((m) => m.movieID === spin.winner.movieID);
    const winnerAt = winnerPos >= 0 ? winnerPos : cands.length - 1;
    const winner = cands[winnerAt];
    const loops = Math.max(6, Math.ceil(TARGET_LEAD / cands.length));
    const lead: MovieTile[] = [];
    for (let i = 0; i < loops; i++) lead.push(...cands);
    // No identical poster next to the winner at the landing seam.
    if (lead.length && lead[lead.length - 1].movieID === winner.movieID) lead.pop();
    // Loop the pool so small pools still fill the trail; start one past the winner for the same seam reason.
    const trail: MovieTile[] = [];
    for (let i = 1; i <= TARGET_TRAIL; i++) trail.push(cands[(winnerAt + i) % cands.length]);
    return { strip: [...lead, winner, ...trail], winnerIndex: lead.length };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [spin.drawnAt]);

  // Does not close the reel: that waits for the confirm or reveal.
  const settle = useCallback(() => {
    if (settledRef.current) return;
    settledRef.current = true;
    setConfirmMs(confirmRemainingMs(spin, Date.now()));
    setSettled(true);
    onScrollDone();
  }, [onScrollDone, spin]);

  // Fast-forwards the animation only; it does not reveal.
  const skip = useCallback(() => {
    const track = trackRef.current;
    if (track && targetXRef.current != null) {
      track.style.transition = "none";
      track.style.transform = `translate3d(${targetXRef.current}px, 0, 0)`;
    }
    settle();
  }, [settle]);

  // Warm the winner's backdrop so the Hero handoff does not wait on the network.
  useEffect(() => {
    const url = spin.winner.backdropPath ? backdropUrl(spin.winner.backdropPath) : null;
    if (url) new Image().src = url;
  }, [spin]);

  // Escape skips, then confirms; spectators cannot close the reel for everyone.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      if (!settledRef.current) skip();
      else if (canReveal) onConfirm();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [skip, onConfirm, canReveal]);

  useEffect(() => {
    if (settled) {
      if (canReveal) confirmRef.current?.focus();
    } else {
      skipRef.current?.focus();
    }
  }, [settled, canReveal]);

  // Jitter the landing within the winner tile so it can rest near a border.
  useLayoutEffect(() => {
    const track = trackRef.current;
    const viewport = viewportRef.current;
    const winnerEl = winnerRef.current;
    if (!track || !viewport || !winnerEl) {
      settle();
      return;
    }

    const vpCenter = viewport.clientWidth / 2;
    const winnerCenter = winnerEl.offsetLeft + winnerEl.offsetWidth / 2;
    const edgePad = Math.min(28, winnerEl.offsetWidth * 0.16);
    const reach = Math.max(0, winnerEl.offsetWidth / 2 - edgePad);
    const jitter = (Math.random() * 2 - 1) * reach;
    const targetX = vpCenter - winnerCenter + jitter;
    targetXRef.current = targetX;

    const full = spinDurationMs();

    // Already landed: a replay would spend the confirm window and the reveal would close it mid-replay.
    if (resume.settled) {
      track.style.transition = "none";
      track.style.transform = `translate3d(${targetX}px, 0, 0)`;
      // A scroll that ran out while unmounted leaves the machine `spinning`; a duplicate report is silent.
      onScrollDone();
      return;
    }

    const remaining = Math.max(150, Math.min(resume.remainingMs, full));
    // Resume: enter the easing curve where it would already be.
    const startFrac = full > 0 ? 1 - remaining / full : 0;
    const startX = reelEaseOutput(startFrac) * targetX;

    track.style.transition = "none";
    track.style.transform = `translate3d(${startX}px, 0, 0)`;
    void track.offsetHeight; // commit the start position before transitioning
    track.style.transition = `transform ${remaining}ms var(--ease-reel)`;
    track.style.transform = `translate3d(${targetX}px, 0, 0)`;

    // A click per poster gap crossing the reticle, timed by inverting the reel easing per gap.
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
    // A cold reload's audio context is suspended; scheduling onto it would shift the clicks out of sync.
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
        ) : (
          <button
            type="button"
            className="btn btn--accent drawreel__ok"
            ref={confirmRef}
            onClick={() => {
              if (canReveal) onConfirm();
            }}
            aria-disabled={!canReveal || undefined}
            aria-label={!canReveal ? `OK, ${revealTip}` : undefined}
            title={!canReveal ? revealTip : undefined}
          >
            {/* Duration comes from the spin, not --dur-confirm. Only the drawing client sees it. */}
            {canReveal && spin.mine && (
              <span
                className="drawreel__ok-fill"
                style={{ animationDuration: `${confirmMs}ms` }}
                aria-hidden="true"
              />
            )}
            <span className="drawreel__ok-label">OK</span>
          </button>
        )}
      </div>
    </div>
  );
}
