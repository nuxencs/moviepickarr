import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";

import { applyImmediateLifecycleState } from "@/api/poolStateCache";
import { MoviesKeys } from "@/api/query_keys";

import { drawStore } from "@/components/moviepickarr/drawStore";

import type { MovieDrawPayload } from "@/types/Response";
import type { SSEConnectedFrame, SSEEvent, SSEHeartbeatFrame } from "@/types/SSEEvent";

import { type ConnFrame, initialConnState, reduceConnection } from "@/hooks/sseConnection";
import { createInvalidationQueue, timeoutScheduler } from "@/hooks/sseInvalidationQueue";
import { invalidationsFor, resyncKeys } from "@/hooks/sseInvalidations";


function baseURL(): string {
  // Same-origin via the Vite proxy in dev (vite.config.ts), matching APIClient.
  if (import.meta.env.DEV) {
    return "";
  }

  return window.location.origin;
}

/**
 * EventSource adapter. Connection bookkeeping lives in sseConnection.ts, the
 * event-to-query-key table in sseInvalidations.ts, and key coalescing in
 * sseInvalidationQueue.ts; this hook only wires the browser to them.
 */
export function useSSE() {
  const queryClient = useQueryClient();
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const closedRef = useRef(false);
  const connRef = useRef(initialConnState);

  useEffect(() => {
    closedRef.current = false;

    // Hold the pool refresh during a draw spin: the post-draw pool lacks the
    // winner and would spoil the reveal. drawStore releases the pool when the
    // reel lands. Checked at flush time, since a spin can start mid-window.
    const [poolNs, poolSub] = MoviesKeys.listpool();
    const held = (key: readonly unknown[]) =>
      key[0] === poolNs && key[1] === poolSub && drawStore.getState().phase !== "idle";

    const queue = createInvalidationQueue((keys) => {
      for (const key of keys) {
        if (held(key)) continue;
        void queryClient.invalidateQueries({ queryKey: key });
      }
    }, timeoutScheduler);

    // SSE has no replay, so after a reconnect or gap re-pull every cache an
    // event can touch.
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

      if (eventSourceRef.current) {
        eventSourceRef.current.close();
      }

      const eventSource = new EventSource(`${baseURL()}/api/v1/events`);
      eventSourceRef.current = eventSource;

      eventSource.addEventListener("connected", (event) => {
        // Reset backoff here, not on `open`: an accept-then-drop proxy fires
        // `open` without `connected` and would pin the ladder at its floor.
        clearReconnectTimer();
        let frame: SSEConnectedFrame | null = null;
        try {
          frame = JSON.parse((event as MessageEvent).data) as SSEConnectedFrame;
        } catch (error) {
          console.error("[SSE] Error parsing connected frame:", error);
        }
        dispatch({ kind: "connected", seq: frame?.seq, epoch: frame?.epoch });
      });

      eventSource.addEventListener("message", (event) => {
        try {
          const sseEvent: SSEEvent = JSON.parse(event.data);

          // Gap detection first; this event's own row still runs below.
          dispatch({ kind: "event", seq: sseEvent.seq });

          // Before the coalescing window, so controls never show a stale draw gate.
          applyImmediateLifecycleState(queryClient, sseEvent);

          if (sseEvent.type === "movie:drawn") {
            const drawnMovie = sseEvent.data as MovieDrawPayload | undefined;
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
        // Close so native auto-reconnect cannot race our backoff; this also
        // recovers CLOSED states the browser never retries.
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
      // A background tab can lose its socket silently. An open stream needs no
      // resync: heartbeat and seq gap checks already cover it.
      const es = eventSourceRef.current;
      if (!es || es.readyState !== EventSource.OPEN) {
        dispatch({ kind: "reset" });
        clearReconnectTimer();
        connect();
      }
    };

    document.addEventListener("visibilitychange", handleVisibility);
    connect();

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
