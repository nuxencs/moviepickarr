import { describe, expect, it } from "vitest";

import {
  type DrawCommand,
  type DrawEnv,
  type DrawState,
  drawAwaitingReveal,
  confirmRemainingMs,
  initialDrawState,
  reduce,
  reelResume,
} from "@/components/moviepickarr/drawMachine";

import type { MovieDetail, MovieDrawPayload, MovieTile } from "@/types/Response";

const T0 = "2026-07-17T20:00:00Z";
const T0_PLUS = (ms: number) => new Date(Date.parse(T0) + ms).toISOString();
/** The client's wall clock when a spin is built. Arbitrary and independent of
 *  the server timestamps above: only differences against it are ever read. */
const NOW = 1_000_000;

function env(overrides: Partial<DrawEnv> = {}): DrawEnv {
  return {
    spinDurationMs: 6500,
    reducedMotion: false,
    clientId: "me",
    confirmFallbackMs: 10_000,
    fallbackGraceMs: 5_000,
    now: NOW,
    ...overrides,
  };
}

function tile(id: number, overrides: Partial<MovieTile> = {}): MovieTile {
  return {
    movieID: id,
    title: `Movie ${id}`,
    link: "",
    addedAt: T0,
    addedByID: 1,
    addedByName: "ana",
    ...overrides,
  };
}

function detail(id: number, overrides: Partial<MovieDetail> = {}): MovieDetail {
  return {
    ...tile(id),
    status: "pool",
    ...overrides,
  };
}

/** A drawn payload: winner + self-contained candidates, drawn at T0 with the
 *  server deadline at T0 + 16.5s (6.5s scroll + 10s confirm). */
function drawn(overrides: Partial<MovieDrawPayload> = {}): MovieDrawPayload {
  return {
    ...detail(1),
    drawnAt: T0,
    revealAt: T0_PLUS(16_500),
    drawClientId: "me",
    backdropPath: "/w.jpg",
    candidates: [tile(1, { posterPath: "/winner.jpg" }), tile(2), tile(3)],
    ...overrides,
  };
}

function spinning(overrides: Partial<MovieDrawPayload> = {}): DrawState {
  const [state] = reduce(initialDrawState, { type: "DRAWN", movie: drawn(overrides) }, env());
  return state;
}

function settled(overrides: Partial<MovieDrawPayload> = {}): DrawState {
  const [state] = reduce(spinning(overrides), { type: "SCROLL_DONE" }, env());
  return state;
}

const cmds = (commands: DrawCommand[]) => commands.map((c) => c.cmd);

