import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";

import { MoviesKeys, SettingsKeys, StatsKeys, UsersKeys } from "@/api/query_keys";

import type { SSEEvent } from "@/types/SSEEvent";

function baseURL(): string {
  // Empty in dev so the EventSource connects same-origin via the Vite proxy
  // (see vite.config.ts), matching APIClient.
  if (import.meta.env.DEV) {
    return "";
  }

  return window.location.origin;
}

export function useSSE() {
  const queryClient = useQueryClient();
  const eventSourceRef = useRef<EventSource | null>(null);

  useEffect(() => {
    const eventSource = new EventSource(`${baseURL()}/api/v1/events`);
    eventSourceRef.current = eventSource;

    eventSource.addEventListener("connected", () => {
      console.log("[SSE] Connected to event stream");
    });

    eventSource.addEventListener("message", (event) => {
      try {
        const sseEvent: SSEEvent = JSON.parse(event.data);
        // console.log("[SSE] Received event:", sseEvent.type);

        // Map events to query invalidations
        switch (sseEvent.type) {
          case "user:created":
          case "user:deleted":
            void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
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
            void queryClient.invalidateQueries({ queryKey: StatsKeys.all });
            break;

          case "movie:picked":
            void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
            void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
            void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
            void queryClient.invalidateQueries({ queryKey: SettingsKeys.nextPicker() });
            break;

          case "movie:watched":
            void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
            void queryClient.invalidateQueries({ queryKey: MoviesKeys.listwatched() });
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

    // Handle errors
    eventSource.addEventListener("error", (error) => {
      console.error("[SSE] Connection error:", error);
    });

    // Cleanup on unmount
    return () => {
      console.log("[SSE] Closing connection");
      eventSource.close();
      eventSourceRef.current = null;
    };
  }, [queryClient]);
}
