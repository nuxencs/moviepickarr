// Pure SSE session bookkeeping (seq gap cursor, broker epoch, backoff ladder).
// useSSE feeds every frame in and runs the returned actions.

// The client owns reconnection: native EventSource retry has no backoff and
// gives up for good on a terminal CLOSED state (a 502 during a deploy).
export const RECONNECT_BASE_MS = 1_000;
export const RECONNECT_MAX_MS = 30_000;
export const RECONNECT_JITTER = 0.3;

export interface ConnState {
  /** Gap-detection cursor: the last seq processed; null before alignment. */
  lastSeq: number | null;
  /** The broker epoch connected to; a change means the server restarted. */
  epoch: string | null;
  /** Consecutive failed connects. */
  attempts: number;
  /** Only reconnects resync: mount queries already fetch fresh state. */
  everConnected: boolean;
}

export const initialConnState: ConnState = {
  lastSeq: null,
  epoch: null,
  attempts: 0,
  everConnected: false,
};

export type ConnFrame =
  | { kind: "connected"; seq?: number; epoch?: string }
  | { kind: "event"; seq?: number }
  /** Idle keep-alive carrying the broker's head seq. */
  | { kind: "heartbeat"; seq?: number }
  | { kind: "error" }
  /** A refocus found the socket dead; the ladder resets. */
  | { kind: "reset" };

export type ConnAction =
  | { action: "resync" }
  | { action: "reconnect"; delayMs: number }
  | { action: "log"; level: "log" | "warn" | "error"; message: string };

const NONE: ConnAction[] = [];

/** random01 feeds the jitter, passed in so the reducer stays pure. */
export function reduceConnection(state: ConnState, frame: ConnFrame, random01: number): [ConnState, ConnAction[]] {
  switch (frame.kind) {
    case "connected": {
      const restarted = !!frame.epoch && state.epoch !== null && state.epoch !== frame.epoch;
      const actions: ConnAction[] = [
        { action: "log", level: "log", message: `[SSE] Connected to event stream${restarted ? " (server restarted)" : ""}` },
      ];
      if (state.everConnected) actions.push({ action: "resync" });
      return [
        {
          // Align to the head so the first live frame is not a gap; this also
          // clears a stale higher cursor after a server restart.
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
      // A seq jump means a dropped frame (full client buffer).
      const gap = state.lastSeq !== null && frame.seq !== state.lastSeq + 1;
      const next = { ...state, lastSeq: frame.seq };
      if (!gap) return [next, NONE];
      return [next, [
        { action: "log", level: "warn", message: `[SSE] seq gap: expected ${state.lastSeq! + 1}, got ${frame.seq}; resyncing` },
        { action: "resync" },
      ]];
    }

    case "heartbeat": {
      // Idle gap check; lets a healthy tab skip a resync on refocus.
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