describe("DRAWN", () => {
  it("starts a spin with the winner, deduped candidates, and full duration", () => {
    const [state, commands] = reduce(initialDrawState, { type: "DRAWN", movie: drawn() }, env());
    expect(state.phase).toBe("spinning");
    // The self-heal fallback is scheduled up front, draw-anchored: full scroll
    // (6.5s) + confirm window (10s) + grace (5s) = 21.5s past spin start.
    expect(commands).toEqual([{ cmd: "scheduleFallback", afterMs: 21_500 }]);
    expect(state.spin).toMatchObject({
      drawnAt: T0,
      winner: { movieID: 1, backdropPath: "/w.jpg" },
      durationMs: 6500,
      live: true,
      mine: true,
    });
    // Winner appears once even though it rides both the candidates and the payload.
    expect(state.spin!.candidates.map((m) => m.movieID)).toEqual([1, 2, 3]);
  });

  it("dedups by drawnAt: the SSE event after the mutation response is a no-op", () => {
    const first = spinning();
    const [second, commands] = reduce(first, { type: "DRAWN", movie: drawn() }, env());
    expect(second).toBe(first);
    expect(commands).toEqual([]);
  });

  it("derives mine from the draw's client id", () => {
    const [state] = reduce(initialDrawState, { type: "DRAWN", movie: drawn({ drawClientId: "someone-else" }) }, env());
    expect(state.spin!.mine).toBe(false);
  });

  it("anchors the reveal deadline to the server clock the payload was stamped with", () => {
    // The payload left the server 400ms after the draw, so 16.1s of the 16.5s
    // window is left when it lands here.
    const state = spinning({ serverNow: T0_PLUS(400) });
    expect(state.spin!.deadlineAtMs).toBe(NOW + 16_100);
  });

  it("falls back to drawnAt when the payload carries no serverNow", () => {
    const state = spinning();
    expect(state.spin!.deadlineAtMs).toBe(NOW + 16_500);
  });

  it("falls back to the default confirm window without a revealAt", () => {
    const state = spinning({ revealAt: undefined });
    // 6.5s scroll + the 10s default.
    expect(state.spin!.deadlineAtMs).toBe(NOW + 16_500);
  });

  it("keeps a second of confirm even when the deadline has already passed", () => {
    const state = spinning({ serverNow: T0_PLUS(20_000) });
    expect(state.spin!.deadlineAtMs).toBe(NOW + 7500);
  });

  it("skips the reel under reduced motion but still releases the pool refresh", () => {
    const [state, commands] = reduce(initialDrawState, { type: "DRAWN", movie: drawn() }, env({ reducedMotion: true }));
    expect(state.phase).toBe("idle");
    expect(state.seen).toContain(T0);
    expect(cmds(commands)).toEqual(["invalidatePool"]);
  });

  it("skips the reel for a pool of one (no decoys to scroll past)", () => {
    const lone = drawn({ candidates: [tile(1)] });
    const [state, commands] = reduce(initialDrawState, { type: "DRAWN", movie: lone }, env());
    expect(state.phase).toBe("idle");
    expect(cmds(commands)).toEqual(["invalidatePool"]);
  });
});

describe("RESUME", () => {
  const current = (overrides: Partial<MovieDetail> = {}) =>
    detail(1, {
      drawnAt: T0,
      revealAt: T0_PLUS(16_500),
      serverNow: T0_PLUS(2000),
      drawClientId: "other",
      ...overrides,
    });
  const pool = [tile(2), tile(3)];

  it("resumes with the remaining scroll time (server-relative, skew-free)", () => {
    const [state] = reduce(initialDrawState, { type: "RESUME", current: current(), pool }, env());
    expect(state.phase).toBe("spinning");
    expect(state.spin).toMatchObject({ durationMs: 4500, live: false, mine: false });
  });

  it("keeps the full current winner when the held pool tile is lean", () => {
    const heldPool = [tile(1, { posterPath: "/winner.jpg" }), ...pool];
    const [resuming] = reduce(
      initialDrawState,
      {
        type: "RESUME",
        current: current({ backdropPath: "/winner-backdrop.jpg" }),
        pool: heldPool,
      },
      env(),
    );

    expect(resuming.spin!.candidates.map((candidate) => candidate.movieID)).toEqual([1, 2, 3]);
    expect(resuming.spin!.candidates[0].posterPath).toBe("/winner.jpg");
    expect(resuming.spin!.winner.backdropPath).toBe("/winner-backdrop.jpg");

    const [settledState] = reduce(resuming, { type: "SCROLL_DONE" }, env());
    const [, commands] = reduce(settledState, { type: "REVEALED", drawnAt: T0 }, env());
    expect(commands).toContainEqual({
      cmd: "decode",
      drawnAt: T0,
      backdropPath: "/winner-backdrop.jpg",
    });
  });

  it("times the reveal deadline to the same absolute instant", () => {
    const [state] = reduce(initialDrawState, { type: "RESUME", current: current(), pool }, env());
    // 16.5s deadline − 2s already elapsed: this client reaches the reveal at
    // the same instant as everyone else's reel.
    expect(state.spin!.deadlineAtMs).toBe(NOW + 14_500);
  });

  it("schedules the draw-anchored self-heal fallback on resume", () => {
    const [, commands] = reduce(initialDrawState, { type: "RESUME", current: current(), pool }, env());
    // 14.5s left on the deadline + 5s grace: the same absolute revealAt + grace
    // instant as a fresh draw, measured from serverNow.
    expect(commands).toEqual([{ cmd: "scheduleFallback", afterMs: 19_500 }]);
  });

  it("snaps to the settled winner when the scroll already elapsed", () => {
    const late = current({ serverNow: T0_PLUS(8000) });
    const [state] = reduce(initialDrawState, { type: "RESUME", current: late, pool }, env());
    expect(state.spin!.durationMs).toBe(0);
    // 16.5 − 8 = 8.5s left on the shared deadline.
    expect(state.spin!.deadlineAtMs).toBe(NOW + 8500);
  });

  it("marks an already-revealed draw handled without spinning", () => {
    const [state, commands] = reduce(
      initialDrawState,
      { type: "RESUME", current: current({ revealed: true }), pool },
      env(),
    );
    expect(state.phase).toBe("idle");
    expect(state.seen).toContain(T0);
    expect(commands).toEqual([]);
  });

  it("never resumes a draw that already spun this session", () => {
    const done = settled();
    const [state, commands] = reduce(done, { type: "RESUME", current: current(), pool }, env());
    expect(state).toBe(done);
    expect(commands).toEqual([]);
  });

  it("drawAwaitingReveal holds the hero commit only for a pending reel", () => {
    expect(drawAwaitingReveal(current(), env())).toBe(true);
    expect(drawAwaitingReveal(current({ revealed: true }), env())).toBe(false);
    expect(drawAwaitingReveal(current(), env({ reducedMotion: true }))).toBe(false);
    expect(drawAwaitingReveal(detail(1), env())).toBe(false);
  });
});

