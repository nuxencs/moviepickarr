import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";

import { applyImmediateLifecycleState } from "@/api/poolStateCache";
import { MoviesKeys } from "@/api/query_keys";

import { drawStore } from "@/components/moviepickarr/drawStore";

import type { Movie } from "@/types/Response";
import type { SSEConnectedFrame, SSEEvent, SSEHeartbeatFrame } from "@/types/SSEEvent";

import { type ConnFrame, initialConnState, reduceConnection } from "@/hooks/sseConnection";
import { createInvalidationQueue, timeoutScheduler } from "@/hooks/sseInvalidationQueue";
import { invalidationsFor, resyncKeys } from "@/hooks/sseInvalidations";


function baseURL(): string {
  // Empty in dev so the EventSource connects same-origin via the Vite proxy
  // (see vite.config.ts), matching APIClient.
  if (import.meta.env.DEV) {
    return "";
  }

  return window.location.origin;
}

/**
 * The EventSource adapter. The two things that make realtime honest live in
 * pure modules, and this hook only wires the browser to them:
 *
 * - sseConnection.ts owns the bookkeeping (seq gap-detection cursor, broker
 *   epoch, reconnect backoff ladder), every frame is fed through its reducer
 *   and the returned actions (resync / reconnect / log) run here.
 * - sseInvalidations.ts owns the event → query-key table; the per-event
 *   dispatch walks a row, and resync() derives its key set from the union.
 * - sseInvalidationQueue.ts coalesces those keys, so a bulk operation emitting
 *   one event per item costs one refetch per distinct key, not per event.
 *
 * Draw events additionally drive the draw machine (drawStore).
 */
