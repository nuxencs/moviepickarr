import { MoviesKeys, SettingsKeys } from "@/api/query_keys";

import type { Movie, Settings } from "@/types/Response";
import type { SSEEvent, SSEEventType } from "@/types/SSEEvent";
import type { QueryClient } from "@tanstack/react-query";

/** Patch an already-known round without inventing the independent lock flag. */
export function setCachedDrawInProgress(
  queryClient: QueryClient,
  drawInProgress: boolean,
): void {
  queryClient.setQueryData<Settings>(SettingsKeys.poolLock(), (state) =>
    state ? { ...state, drawInProgress } : state,
  );
}

/** Draw lifecycle frames whose meaning for the server-owned hold is exact. */
export function drawInProgressForEvent(
  type: SSEEventType,
): boolean | undefined {
  if (type === "movie:drawn") return true;
  if (type === "movie:revealed" || type === "movie:watched") return false;
  return undefined;
}

/**
 * Apply lifecycle facts that an event establishes exactly, before the
 * coalesced refetch begins. This closes the queue window without fabricating
 * unrelated cache fields.
 */
export function applyImmediateLifecycleState(
  queryClient: QueryClient,
  event: SSEEvent,
): void {
  const drawInProgress = drawInProgressForEvent(event.type);
  if (drawInProgress !== undefined) {
    setCachedDrawInProgress(queryClient, drawInProgress);
  }

  if (event.type === "movie:revealed") {
    const movieID = (event.data as { movieID?: unknown } | undefined)?.movieID;
    if (typeof movieID === "number") {
      queryClient.setQueryData<Movie>(MoviesKeys.detail(movieID), (movie) =>
        movie ? { ...movie, status: "current" } : movie,
      );
    }
    return;
  }

  if (event.type === "movie:watched") {
    const watched = event.data as Movie | undefined;
    if (typeof watched?.movieID === "number") {
      queryClient.setQueryData<Movie>(MoviesKeys.detail(watched.movieID), (movie) =>
        movie ? { ...movie, ...watched } : movie,
      );
    }
    return;
  }

  if (event.type === "settings:pool-lock-changed") {
    const state = event.data as Settings | undefined;
    if (
      typeof state?.poolLocked === "boolean" &&
      typeof state.drawInProgress === "boolean"
    ) {
      // The handler reads the draw gate before entering the broker. A draw or
      // reveal can broadcast between that read and this event, so a cached
      // lifecycle fact owns this field while the lock event owns the lock.
      queryClient.setQueryData<Settings>(SettingsKeys.poolLock(), (current) =>
        current ? { ...current, poolLocked: state.poolLocked } : state,
      );
    }
  }
}