describe("settle and reveal", () => {
  it("SCROLL_DONE settles without rescheduling the fallback (it is draw-anchored)", () => {
    const [state, commands] = reduce(spinning(), { type: "SCROLL_DONE" }, env());
    expect(state.phase).toBe("settled");
    // The fallback was scheduled at spin start, so an early skip (an early
    // SCROLL_DONE) can't pull it in: settling emits no new timer.
    expect(commands).toEqual([]);
  });

  it("decodes the full drawn winner backdrop when reel candidates are lean", () => {
    const [state, commands] = reduce(settled(), { type: "CONFIRM", source: "local" }, env());
    expect(state.phase).toBe("revealing");
    expect(commands).toEqual([
      { cmd: "cancelFallback" },
      { cmd: "postReveal" },
      { cmd: "decode", drawnAt: T0, backdropPath: "/w.jpg" },
    ]);
  });

  it("a remote confirm never posts back (it IS the server's broadcast)", () => {
    const [state, commands] = reduce(settled(), { type: "REVEALED", drawnAt: T0 }, env());
    expect(state.phase).toBe("revealing");
    expect(cmds(commands)).toEqual(["cancelFallback", "decode"]);
  });

  it("a remote reveal closes a reel that is still scrolling", () => {
    const [state] = reduce(spinning(), { type: "REVEALED", drawnAt: T0 }, env());
    expect(state.phase).toBe("revealing");
  });

  it("REVEALED for a different draw is ignored", () => {
    const before = settled();
    const [state, commands] = reduce(before, { type: "REVEALED", drawnAt: T0_PLUS(99) }, env());
    expect(state).toBe(before);
    expect(commands).toEqual([]);
  });

  it("reveals exactly once: every later confirm is silent", () => {
    const [revealing] = reduce(settled(), { type: "CONFIRM", source: "local" }, env());
    for (const dup of [
      { type: "CONFIRM", source: "local" },
      { type: "CONFIRM", source: "remote" },
      { type: "REVEALED", drawnAt: T0 },
    ] as const) {
      const [state, commands] = reduce(revealing, dup, env());
      expect(state).toBe(revealing);
      expect(commands).toEqual([]);
    }
  });
});

