import { type QueryClient, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useRouter, useSearch } from "@tanstack/react-router";
import {
  ArrowLeftIcon,
  ChevronRightIcon,
  MoveDownIcon,
  MoveUpIcon,
  PlusIcon,
  SearchIcon,
} from "lucide-react";
import {
  memo,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";

import { APIClient } from "@/api/APIClient";
import { MeQueryOptions, SettingsGetPoolStateQueryOptions, UsersGetAllQueryOptions } from "@/api/queries";

import { Avatar } from "@/components/moviepickarr/Bits";
import { hueOf, plural } from "@/components/moviepickarr/lib";
import { orderMembers, selectedMember } from "@/components/moviepickarr/membersSearch";
import { MembersSkeleton } from "@/components/moviepickarr/MembersSkeleton";
import { MovieModal } from "@/components/moviepickarr/MovieModal";
import { isSelf } from "@/components/moviepickarr/ownership";
import { membersStatus, POOL_SIZE, type RosterOccupancy } from "@/components/moviepickarr/poolLock";
import { possessive } from "@/components/moviepickarr/possessive";
import { Poster } from "@/components/moviepickarr/Poster";
import { actionLabel, type ActionKind, refusalOf, type Refusal } from "@/components/moviepickarr/refusals";
import { SearchModal } from "@/components/moviepickarr/SearchModal";
import { Skeleton } from "@/components/moviepickarr/Skeletons";
import { columnCount, filterStash, landingCell, missLine, nextCell } from "@/components/moviepickarr/stashWall";
import { toast } from "@/components/ui/toast-api";

import type { MovieTile, MoveTarget, User } from "@/types/Response";
import type { RefObject } from "react";

import { useMovieModal } from "@/hooks/useMovieModalHistory";

import "@/components/moviepickarr/members.css";

/**
 * The members.css push query (#236), mirrored for the focus handoff and the
 * leaving screen's `inert`. CSS alone picks the drawn screen. Keep in step.
 */
const PUSH_WIDTH = "not all and (min-width: 761px)";

const POOL_POSTER_SIZES =
  "auto, (max-width: 700px) calc((100vw - 92px) / 3), " +
  "(min-width: 761px) and (max-width: 900px) 112px, " +
  "(min-width: 761px) 128px, calc((100vw - 120px) / 3)";
const STASH_POSTER_SIZES =
  "auto, (max-width: 700px) calc((100vw - 66px) / 4), " +
  "(min-width: 761px) and (max-width: 899px) 120px, " +
  "(min-width: 761px) and (max-width: 1199px) 112px, " +
  "(min-width: 761px) 128px, calc((100vw - 94px) / 4)";

const isPushWidth = () => window.matchMedia(PUSH_WIDTH).matches;

const watchPushWidth = (onChange: () => void) => {
  const query = window.matchMedia(PUSH_WIDTH);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
};

/**
 * Whether the layout is two screens rather than two columns. Subscribed, not
 * read once, because it sets `inert` on the screen you are not on while its
 * exit animation still keeps a box (#266).
 */
function usePushWidth(): boolean {
  return useSyncExternalStore(watchPushWidth, isPushWidth);
}

type MoveHandlers = {
  onStarted: (attempt: number) => void;
  onError: (attempt: number) => void;
};

type RequestMove = (
  movieID: number,
  target: MoveTarget,
  handlers: MoveHandlers,
) => void;

type MoveRegistry = {
  nextAttempt: number;
  pending: Map<string, { attempt: number; onError: MoveHandlers["onError"] }>;
};

// Keyed by QueryClient, which outlives keyed panes and route remounts, so a
// remount cannot send a second copy of a pending move.
const moveRegistries = new WeakMap<QueryClient, MoveRegistry>();

function moveRegistry(client: QueryClient): MoveRegistry {
  const existing = moveRegistries.get(client);
  if (existing) return existing;
  const created = { nextAttempt: 0, pending: new Map() };
  moveRegistries.set(client, created);
  return created;
}

/**
 * The Members page: a rail of members beside one board pane. The selected
 * member is the URL (`/users?member=<id>`, see membersSearch), so boards link
 * and Back works. Below 761px `stash=true` pushes the pane over the rail as a
 * second screen (#236).
 *
 * Every poster opens the movie modal on every board. A board you cannot act on
 * lacks only the corner actions; temporary refusals (full pool, lock, draw)
 * keep the control, inert, with the reason (refusals.ts). The wall is a
 * roving-tabindex list and the rail is plain tab stops (#235). Focus moves only
 * when its element goes away, to the nearest one still there.
 */
export function UsersTab() {
  const { data: users, isPending: usersPending, isError: usersError } = useQuery(UsersGetAllQueryOptions());
  // Drives isSelf gating (ownership.ts). Adding and removing members belongs
  // to the admin roster, not this page.
  const { data: me } = useQuery(MeQueryOptions());
  const [searchUser, setSearchUser] = useState<User | null>(null);

  // Board-level move registry: repeating a pending move rebinds its existing
  // attempt instead of sending again, and with no mutation observer, request
  // state does not re-render the page.
  const queryClient = useQueryClient();
  const moves = moveRegistry(queryClient);
  const requestMove = useCallback<RequestMove>(
    (movieID, target, handlers) => {
      const key = `${target}:${movieID}`;
      const pending = moves.pending.get(key);
      if (pending) {
        moves.pending.set(key, { ...pending, onError: handlers.onError });
        handlers.onStarted(pending.attempt);
        return;
      }
      const attempt = ++moves.nextAttempt;
      moves.pending.set(key, { attempt, onError: handlers.onError });
      handlers.onStarted(attempt);
      // Built in the cache, not via an observer: keeps offline pause/resume and
      // global mutation callbacks without request-state renders.
      const mutation = queryClient.getMutationCache().build(queryClient, {
        mutationFn: () => APIClient.board.moveMovie(movieID, target),
      });
      void mutation
        .execute(undefined)
        .catch(() => {
          const owner = moves.pending.get(key);
          if (owner?.attempt === attempt) owner.onError(attempt);
          toast.error("Failed to move movie");
        })
        .finally(() => {
          if (moves.pending.get(key)?.attempt === attempt) {
            moves.pending.delete(key);
          }
        });
    },
    [moves, queryClient],
  );

  // Opening a movie pushes a history entry, so Back closes it (#196). The
  // tile's lean object is enough: the modal lazy-loads the full record.
  const { selected: openMovie, isOpen, open, close, onClosed } = useMovieModal();

  // Status line wording and composition live in poolLock.ts.
  const {
    data: poolState,
    isError: poolStateError,
    isFetching: poolStateFetching,
  } = useQuery(SettingsGetPoolStateQueryOptions());
  const occupancy = useMemo<RosterOccupancy>(() => {
    if (usersError) return { state: "error" };
    if (usersPending || !users) return { state: "pending" };
    return {
      state: "ready",
      // Not adjusted for a draw: the server keeps the winner pooled until the
      // reveal, and an early drop would give the movie away.
      filled: users.reduce((n, user) => n + Object.keys(user.currentPool).length, 0),
      slots: users.length * POOL_SIZE,
    };
  }, [users, usersPending, usersError]);
  const poolStateKnown =
    poolState !== undefined && !poolStateError && !poolStateFetching;
  const isLocked = !!poolState?.poolLocked;
  const drawInFlight = !!poolState?.drawInProgress;
  const status = poolStateError
    ? { text: "Round state failed to load", announce: "Round state failed to load" }
    : poolState === undefined
      ? { text: null, announce: "" }
      : membersStatus(occupancy, isLocked, drawInFlight);

  // The route id is `/_app/users` (pathless app layout), not the URL.
  const { member, stash } = useSearch({ from: "/_app/users" });
  // Below 761px the screen you are not on is inert; above it neither is.
  const twoScreens = usePushWidth();
  const railOffScreen = twoScreens && !!stash;
  const paneOffScreen = twoScreens && !stash;
  const ordered = useMemo(() => orderMembers(users, me?.id), [users, me?.id]);
  const selected = selectedMember(ordered, member, me?.id);

  // The rail has no scrollbar (its width is three posters), so a bottom fade
  // signals more members. A boolean, not a measured size.
  const railRef = useRef<HTMLElement>(null);
  const [railOverflows, setRailOverflows] = useState(false);

  // Switching member remounts the keyed pane and drops focus to the document.
  // The outgoing pane raises this and the incoming one focuses its heading.
  const paneLostFocus = useRef(false);

  // Held here because the push lands focus on it. The keyed pane mounts before
  // this parent's effect runs, so it is always the incoming heading.
  const paneHeadingRef = useRef<HTMLHeadingElement>(null);
  // Each drawer's stash link by member, so returning from the push refocuses
  // the link you left from. The rail is hidden, not unmounted, while pushed.
  const stashLinks = useRef(new Map<number, HTMLAnchorElement>());

  // The address as of last render, to tell a navigation from a cold arrival,
  // which must not move focus. One ref, so all three come from one render.
  const last = useRef({ pushed: !!stash, member, selectedID: selected?.userID });
  useEffect(() => {
    const was = last.current;
    last.current = { pushed: !!stash, member, selectedID: selected?.userID };
    if ((was.pushed === !!stash && was.member === member) || !isPushWidth()) return;
    // Onto a board: focus the heading, where a screen reader meets the self-mark.
    if (stash) {
      paneHeadingRef.current?.focus();
      return;
    }
    // Back to the rail from the pushed screen: refocus the stash link you left
    // from, but only if that board is still open. A resize mid-stack can pop to
    // another member's rail, and a shut drawer's link is inert.
    if (!was.pushed || was.selectedID === undefined || was.selectedID !== selected?.userID) return;
    stashLinks.current.get(was.selectedID)?.focus();
  }, [stash, member, selected?.userID]);
  useEffect(() => {
    const rail = railRef.current;
    if (!rail) return;
    const check = () => setRailOverflows(rail.scrollHeight > rail.clientHeight + 1);
    check();
    // The rail has no constrained height on the first pass; a frame later it does.
    const frame = requestAnimationFrame(check);
    const ro = new ResizeObserver(check);
    ro.observe(rail);
    // Opening a drawer changes the rail's content, not its box, so the observer
    // misses it. Only the drawer's size transition needs another read.
    const onDrawerTransitionEnd = (event: TransitionEvent) => {
      if (
        event.propertyName === "grid-template-rows" &&
        event.target instanceof HTMLElement &&
        event.target.classList.contains("mem-drop")
      ) {
        check();
      }
    };
    rail.addEventListener("transitionend", onDrawerTransitionEnd);
    return () => {
      cancelAnimationFrame(frame);
      ro.disconnect();
      rail.removeEventListener("transitionend", onDrawerTransitionEnd);
    };
  }, [ordered.length, selected?.userID]);

  const membersHead = (
    <div className="sec-head">
      <div className="sec-title">
        <h2>Members</h2>
        {/* No count until the roster loads: "0 people" would be wrong. */}
        {users && <span className="sec-count">{plural(users.length, "person", "people")}</span>}
        {status.text === null ? (
          <Skeleton w={132} h={12} />
        ) : (
          <span className="sec-status mono">{status.text}</span>
        )}
      </div>
    </div>
  );

  return (
    <>
      {/* data-pushed is the URL: below 761px the head goes with the rail. */}
      <div className="mg-rise mem" data-pushed={!!stash}>
        {/* Round and draw clauses only (see membersStatus). Outside the head
            because the pushed screen hides the head, and a display: none live
            region announces nothing. */}
        <span className="vis-hidden" role="status">
          {status.announce}
        </span>

        {usersError ? (
          <>
            {membersHead}
            <p className="empty text-destructive">Failed to load members.</p>
          </>
        ) : usersPending ? (
          <>
            {membersHead}
            {/* The page's own shape (#239); data-pushed picks its screen. */}
            <MembersSkeleton />
          </>
        ) : selected ? (
          <div className="mem__shell mem__shell--with-head">
            <div className="mem-rail-screen" inert={railOffScreen}>
              {membersHead}
              {/* Links with aria-current, like the primary nav: not a tab list
                  or a disclosure, so no aria-expanded. Plain tab stops. */}
              <nav
                className="mem-rail"
                aria-label="Members"
                ref={railRef}
                data-overflow={railOverflows}
                data-page-scroll-owner
              >
                {ordered.map((user) => (
                  <RailRow
                    key={user.userID}
                    user={user}
                    active={user.userID === selected.userID}
                    isOwnBoard={isSelf(me?.id, user.userID)}
                    isLocked={!!isLocked}
                    drawInFlight={drawInFlight}
                    poolStateKnown={poolStateKnown}
                    onOpen={open}
                    stashLinks={stashLinks}
                    requestMove={requestMove}
                  />
                ))}
              </nav>
            </div>

            {/* Keyed on the member, so a switch resets scroll and filter. */}
            <StashPane
              key={selected.userID}
              user={selected}
              isOwnBoard={isSelf(me?.id, selected.userID)}
              isLocked={!!isLocked}
              drawInFlight={drawInFlight}
              poolStateKnown={poolStateKnown}
              guest={me?.role === "guest"}
              onOpenSearch={() => setSearchUser(selected)}
              onOpen={open}
              lostFocus={paneLostFocus}
              headingRef={paneHeadingRef}
              offScreen={paneOffScreen}
              requestMove={requestMove}
            />
          </div>
        ) : (
          <>
            {membersHead}
            {/* Defensive: a live session implies your own non-archived row. */}
            <p className="empty">No members yet</p>
          </>
        )}
      </div>

      {searchUser && (
        <SearchModal userName={searchUser.name} onClose={() => setSearchUser(null)} />
      )}

      {openMovie && (
        <MovieModal movie={openMovie} open={isOpen} onRequestClose={close} onClose={onClosed} />
      )}
    </>
  );
}

/**
 * One rail member: the selecting link and the drawer with their pool. The
 * row's accessible name comes from its contents, never an aria-label, so the
 * shown and spoken text cannot drift. Hence the aria-hidden initials (Bits)
 * and the role="img" pips.
 */
function RailRow({
  user,
  active,
  isOwnBoard,
  isLocked,
  drawInFlight,
  poolStateKnown,
  onOpen,
  stashLinks,
  requestMove,
}: {
  user: User;
  active: boolean;
  isOwnBoard: boolean;
  isLocked: boolean;
  drawInFlight: boolean;
  poolStateKnown: boolean;
  onOpen: (movie: MovieTile) => void;
  /** Stash links by member, so returning from the push can refocus one (#236). */
  stashLinks: RefObject<Map<number, HTMLAnchorElement>>;
  requestMove: RequestMove;
}) {
  const pool = useMemo(
    () => Object.values(user.currentPool).sort((a, b) => a.title.localeCompare(b.title)),
    [user.currentPool],
  );
  const stashCount = Object.keys(user.stash).length;

  // A shut drawer's art is not fetched until first opened: Chrome still loads a
  // zero-height `loading="lazy"` image in the viewport. Once opened it stays, so
  // art does not pop out of a closing drawer.
  const [everOpened, setEverOpened] = useState(active);
  useEffect(() => {
    if (active) setEverOpened(true);
  }, [active]);

  // Focus target when the last movie leaves this pool (#235).
  const linkRef = useRef<HTMLAnchorElement>(null);

  const holdStashLink = useCallback(
    (el: HTMLAnchorElement | null) => {
      if (el) stashLinks.current.set(user.userID, el);
      return () => {
        stashLinks.current.delete(user.userID);
      };
    },
    [stashLinks, user.userID],
  );

  return (
    <div className="mem-row" data-active={active}>
      {/* Always an explicit id, even yours, so a copied URL shows the recipient your board. */}
      <Link
        to="/users"
        search={{ member: user.userID }}
        className="mem-row__link"
        aria-current={active ? "page" : undefined}
        ref={linkRef}
      >
        <Avatar name={user.name} size={30} />
        <span className="mem-row__text">
          {/* No self-mark in the rail. Full name: a roster must tell apart shared first names. */}
          <span className="mem-row__nm" title={user.name}>
            {user.name}
          </span>
          <span className="mem-row__ct mono">{stashCount} in stash</span>
        </span>
        {/* Hidden when open: the pool itself says it. */}
        {!active && <PoolPips filled={pool.length} />}
      </Link>

      {/* Every drawer stays mounted so the 0fr/1fr transitions sum to a
          constant (members.css). `inert`, not aria-hidden (buttons would stay
          tabbable) or visibility: hidden (it shows mid-transition). */}
      <div className="mem-drop" data-open={active}>
        <div className="mem-drop__inner" inert={!active}>
          <div className="mem-drop__body">
            <PoolSlots
              pool={pool}
              showArt={everOpened}
              isOwnBoard={isOwnBoard}
              isLocked={isLocked}
              drawInFlight={drawInFlight}
              poolStateKnown={poolStateKnown}
              onOpen={onOpen}
              rowLinkRef={linkRef}
              requestMove={requestMove}
            />

            {/* Below 761px only (members.css). A link, not a button: the
                pushed screen is part of the address. In every drawer for a
                constant rail height; shut drawers are inert, so only one is
                exposed and its name need not repeat the member. */}
            <Link
              to="/users"
              search={{ member: user.userID, stash: true }}
              className="mem-tostash"
              ref={holdStashLink}
            >
              Stash
              <span className="mem-tostash__ct mono">{stashCount}</span>
              <ChevronRightIcon />
            </Link>
          </div>
        </div>
      </div>
    </div>
  );
}

/** role="img" so the label survives: a bare span's label is dropped. */
function PoolPips({ filled }: { filled: number }) {
  return (
    <span className="mem-pips" role="img" aria-label={`${filled} of ${POOL_SIZE} slots filled`}>
      {Array.from({ length: POOL_SIZE }).map((_, i) => (
        <span key={i} className="mem-pip" data-filled={i < filled} />
      ))}
    </span>
  );
}

/**
 * The poster as a button that opens the movie, on every board and never
 * gated: the lock and the draw freeze moves, not reads. A sibling of the
 * corner action, never its parent (no button inside a button).
 */
function PosterButton({
  movie,
  posterSizes,
  showArt = true,
  cell,
  tabIndex,
  onOpen,
}: {
  movie: MovieTile;
  posterSizes: string;
  /** Whether to fetch the art. False in a drawer nobody has opened (see RailRow). */
  showArt?: boolean;
  /** Index in its band, to refocus by number after a move (#235). */
  cell?: number;
  /** -1 on wall posters except the roving one; unset in the pool. */
  tabIndex?: number;
  onOpen: (movie: MovieTile) => void;
}) {
  return (
    <button
      type="button"
      className="mem-open"
      onClick={() => onOpen(movie)}
      aria-label={movie.title}
      title={movie.title}
      data-cell={cell}
      tabIndex={tabIndex}
    >
      {showArt && (
        <Poster
          title={movie.title}
          hue={hueOf(movie.title)}
          posterPath={movie.posterPath}
          showTitle={false}
          sizes={posterSizes}
        />
      )}
    </button>
  );
}

/**
 * Whether focus was dropped from the region rather than moved away. Body focus
 * alone is ambiguous (a deliberate blur lands there too); removal fires no
 * blur, so the region's ownership flag survives it.
 */
function focusWasDropped(regionOwnsFocus: boolean): boolean {
  return regionOwnsFocus && document.activeElement === document.body;
}

/** The wall cell owned by a poster or by its sibling corner action. */
function wallCellOf(target: HTMLElement): number | null {
  const marked = target.hasAttribute("data-cell")
    ? target
    : target.closest(".mem-tile")?.querySelector("[data-cell]");
  const index = Number(marked?.getAttribute("data-cell"));
  return Number.isInteger(index) ? index : null;
}

/**
 * The tile's one corner action: promote on the stash, demote in the pool, own
 * board only. A refusal uses `aria-disabled`, not `disabled`, so the control
 * stays focusable (the members.css focus reveal needs it) and its name and
 * tooltip carry the reason (refusals.ts).
 */
function TileAction({
  kind,
  refusal,
  tabIndex,
  onActivate,
}: {
  kind: ActionKind;
  refusal: Refusal | null;
  /** -1 on wall tiles except the roving one, so Tab goes poster, action, out.
   *  Unset in the pool. A refusal never changes it. */
  tabIndex?: number;
  onActivate: () => void;
}) {
  const label = actionLabel(kind, refusal);
  return (
    <button
      type="button"
      className="mem-act"
      tabIndex={tabIndex}
      // Absent, not `false`, when allowed.
      aria-disabled={refusal ? true : undefined}
      onClick={() => {
        if (refusal) return;
        onActivate();
      }}
      aria-label={label}
      title={label}
    >
      {/* No refusal glyph: on a full pool it marked every poster as barred. */}
      {kind === "promote" ? <MoveUpIcon /> : <MoveDownIcon />}
    </button>
  );
}

/**
 * The open row's pool: always POOL_SIZE slots, never reordered (the draw is
 * random). No heading or hint, so every drawer has the same height.
 */
function PoolSlots({
  pool,
  showArt,
  isOwnBoard,
  isLocked,
  drawInFlight,
  poolStateKnown,
  onOpen,
  rowLinkRef,
  requestMove,
}: {
  pool: MovieTile[];
  /** False until the drawer first opens: slots draw, art does not (see RailRow). */
  showArt: boolean;
  isOwnBoard: boolean;
  isLocked: boolean;
  drawInFlight: boolean;
  poolStateKnown: boolean;
  onOpen: (movie: MovieTile) => void;
  /** The member's own row, where focus goes when the last movie leaves the pool. */
  rowLinkRef: RefObject<HTMLAnchorElement | null>;
  requestMove: RequestMove;
}) {
  // Demote to the stash. A repeat of a pending move reuses its attempt (see
  // requestMove), so focus follows the latest activation.
  const landing = useRef<PoolMove | null>(null);
  const moveOwnsFocus = useRef(false);
  const bandRef = useRef<HTMLDivElement>(null);
  const demote = ({ movieID, slot }: Omit<PoolMove, "attempt">) => {
    // Only the source slot may own the landing: body focus alone is ambiguous.
    const sourceOwnsFocus =
      bandRef.current?.children.item(slot)?.contains(document.activeElement) ?? false;
    requestMove(movieID, "stash", {
      // Record the slot, not the element (that node is leaving). Set before the
      // request: the roster can arrive over SSE first.
      onStarted: (attempt) => {
        moveOwnsFocus.current = sourceOwnsFocus;
        landing.current = { movieID, slot, attempt };
      },
      onError: (attempt) => {
        if (landing.current?.attempt === attempt) {
          landing.current = null;
          moveOwnsFocus.current = false;
        }
      },
    });
  };

  // Landed when the roster no longer has the movie, not when the request
  // returns: focusing in between hits a tile about to unmount.
  useEffect(() => {
    const moved = landing.current;
    if (!moved) return;
    const currentSlot = pool.findIndex((movie) => movie.movieID === moved.movieID);
    if (currentSlot !== -1) {
      if (currentSlot !== moved.slot) {
        landing.current = { ...moved, slot: currentSlot };
      }
      return;
    }
    landing.current = null;
    const ownsFocus = moveOwnsFocus.current;
    moveOwnsFocus.current = false;
    if (!focusWasDropped(ownsFocus)) return;
    const to = landingCell(moved.slot, pool.length);
    // Empty slots are not focusable, so an emptied pool focuses the row.
    if (to === null) {
      rowLinkRef.current?.focus();
      return;
    }
    bandRef.current?.querySelector<HTMLElement>(`.mem-open[data-cell="${to}"]`)?.focus();
  }, [pool, rowLinkRef]);

  // Never true for a demote, which is the way out of a full pool; refusalOf decides.
  const poolFull = pool.length >= POOL_SIZE;

  return (
    <div
      className="mem-pool"
      ref={bandRef}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
          moveOwnsFocus.current = false;
        }
      }}
    >
      {Array.from({ length: POOL_SIZE }).map((_, i) => {
        const movie = pool[i];
        return movie ? (
          <div className="pslot pslot--filled" key={movie.movieID}>
            <PosterButton
              movie={movie}
              posterSizes={POOL_POSTER_SIZES}
              showArt={showArt}
              cell={i}
              onOpen={onOpen}
            />
            {isOwnBoard && (
              <TileAction
                kind="demote"
                refusal={refusalOf({
                  kind: "demote",
                  isLocked,
                  drawInFlight,
                  poolFull,
                  stateKnown: poolStateKnown,
                })}
                onActivate={() => demote({ movieID: movie.movieID, slot: i })}
              />
            )}
          </div>
        ) : (
          // Not clickable: movies reach the pool only by promotion from the stash.
          <div className="pslot pslot--empty" key={`empty-${i}`} aria-hidden="true" />
        );
      })}
    </div>
  );
}

