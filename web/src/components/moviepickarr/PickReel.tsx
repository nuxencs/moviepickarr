import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

import { backdropUrl, hueOf } from "@/components/moviepickarr/lib";
import { type ActiveSpin, spinDurationMs } from "@/components/moviepickarr/pickSpin";
import { Poster } from "@/components/moviepickarr/Poster";

import type { Movie } from "@/types/Response";

import { playPickJingle } from "@/lib/sound";

/** Roughly how many decoy tiles to scroll before the winner — enough for a long
 *  spin, while the per-pick cap keeps the DOM light (no virtualization needed). */
const TARGET_LEAD = 48;

/** Fallback confirm-countdown when the --dur-confirm token can't be read. */
const DEFAULT_CONFIRM_MS = 10000;

/** How long the settled reel waits for confirmation before auto-revealing. Read
 *  from --dur-confirm so the JS timer and the button's fill animation share one
 *  source of truth. */
function confirmDurationMs(): number {
  if (typeof window === "undefined") return DEFAULT_CONFIRM_MS;
  const raw = getComputedStyle(document.documentElement).getPropertyValue("--dur-confirm");
  const secs = parseFloat(raw);
  return Number.isFinite(secs) && secs > 0 ? Math.round(secs * 1000) : DEFAULT_CONFIRM_MS;
}

interface PickReelProps {
  spin: ActiveSpin;
  /** Called once when the pick is confirmed — the picker pressed OK, or the
   *  settled reel's countdown filled. The Hero closes the reel for every client
   *  (broadcasting movie:revealed) and hands off to its own pick-reveal. */
  onConfirm: () => void;
}

/**
 * The slot-machine pick reveal: a horizontal reel of pool-candidate posters that
 * scrolls and decelerates onto the server-chosen winner, then *settles* and waits
 * for confirmation rather than auto-closing. The picker sees an OK button whose
 * fill counts down ~10s; pressing it (or letting it fill) closes the reel for
 * everyone. Motion is a JS-measured target + a CSS transition — the same
 * "measure then transition" idiom as the FLIP rails — so no animation library.
 */
export function PickReel({ spin, onConfirm }: PickReelProps) {
  const viewportRef = useRef<HTMLDivElement>(null);
  const trackRef = useRef<HTMLDivElement>(null);
  const winnerRef = useRef<HTMLDivElement>(null);
  const skipRef = useRef<HTMLButtonElement>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);
  const settledRef = useRef(false);
  const confirmedRef = useRef(false);
  const targetXRef = useRef<number | null>(null);

  // The reel has scrolled to rest on the winner and now awaits confirmation.
  const [settled, setSettled] = useState(false);

  // Built once per pick (pickedAt identity). The pool repeats in its natural
  // order for a long run, then the winner, then a few trailing tiles so the
  // winner isn't the very last cell.
  const { strip, winnerIndex } = useMemo(() => {
    const cands = spin.candidates;
    const winner = cands.find((m) => m.movieID === spin.winnerId) ?? cands[cands.length - 1];
    const loops = Math.max(6, Math.ceil(TARGET_LEAD / cands.length));
    const lead: Movie[] = [];
    for (let i = 0; i < loops; i++) lead.push(...cands);
    // Avoid a double-poster at the landing seam: if the lead already ends on the
    // winner, drop that tile so the winner cell isn't next to an identical copy.
    if (lead.length && lead[lead.length - 1].movieID === winner.movieID) lead.pop();
    // Trail tiles sit just past the winner; keep the winner out so its right
    // neighbour can't be an identical copy either.
    const trail = cands.filter((m) => m.movieID !== winner.movieID).slice(0, 4);
    return { strip: [...lead, winner, ...trail], winnerIndex: lead.length };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [spin.pickedAt]);

  // The reel has rested on the winner; show the confirm controls + start the
  // countdown. Does NOT close — that waits for confirm().
  const settle = useCallback(() => {
    if (settledRef.current) return;
    settledRef.current = true;
    setSettled(true);
  }, []);

  // Hand off to the Hero exactly once (OK press, or the countdown elapsing).
  const confirm = useCallback(() => {
    if (confirmedRef.current) return;
    confirmedRef.current = true;
    onConfirm();
  }, [onConfirm]);

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
  // (the picker only — spectators can't close the reel for everyone).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      if (!settledRef.current) skip();
      else if (spin.mine) confirm();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [skip, confirm, spin.mine]);

  // Move focus to whichever control is live, for keyboard users.
  useEffect(() => {
    if (settled) {
      if (spin.mine) confirmRef.current?.focus();
    } else {
      skipRef.current?.focus();
    }
  }, [settled, spin.mine]);

  // Once settled, count down to an automatic confirm. Every client runs this so
  // the reel never hangs — normally the picker's OK (broadcast as movie:revealed)
  // closes everyone first; if the picker vanished, each client self-heals here.
  useEffect(() => {
    if (!settled) return;
    const t = window.setTimeout(confirm, confirmDurationMs());
    return () => window.clearTimeout(t);
  }, [settled, confirm]);

  // Measure the winner tile, pick a within-tile landing offset (jitter, so it can
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
    // Resume: enter the easing curve roughly where it would already be, then ease
    // the rest. easeOutQuad (1 − (1−t)²) here mirrors --ease-reel so the resume
    // start position matches the curve the transition will glide along.
    const startFrac = full > 0 ? 1 - remaining / full : 0;
    const easedStart = 1 - Math.pow(1 - startFrac, 2);
    const startX = easedStart * targetX;

    track.style.transition = "none";
    track.style.transform = `translate3d(${startX}px, 0, 0)`;
    void track.offsetHeight; // commit the start position before transitioning
    track.style.transition = `transform ${remaining}ms var(--ease-reel)`;
    track.style.transform = `translate3d(${targetX}px, 0, 0)`;

    // Fire the jingle in lockstep with the reel's first frame — fresh picks only;
    // a reload-resume starts mid-spin where the jingle's intro would be off-cue.
    if (spin.live) playPickJingle();

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
    <div className="pickreel" role="dialog" aria-modal="true" aria-label="Picking a random movie">
      <div className="pickreel__label eyebrow">{settled ? "Your pick" : "Picking…"}</div>
      <div className="pickreel__viewport" ref={viewportRef}>
        <div className="pickreel__track" ref={trackRef}>
          {strip.map((m, i) => (
            <div className="pickreel__tile" key={i} ref={i === winnerIndex ? winnerRef : undefined}>
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
        <div className="pickreel__reticle" aria-hidden="true" />
      </div>

      {!settled ? (
        <button type="button" className="pickreel__skip" ref={skipRef} onClick={skip}>
          Skip
        </button>
      ) : spin.mine ? (
        <button type="button" className="btn btn--accent pickreel__ok" ref={confirmRef} onClick={confirm}>
          <span className="pickreel__ok-fill" aria-hidden="true" />
          <span className="pickreel__ok-label">OK</span>
        </button>
      ) : (
        <div className="pickreel__waiting" role="status">
          Waiting for the picker…
        </div>
      )}
    </div>
  );
}