describe("commit", () => {
  it("DECODE_DONE commits: idle, commitSeq bump, pool refresh released", () => {
    const [revealing] = reduce(settled(), { type: "CONFIRM", source: "local" }, env());
    const [state, commands] = reduce(
      revealing,
      { type: "DECODE_DONE", drawnAt: T0, decodedBackdropPath: "/w.jpg" },
      env(),
    );
    expect(state).toMatchObject({
      phase: "idle",
      spin: null,
      commitSeq: 1,
      decodedBackdrop: { movieID: 1, drawnAt: T0, backdropPath: "/w.jpg" },
    });
    expect(cmds(commands)).toEqual(["invalidatePool"]);
  });

  it("does not expose artwork when the reveal decode failed", () => {
    const [revealing] = reduce(settled(), { type: "CONFIRM", source: "local" }, env());
    const [state] = reduce(
      revealing,
      { type: "DECODE_DONE", drawnAt: T0, decodedBackdropPath: null },
      env(),
    );

    expect(state.decodedBackdrop).toBeNull();
  });

  it("a stale decode from an outlived draw never commits", () => {
    const [revealing] = reduce(settled(), { type: "CONFIRM", source: "local" }, env());
    const [state, commands] = reduce(
      revealing,
      { type: "DECODE_DONE", drawnAt: T0_PLUS(99), decodedBackdropPath: "/w.jpg" },
      env(),
    );
    expect(state).toBe(revealing);
    expect(commands).toEqual([]);
  });

  it("a full cycle leaves the machine ready for the next draw", () => {
    let state = initialDrawState;
    const step = (event: Parameters<typeof reduce>[1]) => {
      [state] = reduce(state, event, env());
    };
    step({ type: "DRAWN", movie: drawn() });
    step({ type: "SCROLL_DONE" });
    step({ type: "CONFIRM", source: "local" });
    step({ type: "DECODE_DONE", drawnAt: T0, decodedBackdropPath: "/w.jpg" });
    expect(state).toMatchObject({ phase: "idle", commitSeq: 1 });

    const nextDrawnAt = T0_PLUS(60_000);
    step({
      type: "DRAWN",
      movie: drawn({ drawnAt: nextDrawnAt, revealAt: T0_PLUS(76_500) }),
    });
    expect(state.phase).toBe("spinning");
    expect(state.spin!.drawnAt).toBe(nextDrawnAt);
  });
});

describe("confirm bar", () => {
  const spin = () => spinning({ serverNow: T0 }).spin!;

  it("runs for whatever is left of the deadline when it starts", () => {
    // Straight after the scroll: the full confirm window.
    expect(confirmRemainingMs(spin(), NOW + 6500)).toBe(10_000);
  });

  it("shortens when the scroll was skipped or the bar mounts late", () => {
    expect(confirmRemainingMs(spin(), NOW + 1000)).toBe(15_500);
    expect(confirmRemainingMs(spin(), NOW + 12_000)).toBe(4500);
  });

  it("never runs backwards once the deadline has passed", () => {
    expect(confirmRemainingMs(spin(), NOW + 20_000)).toBe(0);
  });
});

describe("reel resume", () => {
  const spin = () => spinning().spin!;

  it("a fresh mount scrolls the whole duration", () => {
    expect(reelResume(spin(), "spinning", NOW)).toEqual({ settled: false, remainingMs: 6500 });
  });

  it("a remount mid-scroll glides only the time that's left", () => {
    expect(reelResume(spin(), "spinning", NOW + 5000)).toEqual({ settled: false, remainingMs: 1500 });
  });

  it("a remount once the draw has settled shows the confirm, no scroll", () => {
    expect(reelResume(spin(), "settled", NOW + 100)).toEqual({ settled: true, remainingMs: 0 });
    expect(reelResume(spin(), "revealing", NOW + 100)).toEqual({ settled: true, remainingMs: 0 });
  });

  it("a remount after the scroll window ran out settles straight away", () => {
    expect(reelResume(spin(), "spinning", NOW + 6500)).toEqual({ settled: true, remainingMs: 0 });
    expect(reelResume(spin(), "spinning", NOW + 20_000)).toEqual({ settled: true, remainingMs: 0 });
  });
});