/**
 * The selected member's stash as a wall of untitled posters, plus the add tile
 * on your own board. Fixed title order, no sort control: other keys arrive
 * with enrichment and would reorder the wall as SSE lands. A roving-tabindex
 * list, not `role="grid"` (#235): two tab stops on your board, one on a guest's.
 */
function StashPane({
  user,
  isOwnBoard,
  isLocked,
  drawInFlight,
  poolStateKnown,
  guest,
  onOpenSearch,
  onOpen,
  lostFocus,
  headingRef,
  offScreen,
  requestMove,
}: {
  user: User;
  isOwnBoard: boolean;
  isLocked: boolean;
  poolStateKnown: boolean;
  guest: boolean;
  /** Not pre-judged: refusalOf alone decides what a draw refuses. */
  drawInFlight: boolean;
  onOpenSearch: () => void;
  onOpen: (movie: MovieTile) => void;
  /** Set on unmount if focus was inside; read by the next pane on mount. */
  lostFocus: RefObject<boolean>;
  /** Focus target when a tile is taken from under focus, and the push target (#236). */
  headingRef: RefObject<HTMLHeadingElement | null>;
  /** The screen you are not on, below 761px. Keeps the leaving pane out of
   *  the tab order and accessibility tree while it slides away (#266). */
  offScreen: boolean;
  requestMove: RequestMove;
}) {
  const router = useRouter();
  const [filter, setFilter] = useState("");

  const stash = useMemo(
    () => Object.values(user.stash).sort((a, b) => a.title.localeCompare(b.title)),
    [user.stash],
  );
  const filteredStash = useMemo(() => filterStash(stash, filter), [stash, filter]);

  const pooled = Object.keys(user.currentPool).length;
  const poolFull = pooled >= POOL_SIZE;
  const firstName = user.name.split(" ")[0];
  // The rail shows full names; the heading reads like speech ("Ada's stash").
  // Shared first names give identical headings, a known limit.
  const who = isOwnBoard ? "Your" : possessive(firstName);
  const headingID = `mem-stash-${user.userID}`;

  // Bottom fade only when the wall overflows, like the rail's.
  const wallRef = useRef<HTMLDivElement>(null);
  const [wallOverflows, setWallOverflows] = useState(false);

  // The add tile is cell 0. Hidden under any filter: next to hits it reads as a result.
  const addTile = isOwnBoard && !filter.trim();
  // Cells, not movies: the add tile is a cell too.
  const cells = filteredStash.length + (addTile ? 1 : 0);

  useEffect(() => {
    const wall = wallRef.current;
    if (!wall) return;
    const check = () => setWallOverflows(wall.scrollHeight > wall.clientHeight + 1);
    check();
    const ro = new ResizeObserver(check);
    ro.observe(wall);
    return () => ro.disconnect();
  }, [cells]);

  // Clamped: an SSE roster update can remove the cell the index names.
  const [roving, setRoving] = useState(0);
  const cell = Math.min(roving, Math.max(cells - 1, 0));

  const gridRef = useRef<HTMLDivElement>(null);
  const cellAt = useCallback(
    (index: number) => gridRef.current?.querySelector<HTMLElement>(`[data-cell="${index}"]`),
    [],
  );
  const focusCell = useCallback(
    (index: number) => {
      setRoving(index);
      cellAt(index)?.focus();
    },
    [cellAt],
  );

  const syncedFilter = useRef(filter);

  // A new filter restarts the index at its first result (focus stays in the
  // field). Otherwise follow the focused node: React keeps a keyed node focused
  // when earlier movies change, with no focus event, so its cell shifts.
  useLayoutEffect(() => {
    if (syncedFilter.current !== filter) {
      syncedFilter.current = filter;
      setRoving(0);
      return;
    }
    const grid = gridRef.current;
    const active = document.activeElement;
    if (!grid || !(active instanceof HTMLElement) || !grid.contains(active)) return;
    const actual = wallCellOf(active);
    if (actual !== null) setRoving(actual);
  }, [filter, filteredStash, addTile]);

  // Focus anywhere in the wall takes the index, so mouse and keyboard agree.
  // A corner action counts as its tile's cell.
  const onWallFocus = (e: React.FocusEvent<HTMLDivElement>) => {
    const index = wallCellOf(e.target as HTMLElement);
    if (index !== null) setRoving(index);
  };

  const onWallKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    // Modifier chords belong to the browser.
    if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
    const columns = gridRef.current
      ? columnCount(getComputedStyle(gridRef.current).gridTemplateColumns)
      : 1;
    const to = nextCell(e.key, cell, cells, columns);
    if (to === null) return;
    // Only for a move that lands; a refused arrow scrolls as usual.
    e.preventDefault();
    focusCell(to);
  };

  // True while focus is in the pane, so a tile unmounting under it reads as a
  // loss. React's onFocus and onBlur bubble (focusin and focusout).
  const holdsFocus = useRef(false);
  // Separate from pane focus: only the source movie's cell may own its landing.
  const moveOwnsFocus = useRef(false);
  const landing = useRef<PromotionMove | null>(null);
  const onPromote = useCallback(
    (movieID: number, from: number) => {
      const active = document.activeElement;
      const sourceOwnsFocus =
        active instanceof HTMLElement &&
        gridRef.current?.contains(active) === true &&
        wallCellOf(active) === from;
      requestMove(movieID, "pool", {
        onStarted: (attempt) => {
          moveOwnsFocus.current = sourceOwnsFocus;
          landing.current = { movieID, cell: from, attempt };
        },
        onError: (attempt) => {
          if (landing.current?.attempt === attempt) {
            landing.current = null;
            moveOwnsFocus.current = false;
          }
        },
      });
    },
    [requestMove],
  );
  // A promote has landed once the movie leaves the stash (see PoolSlots).
  useEffect(() => {
    const moved = landing.current;
    if (!moved) return;
    const visibleIndex = filteredStash.findIndex(
      (movie) => movie.movieID === moved.movieID,
    );
    if (visibleIndex !== -1) {
      const currentCell = visibleIndex + (addTile ? 1 : 0);
      if (currentCell !== moved.cell) {
        landing.current = { ...moved, cell: currentCell };
      }
      return;
    }
    // A filter can hide the movie without moving it; wait for the roster.
    if (stash.some((movie) => movie.movieID === moved.movieID)) return;
    landing.current = null;
    const ownsFocus = moveOwnsFocus.current;
    moveOwnsFocus.current = false;
    if (!focusWasDropped(ownsFocus)) return;
    const to = landingCell(moved.cell, cells);
    // The poster, not its corner action: the third promote fills the pool, so
    // the action is now refused.
    if (to === null) {
      headingRef.current?.focus();
      return;
    }
    focusCell(to);
  }, [stash, filteredStash, addTile, cells, focusCell, headingRef]);

  // Removing the focused node fires no blur, so recover focus to the heading.
  // A click on a rail row is a departure and is left alone.
  useEffect(() => {
    if (!holdsFocus.current || document.activeElement !== document.body) return;
    holdsFocus.current = false;
    headingRef.current?.focus();
  });
  useEffect(() => {
    if (!lostFocus.current) return;
    lostFocus.current = false;
    headingRef.current?.focus();
    // The same rule across a member switch's remount; mount only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(
    () => () => {
      if (holdsFocus.current) lostFocus.current = true;
    },
    [lostFocus],
  );

  // Your own empty wall is the add tile alone. Others get one line, since a
  // blank pane looks like a failed switch. No name and no "yet".
  const emptyLine =
    filteredStash.length > 0 || addTile
      ? null
      : filter.trim()
        ? missLine(filter)
        : "This stash is empty";

  return (
    <section
      className="mem-pane"
      data-page-scroll-owner
      aria-labelledby={headingID}
      inert={offScreen}
      onFocus={() => {
        holdsFocus.current = true;
      }}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
          holdsFocus.current = false;
          moveOwnsFocus.current = false;
        }
      }}
    >
      {/* Below 761px only (members.css), but always rendered so a resize
          needs no render condition. Its pips are this screen's only occupancy
          signal. */}
      <div className="mem-backbar">
        {/* No can-go-back in the router, so a cold deep link exits the app. */}
        <button type="button" className="mem-back" onClick={() => router.history.back()}>
          <ArrowLeftIcon />
          All members
        </button>
        <PoolPips filled={pooled} />
      </div>

      <div className="mem-stash">
        <div className="mem-stash__head">
          <div className="mem-stash__id">
            {/* tabIndex={-1}: focus target when its tile is removed (#235) and
                where the push lands (#236), never a tab stop. */}
            <h3
              id={headingID}
              className="mem-stash__title"
              title={`${who} stash`}
              ref={headingRef}
              tabIndex={-1}
            >
              <span className="mem-stash__who">{who}</span> stash
            </h3>
            {/* Pushed screen only; members.css hides it at 761px and up. */}
            <span className="sec-count">{stash.length}</span>
          </div>
          <label className="field">
            <SearchIcon />
            <input
              name="stash-filter"
              aria-label={`Search ${possessive(firstName)} stash`}
              placeholder="Search stash…"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
          </label>
        </div>

        <div
          className="mem-wallbox"
          ref={wallRef}
          data-overflow={wallOverflows}
          data-page-scroll-owner
        >
          {/* Keys on the wall, not per cell, so arrows work from a corner action. */}
          <div
            className={`mem-wall${emptyLine ? " mem-wall--empty" : ""}`}
            ref={gridRef}
            onKeyDown={onWallKeyDown}
            onFocus={onWallFocus}
          >
            {addTile && (
              // Icon-only, so the name is authored. Inside the roving list, so
              // Tab from the field reaches it without a third tab stop.
              <button
                type="button"
                className="mem-addtile"
                onClick={onOpenSearch}
                aria-label={`Add to ${possessive(firstName)} stash`}
                title={`Add to ${possessive(firstName)} stash`}
                data-cell={0}
                tabIndex={cell === 0 ? 0 : -1}
              >
                <PlusIcon />
              </button>
            )}
            {emptyLine ? (
              // Not a tab stop: a wall with no matches has no cells.
              <p className="empty mem-wall__empty">{emptyLine}</p>
            ) : (
              filteredStash.map((movie, i) => {
                const index = i + (addTile ? 1 : 0);
                return (
                  <StashTile
                    key={movie.movieID}
                    movie={movie}
                    cell={index}
                    roving={cell === index}
                    poolFull={poolFull}
                    locked={isLocked}
                    drawInFlight={drawInFlight}
                    poolStateKnown={poolStateKnown}
                    guest={guest}
                    isOwnBoard={isOwnBoard}
                    onOpen={onOpen}
                    onPromote={onPromote}
                  />
                );
              })
            )}
          </div>
        </div>
      </div>
    </section>
  );
}

