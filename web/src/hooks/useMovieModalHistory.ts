import { useLocation, useRouter } from "@tanstack/react-router";
import { useCallback, useRef, useState } from "react";

import type { MovieTile } from "@/types/Response";
import type { AnyRouter } from "@tanstack/react-router";

/**
 * The movie modal is a history entry in location state, so Back closes it and
 * the URL stays untouched (#196). Not a `?movie=` param: that would make the
 * movie shareable and rewrite the Stats filter URL. Every dismiss gesture ends
 * in `back()`. The entry holds a per-open token, not the movie id: an abandoned
 * entry keeps its id and would leave a reopened modal stuck open.
 */
declare module "@tanstack/react-router" {
  interface HistoryState {
    movieModal?: string;
  }
}

function entryToken(): string {
  return Math.random().toString(36).slice(2);
}

/** Strips the token at startup, since location state survives a reload. */
export function clearMovieModalHistory(router: AnyRouter) {
  const { href, state } = router.history.location;
  if (state.movieModal === undefined) return;
  router.history.replace(href, { ...state, movieModal: undefined });
}

/**
 * Open/close for the movie modal. `selected` lives in React, not the entry, so
 * the modal survives its exit motion after Back lands.
 */
export function useMovieModal() {
  const router = useRouter();
  const token = useLocation({ select: (location) => location.state.movieModal });
  const [selected, setSelected] = useState<MovieTile | null>(null);
  const openedRef = useRef<string | null>(null);

  const open = useCallback(
    (movie: MovieTile) => {
      const opened = entryToken();
      openedRef.current = opened;
      setSelected(movie);
      // Same href, so the entry differs from its predecessor only by state.
      const { href, state } = router.history.location;
      router.history.push(href, { ...state, movieModal: opened });
    },
    [router],
  );

  // Bound to this render's entry, so stale async work cannot pop a newer entry.
  const ownedToken =
    selected !== null && token !== undefined && token === openedRef.current
      ? token
      : null;
  const close = useCallback(() => {
    if (
      ownedToken === null ||
      router.history.location.state.movieModal !== ownedToken
    ) {
      return;
    }
    router.history.back();
  }, [ownedToken, router]);

  return {
    /** The movie the modal was opened on, live through the exit motion. */
    selected,
    isOpen: ownedToken !== null,
    open,
    close,
    /** For the `Modal`'s onClose: the motion is done, drop the surface. */
    onClosed: useCallback(() => setSelected(null), []),
  };
}
