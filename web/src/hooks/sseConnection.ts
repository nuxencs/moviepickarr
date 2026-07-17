/* ============================================================
   moviepickarr — the SSE connection reducer.

   The pure bookkeeping of one SSE session: the seq gap-detection cursor, the
   broker epoch (server-restart detection), and the reconnect backoff ladder.
   useSSE adapts EventSource to it — every frame becomes

       reduceConnection(state, frame, random01) -> [state, actions]

   and the hook just executes the returned actions (resync, reconnect after a
   delay, log). No DOM, no timers, no sockets in here, so the rules that keep
   realtime honest on flaky connections run as plain-value tests
   (sseConnection.test.ts).
   ============================================================ */

// Reconnect backoff bounds. The client owns reconnection rather than leaning
// on the native EventSource auto-reconnect, which has no backoff/jitter and
// gives up permanently on a terminal CLOSED state (e.g. a 502 from nginx
// during a deploy).
export const RECONNECT_BASE_MS = 1_000;
export const RECONNECT_MAX_MS = 30_000;
export const RECONNECT_JITTER = 0.3;

export interface ConnState {
  /** Gap-detection cursor: the last seq processed; null before alignment. */
  lastSeq: number | null;
  /** The broker epoch connected to; a change means the server restarted. */
  epoch: string | null;
  /** Position on the backoff ladder (consecutive failed connects). */
  attempts: number;
  /** False until the first app-level connected frame: the initial mount's
   *  queries already fetch fresh state, so only REconnects resync. */
  everConnected: boolean;
}

export const initialConnState: ConnState = {
  lastSeq: null,
  epoch: null,
  attempts: 0,
  everConnected: false,
};

export type ConnFrame =
  /** The app-level handshake (`event: connected`). */
  | { kind: "connected"; seq?: number; epoch?: string }
  /** A live domain event with its broker seq. */
  | { kind: "event"; seq?: number }
  /** The idle keep-alive carrying the broker's head seq. */
  | { kind: "heartbeat"; seq?: number }
  /** Transport error — the socket is (being) torn down. */
  | { kind: "error" }
  /** A refocus found the socket dead; the hook reconnects immediately and
   *  the ladder resets. */
  | { kind: "reset" };

export type ConnAction =
  | { action: "resync" }
  | { action: "reconnect"; delayMs: number }
  | { action: "log"; level: "log" | "warn" | "error"; message: string };

const NONE: ConnAction[] = [];

/** random01 feeds the reconnect jitter — passed in so the reducer stays pure
 *  (tests pin it; the hook passes Math.random()). */
export function reduceConnection(state: ConnState, frame: ConnFrame, random01: number): [ConnState, ConnAction[]] {
  switch (frame.kind) {
    case "connected": {
      const restarted = !!frame.epoch && state.epoch !== null && state.epoch !== frame.epoch;
      const actions: ConnAction[] = [
        { action: "log", level: "log", message: `[SSE] Connected to event stream${restarted ? " (server restarted)" : ""}` },
      ];
      // Resync on every REconnect — a down socket may have missed events (a
      // restart included). The very first connect skips it: mount queries are
      // already fetching fresh state.
      if (state.everConnected) actions.push({ action: "resync" });
      return [
        {
          // Align the cursor to the head: every event the broker assigns from
          // here is head+1, so the first live frame isn't read as a gap. On a
          // restart the broker's seq resets, so this also clears a stale
          // (higher) cursor that would otherwise spuriously trip the detector.
          lastSeq: typeof frame.seq === "number" ? frame.seq : state.lastSeq,
          epoch: frame.epoch ?? state.epoch,
          attempts: 0,
          everConnected: true,
        },
        actions,
      ];
    }

    case "event": {
      if (typeof frame.seq !== "number") return [state, NONE];
      // A jump in seq means a frame was dropped (a full client buffer).
      // Resync once to heal — the caller still runs this event's own
      // invalidation row afterwards, so the frame that *revealed* the gap
      // isn't itself lost.
      const gap = state.lastSeq !== null && frame.seq !== state.lastSeq + 1;
      const next = { ...state, lastSeq: frame.seq };
      if (!gap) return [next, NONE];
      return [next, [
        { action: "log", level: "warn", message: `[SSE] seq gap: expected ${state.lastSeq! + 1}, got ${frame.seq}; resyncing` },
        { action: "resync" },
      ]];
    }

    case "heartbeat": {
      // Passive idle gap check: the broker's head moved past our cursor while
      // we sat idle → a frame was dropped. This is what lets a healthy tab
      // refocus skip a blanket resync.
      if (typeof frame.seq !== "number" || state.lastSeq === null || frame.seq <= state.lastSeq) {
        return [state, NONE];
      }
      return [{ ...state, lastSeq: frame.seq }, [
        { action: "log", level: "warn", message: `[SSE] heartbeat gap: head ${frame.seq} > cursor ${state.lastSeq}; resyncing` },
        { action: "resync" },
      ]];
    }

    case "error": {
      const backoff = Math.min(RECONNECT_MAX_MS, RECONNECT_BASE_MS * 2 ** state.attempts);
      const delayMs = backoff + random01 * RECONNECT_JITTER * backoff;
      return [{ ...state, attempts: state.attempts + 1 }, [
        { action: "log", level: "error", message: "[SSE] Connection error; scheduling reconnect" },
        { action: "reconnect", delayMs },
      ]];
    }

    case "reset":
      return [{ ...state, attempts: 0 }, NONE];
  }
}
