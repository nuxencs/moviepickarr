import { MoviesKeys, SettingsKeys, UsersKeys } from "@/api/query_keys";

import { SSEEvent } from "@/types/SSEEvent";

import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";

function baseURL(): string {
  if (import.meta.env.DEV) {
    return "http://localhost:3030";
  }

  return window.location.origin;
}

export function useSSE() {
  const queryClient = useQueryClient();
  const eventSourceRef = useRef<EventSource | null>(null);

  useEffect(() => {
    const eventSource = new EventSource(`${baseURL()}/api/events`);
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
            break;

          case "movie:added":
          case "movie:deleted":
          case "movie:moved":
            void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
            void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
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