type PromotionMove = {
  movieID: number;
  cell: number;
  attempt: number;
};

type PoolMove = {
  movieID: number;
  slot: number;
  attempt: number;
};

// Memoized: the filter lives in the pane, so without it each keystroke
// re-renders every tile. Props are cached movies, primitives and stable callbacks.
const StashTile = memo(function StashTile({
  movie,
  cell,
  roving,
  poolFull,
  locked,
  drawInFlight,
  poolStateKnown,
  guest,
  isOwnBoard,
  onOpen,
  onPromote,
}: {
  movie: MovieTile;
  /** This tile's index in the wall, which counts the add tile as a cell. */
  cell: number;
  /** Whether this tile holds the wall's tab stop (see StashPane). */
  roving: boolean;
  poolFull: boolean;
  locked: boolean;
  drawInFlight: boolean;
  poolStateKnown: boolean;
  guest: boolean;
  isOwnBoard: boolean;
  onOpen: (movie: MovieTile) => void;
  /** Stable pane callback into the board-owned request registry. */
  onPromote: (movieID: number, cell: number) => void;
}) {
  // Edit and delete live in the movie modal, which the poster opens.
  return (
    <div className="mem-tile">
      <PosterButton
        movie={movie}
        posterSizes={STASH_POSTER_SIZES}
        cell={cell}
        tabIndex={roving ? 0 : -1}
        onOpen={onOpen}
      />
      {isOwnBoard && (
        <TileAction
          kind="promote"
          refusal={refusalOf({
            kind: "promote",
            isLocked: locked,
            drawInFlight,
            poolFull,
            guest,
            stateKnown: poolStateKnown,
          })}
          tabIndex={roving ? 0 : -1}
          onActivate={() => onPromote(movie.movieID, cell)}
        />
      )}
    </div>
  );
});
