import { useLocation, useRouter } from "@tanstack/react-router";
import { useCallback, useRef, useState } from "react";

import type { MovieTile } from "@/types/Response";
import type { AnyRouter } from "@tanstack/react-router";

/**
 * The movie modal is a history entry, so browser Back closes it (#196).
 *
 * The entry rides in the router's *location state*, which never reaches the
 * URL: the selected film stays unshareable and the address bar is untouched,
 * including the Stats tab's filter params. A `?movie=` param would have got
 * Back for free and was rejected for exactly that reason, as was route
 * masking, which needs a real modal route in the tree.
 *
 * The entry is what makes all four dismiss gestures one path. Esc, the veil,
 * the X and Back all end in `back()`, so each gesture pops the entry its own
 * open pushed and the stack stays flat however many posters get opened. The
 * only way to leave one behind is to navigate away with the modal still up,
 * which costs nothing.
 *
 * What's stored is a token identifying the *entry*, not the film. An id would
 * be the obvious thing and is wrong: an abandoned entry (navigate away with
 * the modal open, then come back to it) keeps whatever id it had, so opening
 * that same film again and dismissing would land on an entry still claiming
 * the modal is open, leaving it stuck with every gesture spent.
 */
declare module "@tanstack/react-router" {
  interface HistoryState {
    movieModal?: string;
  }
}

/** Distinct per open, so no two entries are ever mistaken for each other. */
function entryToken(): string {
  return Math.random().toString(36).slice(2);
}

/**
 * Location state survives a reload of its entry, so a refresh with the modal
 * open would restore it. Strip the token before the first render instead: a
 * refresh is meant to land on a clean page.
 *
 * Called against the router at startup rather than from a hook, so it happens
 * once, ahead of any component that reads the state.
 */
export function clearMovieModalHistory(router: AnyRouter) {
  const { href, state } = router.history.location;
  if (state.movieModal === undefined) return;
  router.history.replace(href, { ...state, movieModal: undefined });
}

/**
 * Open/close for the movie modal, for the two tabs that show one.
 *
 * The history entry says whether the modal is open; `selected` is the film it
 * was opened on, kept in React so the surface survives its own exit motion
 * (the entry is gone the moment Back lands, but the modal has an animation to
 * finish). Callers derive a live movie from `selected` against whatever lists
 * they hold, then hand `isOpen` and `close` straight to the `Modal`, which
 * needs to know nothing about any of this.
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
      // Push the *current* href back at itself, so nothing about the URL
      // changes and the entry differs from its predecessor only by its state.
      const { href, state } = router.history.location;
      router.history.push(href, { ...state, movieModal: opened });
    },
    [router],
  );

  // Bind dismissal to the entry visible in this render. Async work started
  // from one record must not spend a newer record's entry after the first has
  // already closed.
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
    /** The film the modal was opened on, live through the exit motion. */
    selected,
    /** Whether the entry this open pushed is still the one we're on. */
    isOpen: ownedToken !== null,
    open,
    close,
    /** For the `Modal`'s onClose: the motion is done, drop the surface. */
    onClosed: useCallback(() => setSelected(null), []),
  };
}