export function useSSE() {
  const queryClient = useQueryClient();
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const closedRef = useRef(false);
  // Connection bookkeeping, owned by the pure reducer. Persists across
  // reconnects within a mount; resets on remount (which re-fetches anyway).
  const connRef = useRef(initialConnState);

  useEffect(() => {
    closedRef.current = false;

    // The pool refresh is held while a draw-reveal spin is in flight: the
    // post-draw pool no longer holds the winner, so refetching it mid-spin
    // would drop the winner tile from the grid and spoil the reveal. The draw
    // machine releases the pool itself when the reel lands, so a dropped
    // refresh is picked up there rather than lost. The check runs at flush
    // time, not enqueue time: the queue's window means a spin can start after
    // a key was collected.
    const [poolNs, poolSub] = MoviesKeys.listpool();
    const held = (key: readonly unknown[]) =>
      key[0] === poolNs && key[1] === poolSub && drawStore.getState().phase !== "idle";

    // One invalidation per distinct key per window, whoever queued it.
    const queue = createInvalidationQueue((keys) => {
      for (const key of keys) {
        if (held(key)) continue;
        void queryClient.invalidateQueries({ queryKey: key });
      }
    }, timeoutScheduler);

    // Re-pull every cache an SSE event can touch: the union of the
    // invalidation table. Used to reconcile state after a reconnect or a
    // detected gap: SSE has no replay, so any event that fired while the
    // socket was down is lost; re-fetching current state recovers it.
    const resync = () => {
      queue.push(resyncKeys());
    };

    const clearReconnectTimer = () => {
      if (reconnectTimerRef.current !== null) {
        clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
    };

    const scheduleReconnect = (delayMs: number) => {
      if (closedRef.current || reconnectTimerRef.current !== null) {
        return;
      }
      reconnectTimerRef.current = setTimeout(() => {
        reconnectTimerRef.current = null;
        connect();
      }, delayMs);
    };

    // Feed one frame through the connection reducer and run its actions.
    const dispatch = (frame: ConnFrame) => {
      const [next, actions] = reduceConnection(connRef.current, frame, Math.random());
      connRef.current = next;
      for (const action of actions) {
        if (action.action === "resync") resync();
        else if (action.action === "reconnect") scheduleReconnect(action.delayMs);
        else console[action.level](action.message);
      }
    };

    const connect = () => {
      if (closedRef.current) {
        return;
      }

      // Tear down any prior socket before opening a fresh one.
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
      }

      const eventSource = new EventSource(`${baseURL()}/api/v1/events`);
      eventSourceRef.current = eventSource;

      eventSource.addEventListener("connected", (event) => {
        // The backoff ladder resets on the app-level `connected` frame, not on
        // bare transport `open`: an accept-then-drop intermediary (e.g. an LB
        // that 200s then closes a dead upstream during a deploy) fires `open`
        // without ever delivering `connected`, and resetting there would pin
        // the ladder at the ~1s floor forever instead of climbing to the cap.
        clearReconnectTimer();
        let frame: SSEConnectedFrame | null = null;
        try {
          frame = JSON.parse((event as MessageEvent).data) as SSEConnectedFrame;
        } catch (error) {
          console.error("[SSE] Error parsing connected frame:", error);
        }
        // The reducer aligns the gap cursor, detects a server restart via the
        // epoch, and resyncs on every reconnect (never the first connect).
        dispatch({ kind: "connected", seq: frame?.seq, epoch: frame?.epoch });
      });

      eventSource.addEventListener("message", (event) => {
        try {
          const sseEvent: SSEEvent = JSON.parse(event.data);

          // Gap detection first: a resync still runs this event's own row
          // below, so the frame that revealed the gap isn't itself lost.
          dispatch({ kind: "event", seq: sseEvent.seq });

          // Exact lifecycle facts land before the coalescing window: controls
          // never spend that window on the prior draw gate, and reveal updates
          // its cached detail status in the same turn as the gate.
          applyImmediateLifecycleState(queryClient, sseEvent);

          // Draw events drive the machine (identity dedup, the reel,
          // reveal-once, pool-release timing) on top of their table row.
          if (sseEvent.type === "movie:drawn") {
            const drawnMovie = sseEvent.data as Movie | undefined;
            if (drawnMovie) drawStore.send({ type: "DRAWN", movie: drawnMovie });
          } else if (sseEvent.type === "movie:revealed") {
            const data = sseEvent.data as { drawnAt?: string } | undefined;
            if (data?.drawnAt) drawStore.send({ type: "REVEALED", drawnAt: data.drawnAt });
          }

          const row = invalidationsFor(sseEvent.type);
          if (row === null) {
            console.warn("[SSE] Unknown event type:", sseEvent.type);
            return;
          }
          queue.push(row);
        } catch (error) {
          console.error("[SSE] Error parsing event:", error);
        }
      });

      eventSource.addEventListener("heartbeat", (event) => {
        try {
          const frame = JSON.parse((event as MessageEvent).data) as SSEHeartbeatFrame;
          dispatch({ kind: "heartbeat", seq: frame.seq });
        } catch (error) {
          console.error("[SSE] Error parsing heartbeat frame:", error);
        }
      });

      eventSource.addEventListener("error", () => {
        // The native error event carries no useful detail. Own the
        // reconnection: close immediately (so native auto-reconnect can't
        // race our backoff) and let the reducer schedule a jittered retry.
        // This also recovers from terminal CLOSED states the browser would
        // otherwise never retry.
        eventSource.close();
        if (eventSourceRef.current === eventSource) {
          eventSourceRef.current = null;
        }
        dispatch({ kind: "error" });
      });
    };

    const handleVisibility = () => {
      if (document.visibilityState !== "visible") {
        return;
      }
      // On refocus, a backgrounded tab may have had its connection silently
      // killed (frozen tab / a dead socket the browser hasn't noticed yet). If
      // the stream isn't open, reconnect now and reset the backoff. If it IS
      // open, do nothing: the heartbeat proved liveness and the seq cursor
      // (live + heartbeat gap checks) already caught anything missed: a
      // blanket resync here would just over-fetch on every healthy refocus.
      const es = eventSourceRef.current;
      if (!es || es.readyState !== EventSource.OPEN) {
        dispatch({ kind: "reset" });
        clearReconnectTimer();
        connect();
      }
    };

    document.addEventListener("visibilitychange", handleVisibility);
    connect();

    // Cleanup on unmount
    return () => {
      closedRef.current = true;
      queue.cancel();
      document.removeEventListener("visibilitychange", handleVisibility);
      clearReconnectTimer();
      if (eventSourceRef.current) {
        console.log("[SSE] Closing connection");
        eventSourceRef.current.close();
        eventSourceRef.current = null;
      }
    };
  }, [queryClient]);
}
