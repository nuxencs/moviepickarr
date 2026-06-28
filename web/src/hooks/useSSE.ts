import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";

import { MoviesKeys, PickKeys, SettingsKeys, StatsKeys, UsersKeys } from "@/api/query_keys";

import { buildLiveSpin, setActiveSpin, signalRevealed } from "@/components/moviepickarr/pickSpin";

import type { Movie } from "@/types/Response";
import type { SSEConnectedFrame, SSEEvent, SSEHeartbeatFrame } from "@/types/SSEEvent";

function baseURL(): string {
  // Empty in dev so the EventSource connects same-origin via the Vite proxy
  // (see vite.config.ts), matching APIClient.
  if (import.meta.env.DEV) {
    return "";
  }

  return window.location.origin;
}

// Reconnect backoff bounds. We own reconnection rather than leaning on the
// native EventSource auto-reconnect, which has no backoff/jitter and gives up
// permanently on a terminal CLOSED state (e.g. a 502 from nginx during a deploy).
const RECONNECT_BASE_MS = 1_000;
const RECONNECT_MAX_MS = 30_000;
const RECONNECT_JITTER = 0.3;

export function useSSE() {
  const queryClient = useQueryClient();
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectAttemptsRef = useRef(0);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const firstConnectRef = useRef(true);
  const closedRef = useRef(false);
  // Gap-detection cursor: the last seq we processed, and the broker epoch we're
  // connected to. A jump in seq (live or revealed by a heartbeat) or an epoch
  // change (server restart) triggers one resync. Persist across reconnects within
  // a mount; reset on remount (which re-fetches anyway).
  const lastSeqRef = useRef<number | null>(null);
  const epochRef = useRef<string | null>(null);

  useEffect(() => {
    closedRef.current = false;

    // Re-pull every cache an SSE event can touch. Used to reconcile state after
    // a reconnect or tab refocus: SSE has no replay, so any event that fired
    // while the socket was down is lost — re-fetching current state recovers it.
    // Safe because the whole app is invalidate-refetch: resync never needs to
    // know *what* it missed, only to re-read the live truth.
    const resync = () => {
      void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
      // Skip the pool refresh while a pick-reveal spin is in flight: the post-pick
      // pool no longer holds the winner, so refetching it mid-spin would drop the
      // winner tile from the grid and spoil the reveal. The Hero refreshes the pool
      // when the reel lands. Mirrors the `if (!spin)` guard in the movie:picked case.
      if (!queryClient.getQueryData(PickKeys.active())) {
        void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
      }
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.listwatched() });
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.details() });
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.filterOptions() });
      void queryClient.invalidateQueries({ queryKey: StatsKeys.all });
      void queryClient.invalidateQueries({ queryKey: SettingsKeys.poolLock() });
      void queryClient.invalidateQueries({ queryKey: SettingsKeys.nextPicker() });
    };

    const clearReconnectTimer = () => {
      if (reconnectTimerRef.current !== null) {
        clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
    };

    const scheduleReconnect = () => {
      if (closedRef.current || reconnectTimerRef.current !== null) {
        return;
      }
      const attempt = reconnectAttemptsRef.current++;
      const backoff = Math.min(RECONNECT_MAX_MS, RECONNECT_BASE_MS * 2 ** attempt);
      const delay = backoff + Math.random() * RECONNECT_JITTER * backoff;
      reconnectTimerRef.current = setTimeout(() => {
        reconnectTimerRef.current = null;
        connect();
      }, delay);
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
        // Reset the backoff ladder on the app-level `connected` frame, not on
        // bare transport `open`: an accept-then-drop intermediary (e.g. nginx/LB
        // that 200s then closes a dead upstream during a deploy) fires `open`
        // without ever delivering `connected`, and resetting there would pin the
        // ladder at the ~1s floor forever instead of climbing toward the cap.
        reconnectAttemptsRef.current = 0;
        clearReconnectTimer();

        // The handshake aligns our gap-detection cursor and flags a server restart.
        let frame: SSEConnectedFrame | null = null;
        try {
          frame = JSON.parse((event as MessageEvent).data) as SSEConnectedFrame;
        } catch (error) {
          console.error("[SSE] Error parsing connected frame:", error);
        }
        const restarted =
          !!frame?.epoch && epochRef.current !== null && epochRef.current !== frame.epoch;
        if (frame?.epoch) epochRef.current = frame.epoch;
        console.log(`[SSE] Connected to event stream${restarted ? " (server restarted)" : ""}`);

        // Resync on every reconnect — but not the very first connect, since the
        // initial mount queries already fetch fresh state. A reconnect may have
        // missed events while the socket was down (a restart included).
        if (!firstConnectRef.current) {
          resync();
        }
        // Align the cursor to the head AFTER resync: every event the broker
        // assigns from here is headSeq+1, so the first live frame isn't read as a
        // gap. On a restart the broker's seq resets, so this also clears a stale
        // (higher) lastSeq that would otherwise spuriously trip the detector.
        if (typeof frame?.seq === "number") lastSeqRef.current = frame.seq;
        firstConnectRef.current = false;
      });

      eventSource.addEventListener("message", (event) => {
        try {
          const sseEvent: SSEEvent = JSON.parse(event.data);
          // console.log("[SSE] Received event:", sseEvent.type);

          // Gap detection: a jump in seq means a frame was dropped (a full client
          // buffer). Resync once to heal — then still run this event's own handler
          // below, so the frame that *revealed* the gap isn't itself lost.
          if (typeof sseEvent.seq === "number") {
            if (lastSeqRef.current !== null && sseEvent.seq !== lastSeqRef.current + 1) {
              console.warn(
                `[SSE] seq gap: expected ${lastSeqRef.current + 1}, got ${sseEvent.seq}; resyncing`,
              );
              resync();
            }
            lastSeqRef.current = sseEvent.seq;
          }

          // Map events to query invalidations
          switch (sseEvent.type) {
            case "user:created":
            case "user:deleted":
              void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
              // Pickers are part of the Stats filter options.
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.filterOptions() });
              void queryClient.invalidateQueries({ queryKey: StatsKeys.all });
              break;

            case "movie:added":
            case "movie:deleted":
            case "movie:moved":
              void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
              break;

            case "movie:updated":
              void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.listwatched() });
              // Refresh any open detail modal for the edited movie.
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.details() });
              void queryClient.invalidateQueries({ queryKey: StatsKeys.all });
              break;

            case "movies:enriched-batch":
              // The enrichment worker coalesces a burst of TMDB enrichments into a
              // single batch event (instead of one per movie). Refresh every cache
              // that embeds enriched fields (poster, runtime, rating, credits, …) —
              // UsersKeys.list included, since the Members boards render posters.
              // Stats aggregate TMDB metadata too (genres, people, runtime, rating),
              // so they're invalidated alongside the movie lists. Same targets as
              // before; now fired once per burst rather than once per movie.
              void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.listwatched() });
              // Enrichment changes credits/genres/runtime/rating — the inputs to
              // both the detail modal and the Stats filter options.
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.details() });
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.filterOptions() });
              void queryClient.invalidateQueries({ queryKey: StatsKeys.all });
              break;

            case "movie:picked": {
              // Start the cross-client reveal spin from the event's self-contained
              // candidates. The pool may be invalidated freely below: the reel no
              // longer reads it (only the grid-continuity defer remains — see the
              // `if (!spin)` guard).
              const picked = sseEvent.data as Movie | undefined;
              // The reel candidates ride in the event itself now, so the spin no
              // longer reads the local pool cache (a client without it still spins).
              const spin = picked ? buildLiveSpin(picked) : null;
              if (picked) setActiveSpin(queryClient, spin);
              void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
              void queryClient.invalidateQueries({ queryKey: SettingsKeys.nextPicker() });
              // When a reel will play, hold the pool refresh until it lands (the Hero
              // does it on land) so the pool grid doesn't drop the winner mid-spin
              // and spoil it. No reel → refresh now.
              if (!spin) void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
              break;
            }

            case "movie:revealed": {
              // The picker confirmed (or the reel's countdown filled). Signal the
              // matching pickedAt so every client's Hero closes its reel together.
              const data = sseEvent.data as { pickedAt?: string } | undefined;
              if (data?.pickedAt) signalRevealed(queryClient, data.pickedAt);
              break;
            }

            case "movie:watched":
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.listwatched() });
              // A newly-watched movie enters the watched-derived filter options.
              void queryClient.invalidateQueries({ queryKey: MoviesKeys.filterOptions() });
              void queryClient.invalidateQueries({ queryKey: StatsKeys.all });
              break;

            case "settings:pool-lock-changed":
              void queryClient.invalidateQueries({ queryKey: SettingsKeys.poolLock() });
              break;

            case "settings:next-picker-changed":
              void queryClient.invalidateQueries({ queryKey: SettingsKeys.nextPicker() });
              break;

            default:
              console.warn("[SSE] Unknown event type:", sseEvent.type);
          }
        } catch (error) {
          console.error("[SSE] Error parsing event:", error);
        }
      });

      eventSource.addEventListener("heartbeat", (event) => {
        // Passive idle gap check: if the broker's head moved past our cursor while
        // we sat idle (a frame was dropped), resync and realign. This is what lets
        // a healthy tab refocus skip its blanket resync (see handleVisibility).
        try {
          const frame = JSON.parse((event as MessageEvent).data) as SSEHeartbeatFrame;
          if (
            typeof frame.seq === "number" &&
            lastSeqRef.current !== null &&
            frame.seq > lastSeqRef.current
          ) {
            console.warn(`[SSE] heartbeat gap: head ${frame.seq} > cursor ${lastSeqRef.current}; resyncing`);
            resync();
            lastSeqRef.current = frame.seq;
          }
        } catch (error) {
          console.error("[SSE] Error parsing heartbeat frame:", error);
        }
      });

      eventSource.addEventListener("error", () => {
        // The native error event carries no useful detail. Own the reconnection:
        // close immediately (so native auto-reconnect can't race our backoff) and
        // schedule a jittered retry. This also recovers from terminal CLOSED
        // states the browser would otherwise never retry.
        console.error("[SSE] Connection error; scheduling reconnect");
        eventSource.close();
        if (eventSourceRef.current === eventSource) {
          eventSourceRef.current = null;
        }
        scheduleReconnect();
      });
    };

    const handleVisibility = () => {
      if (document.visibilityState !== "visible") {
        return;
      }
      // On refocus, a backgrounded tab may have had its connection silently
      // killed (frozen tab / a dead socket the browser hasn't noticed yet). If the
      // stream isn't open, reconnect now and reset the backoff. If it IS open, do
      // nothing: the heartbeat proved liveness and the seq cursor (live + heartbeat
      // gap checks) already caught anything missed — a blanket resync here would
      // just over-fetch on every healthy refocus.
      const es = eventSourceRef.current;
      if (!es || es.readyState !== EventSource.OPEN) {
        reconnectAttemptsRef.current = 0;
        clearReconnectTimer();
        connect();
      }
    };

    document.addEventListener("visibilitychange", handleVisibility);
    connect();

    // Cleanup on unmount
    return () => {
      closedRef.current = true;
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
