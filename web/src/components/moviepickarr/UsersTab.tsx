import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import { MoveDownIcon, MoveUpIcon, PlusIcon, SearchIcon } from "lucide-react";
import { memo, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";

import { APIClient } from "@/api/APIClient";
import { MeQueryOptions, SettingsGetPoolLockQueryOptions, UsersGetAllQueryOptions } from "@/api/queries";

import { Avatar } from "@/components/moviepickarr/Bits";
import { drawStore } from "@/components/moviepickarr/drawStore";
import { hueOf, plural } from "@/components/moviepickarr/lib";
import { orderMembers, selectedMember } from "@/components/moviepickarr/membersSearch";
import { isSelf } from "@/components/moviepickarr/ownership";
import { membersStatus, type RosterOccupancy } from "@/components/moviepickarr/poolLock";
import { possessive } from "@/components/moviepickarr/possessive";
import { Poster } from "@/components/moviepickarr/Poster";
import { SearchModal } from "@/components/moviepickarr/SearchModal";
import { Skeleton, UsersBodySkeleton } from "@/components/moviepickarr/Skeletons";
import { filterStash, missLine } from "@/components/moviepickarr/stashWall";
import { toast } from "@/components/ui/toast-api";

import type { Movie, User } from "@/types/Response";

import "@/components/moviepickarr/members.css";

const POOL_SIZE = 3;

/**
 * The Members page: a rail of members beside one board pane.
 *
 * The rail carries every member equally, one row each, with the two numbers
 * that matter about a person on it — how deep their stash is and how full
 * their pool is — so scanning the group never means opening anybody's board.
 * Selecting a member opens that row downward onto their three pool slots; the
 * pane to the right holds that member's stash.
 *
 * Which member is selected is an address (`/users?member=<userID>`, see
 * membersSearch), not component state, so a board can be linked to and Back
 * returns to the member you were looking at before.
 */
export function UsersTab() {
  const { data: users, isPending: usersPending, isError: usersError } = useQuery(UsersGetAllQueryOptions());
  // The session member drives the board's self-service gating via isSelf (see
  // ownership.ts): movie actions show only on your own board. Member onboarding
  // and removal live on the admin roster, so this page has no add-member form
  // and no delete action.
  const { data: me } = useQuery(MeQueryOptions());
  const [searchUser, setSearchUser] = useState<User | null>(null);

  // The page status line: one answer to "are we ready" for the whole group, so
  // nobody has to count pips across six boards. The words and the composition
  // live in poolLock.ts; this only gathers the three inputs.
  const { data: isLocked, isPending: lockPending } = useQuery(SettingsGetPoolLockQueryOptions());
  const drawPhase = useSyncExternalStore(drawStore.subscribe, () => drawStore.getState().phase);
  const occupancy = useMemo<RosterOccupancy>(() => {
    if (usersError) return { state: "error" };
    // The lock is a second query, so a full roster can land before it. Stay
    // pending until both are in: reading a locked round as `ready to lock` for
    // a frame would announce a thing to go do that isn't there to be done.
    if (usersPending || lockPending || !users) return { state: "pending" };
    return {
      state: "ready",
      // Raw slot occupancy across everybody. It never moves for a draw: the
      // server leaves the winner in its pool until the reveal, and a count that
      // dropped early would give the film away.
      filled: users.reduce((n, user) => n + Object.keys(user.currentPool).length, 0),
      slots: users.length * POOL_SIZE,
    };
  }, [users, usersPending, usersError, lockPending]);
  const status = membersStatus(occupancy, !!isLocked, drawPhase);

  // useSearch keys off the route id, which is `/_app/users` while the URL stays
  // `/users` (the page hangs off the pathless app layout) — same split the
  // Stats tab documents.
  const { member } = useSearch({ from: "/_app/users" });
  const ordered = useMemo(() => orderMembers(users, me?.id), [users, me?.id]);
  const selected = selectedMember(ordered, member, me?.id);

  // The pool is frozen server-side while a draw is unrevealed: the drawn movie
  // is still shown as a pool tile, so demoting any of them has to be refused
  // (letting one tile through would say which movie was drawn). Match that here
  // instead of letting the click come back as a failed move.
  const drawInFlight = useSyncExternalStore(
    drawStore.subscribe,
    () => drawStore.getState().phase !== "idle",
  );

  // The rail's scrollbar is out of the layout (its width is three posters
  // exactly), so a fade at the bottom edge is the only sign there are more
  // members below — and it has to be conditional, or it just dims the last
  // name. This asks whether the rail overflows, not by how much: a comparison,
  // not a size, so the root zoom ramp does not touch it.
  const railRef = useRef<HTMLElement>(null);
  const [railOverflows, setRailOverflows] = useState(false);
  useEffect(() => {
    const rail = railRef.current;
    if (!rail) return;
    const check = () => setRailOverflows(rail.scrollHeight > rail.clientHeight + 1);
    check();
    // On the first pass the rail has not been given its constrained height yet,
    // so a mount-time check always says it fits. One frame later it knows.
    const frame = requestAnimationFrame(check);
    const ro = new ResizeObserver(check);
    ro.observe(rail);
    // Opening a drawer changes what the rail holds without changing the rail,
    // so the observer alone would miss it. The drawer's transition says when.
    rail.addEventListener("transitionend", check);
    return () => {
      cancelAnimationFrame(frame);
      ro.disconnect();
      rail.removeEventListener("transitionend", check);
    };
  }, [ordered.length, selected?.userID]);

  return (
    <>
      <div className="block mem">
        <div className="sec-head">
          <div className="sec-title">
            <h2>Members</h2>
            {/* No count until there's a roster to count: "0 people" while the
                query is in flight states the one number this slot exists for,
                wrongly. The row is flex, so leaving it out moves nothing. */}
            {users && <span className="sec-count">{plural(users.length, "person", "people")}</span>}
            {status.text === null ? (
              <Skeleton w={132} h={12} />
            ) : (
              <span className="sec-status mono">{status.text}</span>
            )}
            {/* Split off the visible span on purpose: this region carries the
                round and draw clauses only, never occupancy. See membersStatus
                for why. Empty means nothing to announce. */}
            <span className="vis-hidden" role="status">
              {status.announce}
            </span>
          </div>
        </div>

        {usersError ? (
          <p className="empty text-destructive">Failed to load members.</p>
        ) : usersPending ? (
          // Still the incumbent board skeleton; the rail's own is #239.
          <div className="boards">
            <UsersBodySkeleton />
          </div>
        ) : selected ? (
          <div className="mem__shell">
            {/* A nav of links with aria-current on the selected row, matching
                the app's primary nav — not a disclosure and not a tab list. No
                aria-expanded: a row navigates, and the drawer opening is a
                consequence of the row being current. Six plain tab stops, no
                roving; activating a row leaves focus on the row, so arriving
                (where you are already selected and nothing was activated) and
                switching behave the same. Enter activates and Space scrolls,
                as on any link — deliberately not hand-rolled. */}
            <nav
              className="mem-rail"
              aria-label="Members"
              ref={railRef}
              data-overflow={railOverflows}
            >
              {ordered.map((user) => (
                <RailRow
                  key={user.userID}
                  user={user}
                  active={user.userID === selected.userID}
                  isOwnBoard={isSelf(me?.id, user.userID)}
                  isLocked={!!isLocked}
                  drawInFlight={drawInFlight}
                />
              ))}
            </nav>

            {/* Keyed on the member, so a switch remounts the pane: the
                scroller outlives its contents otherwise and you land halfway
                down the next person's stash, with their filter still typed in
                the box. */}
            <StashPane
              key={selected.userID}
              user={selected}
              isOwnBoard={isSelf(me?.id, selected.userID)}
              isLocked={!!isLocked}
              onOpenSearch={() => setSearchUser(selected)}
            />
          </div>
        ) : (
          // Unreachable, kept as a defensive fallback: the endpoint lists
          // non-archived members only, and archiving deletes that member's
          // logins and sessions in the same transaction, so a live session
          // implies at least your own row.
          <p className="empty">No members yet</p>
        )}
      </div>

      {searchUser && (
        <SearchModal userName={searchUser.name} onClose={() => setSearchUser(null)} />
      )}
    </>
  );
}

/**
 * One member in the rail: the link that selects them, and the drawer that
 * holds their pool.
 *
 * The row's accessible name is composed from its contents and never authored
 * as an aria-label, so the visible and the spoken strings cannot drift —
 * `Ada 14 in stash 2 of 3 slots filled`, in DOM order, with the punctuation
 * the screen reader's own. That is why the avatar's initials are aria-hidden
 * (in Bits) and why the pips carry role="img" with their label: a roleless
 * span's label is dropped when it stands alone.
 */
function RailRow({
  user,
  active,
  isOwnBoard,
  isLocked,
  drawInFlight,
}: {
  user: User;
  active: boolean;
  isOwnBoard: boolean;
  isLocked: boolean;
  drawInFlight: boolean;
}) {
  const pool = useMemo(
    () => Object.values(user.currentPool).sort((a, b) => a.title.localeCompare(b.title)),
    [user.currentPool],
  );
  const stashCount = Object.keys(user.stash).length;

  // A shut drawer's slots are drawn but its art is not fetched: five hidden
  // pools would cost fifteen TMDB requests on arrival for something nobody is
  // looking at. `loading="lazy"` does not do this on its own — a zero-height
  // image inside the viewport is "near enough" for Chrome and loads anyway
  // (measured). Once a drawer has been opened its posters stay: dropping them
  // again would pop the art out of a drawer that is still closing.
  const [everOpened, setEverOpened] = useState(active);
  useEffect(() => {
    if (active) setEverOpened(true);
  }, [active]);

  return (
    <div className="mem-row" data-active={active}>
      {/* Always an explicit id, your own included: strip it off your own board
          and the URL you copy shows the recipient their board instead. Two
          URLs rendering the same view is the cheaper cost. A push, not a
          replace, so Back returns to the previous member. */}
      <Link
        to="/users"
        search={{ member: user.userID }}
        className="mem-row__link"
        aria-current={active ? "page" : undefined}
      >
        <Avatar name={user.name} size={30} />
        <span className="mem-row__text">
          {/* No self-mark anywhere in the rail: no chip, no tint, no border.
              On arrival your row is first and selected, and selection already
              speaks three times. Full name, because a roster has to separate
              two people who share a first one. */}
          <span className="mem-row__nm" title={user.name}>
            {user.name}
          </span>
          <span className="mem-row__ct mono">{stashCount} in stash</span>
        </span>
        {/* The pips are what the row says when it is shut. Open, the pool
            itself is right there, so they would be saying it twice. */}
        {!active && <PoolPips filled={pool.length} />}
      </Link>

      {/* Every drawer stays mounted, open or not, and goes inert when shut.
          Unmounting the shut one collapses it instantly while the new one
          animates, so the rail loses a drawer's height and grows it back — and
          0fr → 1fr needs both rows present to interpolate at all. `inert` is
          the real React 19 prop, and it is not interchangeable with
          aria-hidden (which would leave the demote buttons tabbable) or with
          visibility: hidden (the drawer has to be visible mid-transition). */}
      <div className="mem-drop" data-open={active}>
        <div className="mem-drop__inner" inert={!active}>
          <div className="mem-drop__body">
            <PoolSlots
              pool={pool}
              showArt={everOpened}
              isOwnBoard={isOwnBoard}
              isLocked={isLocked}
              drawInFlight={drawInFlight}
            />
          </div>
        </div>
      </div>
    </div>
  );
}

/** Three tiny squares standing in for a pool: the rail's whole read at a
 *  glance. role="img" so the label survives — a bare span's is dropped. */
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
 * The open row's contents: that member's pool, always exactly POOL_SIZE slots
 * and never reordered — the draw is random, so a slot carries no priority.
 *
 * No "Pool" heading and no occupancy line: the drawer hangs off the member's
 * own row, which is all the label three slots need, and how full the pool is
 * is already said by the pips, by the slots themselves and by the page status
 * line. A hint on your own empty pool would give one drawer a different height
 * from everyone else's, which is the accordion's whole constraint.
 */
function PoolSlots({
  pool,
  showArt,
  isOwnBoard,
  isLocked,
  drawInFlight,
}: {
  pool: Movie[];
  /** Whether this drawer has been open. The slots are always drawn — they are
   *  what holds every drawer to the same height — but the art inside a drawer
   *  nobody has opened is not fetched (see RailRow). */
  showArt: boolean;
  isOwnBoard: boolean;
  isLocked: boolean;
  drawInFlight: boolean;
}) {
  // Demote a pooled movie back to the stash. The move endpoint is directional
  // (target = destination) and idempotent, so a repeat click is a safe no-op.
  // Gated on isOwnBoard, so it only renders on your own board.
  const demoteMutation = useMutation({
    mutationFn: (movieID: number) => APIClient.board.moveMovie(movieID, "stash"),
    onError: () => toast.error("Failed to move movie"),
  });

  return (
    <div className="mem-pool">
      {Array.from({ length: POOL_SIZE }).map((_, i) => {
        const movie = pool[i];
        return movie ? (
          <div className="pslot pslot--filled" key={movie.movieID} title={movie.title}>
            {showArt && (
              <Poster title={movie.title} hue={hueOf(movie.title)} posterPath={movie.posterPath} showTitle={false} />
            )}
            {isOwnBoard && !isLocked && (
              <button
                type="button"
                className="mem-act"
                onClick={() => demoteMutation.mutate(movie.movieID)}
                disabled={demoteMutation.isPending || drawInFlight}
                aria-label="Move back to stash"
                title={drawInFlight ? "A draw is in progress" : "Move back to stash"}
              >
                <MoveDownIcon />
              </button>
            )}
          </div>
        ) : (
          // Empty slots are non-interactive placeholders. Movies reach the pool
          // by being promoted from the stash, not added directly here — a clickable
          // "+" misleadingly implied a direct pool add (it opened the stash search).
          <div className="pslot pslot--empty" key={`empty-${i}`} aria-hidden="true" />
        );
      })}
    </div>
  );
}

/**
 * The pane: the selected member's stash as a wall of untitled posters, and on
 * your own board the way to add to it.
 *
 * The wall is six columns of 96px posters with no caption under the tile, which
 * is what the density prototypes settled (docs/findings/members-204-stash):
 * dropping the caption buys half again as many films at a size where the art
 * still identifies one. Six is also the floor — four columns puts a stash
 * poster above the rail's 128px pool poster and inverts the ranking the layout
 * exists to express — so the ceiling on poster size here is arithmetic off the
 * rail's width, not taste.
 *
 * Order is fixed title-ascending and there is no sort control: the only keys
 * that are always present are title and date-added, the rest arrive with
 * enrichment and would reorder the wall under you as SSE lands, and an untitled
 * tile makes no key but title verifiable by looking. The field below is the
 * find-a-film path in its place.
 */
function StashPane({
  user,
  isOwnBoard,
  isLocked,
  onOpenSearch,
}: {
  user: User;
  isOwnBoard: boolean;
  isLocked: boolean;
  onOpenSearch: () => void;
}) {
  const [filter, setFilter] = useState("");

  const stash = useMemo(
    () => Object.values(user.stash).sort((a, b) => a.title.localeCompare(b.title)),
    [user.stash],
  );
  const filteredStash = useMemo(() => filterStash(stash, filter), [stash, filter]);

  const poolFull = Object.keys(user.currentPool).length >= POOL_SIZE;
  const firstName = user.name.split(" ")[0];
  // Names split deliberately: the rail carries the full name, because a roster
  // has to separate two people who share a first one; the heading carries the
  // first, because "Ada's stash" reads like speech. Two members sharing a first
  // name give two identically titled panes, which is a known limit.
  const who = isOwnBoard ? "Your" : possessive(firstName);
  const headingID = `mem-stash-${user.userID}`;

  // The scrollbar is out of the layout (see members.css), so a fade at the
  // bottom edge is the only sign there is more wall below — and conditional, or
  // it dims the last row for nothing. A comparison, not a size, so the root
  // zoom ramp does not reach it. Same shape as the rail's, one scroller up.
  const wallRef = useRef<HTMLDivElement>(null);
  const [wallOverflows, setWallOverflows] = useState(false);

  // Your own board's add tile sits at cell 0 of the wall, so the act of growing
  // the stash lives inside the thing it grows. It is suppressed under any
  // filter, hit or miss: a dashed cell one keystroke from a row of search hits
  // reads as a result.
  const addTile = isOwnBoard && !filter.trim();
  // The cell count, not the film count: the add tile is a cell too, and a term
  // that matches every film takes it away without moving the other number.
  const cells = filteredStash.length + (addTile ? 1 : 0);

  useEffect(() => {
    const wall = wallRef.current;
    if (!wall) return;
    const check = () => setWallOverflows(wall.scrollHeight > wall.clientHeight + 1);
    check();
    // The observer catches the width changing under it, which moves both the
    // column count and the cell height and so the number of rows.
    const ro = new ResizeObserver(check);
    ro.observe(wall);
    return () => ro.disconnect();
  }, [cells]);
  // Four empty states, and only one of them is prose. Your own empty wall is the
  // add tile and nothing else — the add affordance is not reachable from the
  // empty state, it *is* the empty state, and a sentence beside one dashed cell
  // would be the only words in a pane emptied of them. Everything else says so
  // in one line, because a blank pane cannot be told apart from a switch that
  // failed. No name in it (the heading two lines up has it) and no "yet", which
  // is an expectation you do not get to hold about someone else's stash.
  const emptyLine =
    filteredStash.length > 0 || addTile
      ? null
      : filter.trim()
        ? missLine(filter)
        : "This stash is empty";

  return (
    <section className="mem-pane" aria-labelledby={headingID}>
      <div className="mem-stash">
        <div className="mem-stash__head">
          {/* The positive self-mark, and the last thing on the pane saying whose
              board you are looking at. Emphasis is symmetric: the possessive
              token takes the ink and the noun steps back, in both directions, so
              lifting "Your" alone is never a self-mark rendered in colour. One
              line with an end ellipsis and a title — a heading that wrapped
              would start one member's wall a line lower than another's. */}
          <div className="mem-stash__id">
            <h3 id={headingID} className="mem-stash__title" title={`${who} stash`}>
              <span className="mem-stash__who">{who}</span> stash
            </h3>
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

        <div className="mem-wallbox" ref={wallRef} data-overflow={wallOverflows}>
          <div className={`mem-wall${emptyLine ? " mem-wall--empty" : ""}`}>
            {addTile && (
              // No label under it: a dashed cell with a plus in it is not
              // ambiguous, and spelling it out was the only text left in the
              // pane, which made it read as a heading rather than as a cell.
              // Icon-only, so the name is authored.
              <button
                type="button"
                className="mem-addtile"
                onClick={onOpenSearch}
                aria-label={`Add to ${possessive(firstName)} stash`}
                title={`Add to ${possessive(firstName)} stash`}
              >
                <PlusIcon />
              </button>
            )}
            {emptyLine ? (
              <p className="empty mem-wall__empty">{emptyLine}</p>
            ) : (
              filteredStash.map((movie) => (
                <StashTile
                  key={movie.movieID}
                  movie={movie}
                  poolFull={poolFull}
                  locked={isLocked}
                  isOwnBoard={isOwnBoard}
                />
              ))
            )}
          </div>
        </div>
      </div>
    </section>
  );
}

// Memoized because the stash filter lives in the pane above: without it every
// keystroke re-renders every surviving tile, and at 60 films that is 60 posters
// and 60 mutation hooks. The props are the movie object straight out of the
// query cache plus primitives, so the memo holds while typing.
const StashTile = memo(function StashTile({
  movie,
  poolFull,
  locked,
  isOwnBoard,
}: {
  movie: Movie;
  poolFull: boolean;
  locked: boolean;
  isOwnBoard: boolean;
}) {
  // Promote to the pool: the one control the tile carries, and only on your own
  // board. Edit and delete are not here — they move to the movie modal, which
  // is also what will make the poster itself openable.
  const moveMutation = useMutation({
    mutationFn: () => APIClient.board.moveMovie(movie.movieID, "pool"),
    onError: () => toast.error("Failed to move movie"),
  });

  return (
    // The native tooltip is what names an untitled poster on hover; the image's
    // alt carries the same title for anyone not hovering anything.
    <div className="mem-tile" title={movie.title}>
      <Poster title={movie.title} hue={hueOf(movie.title)} posterPath={movie.posterPath} showTitle={false} />
      {isOwnBoard && (
        <button
          type="button"
          className="mem-act"
          onClick={() => moveMutation.mutate()}
          disabled={locked || poolFull || moveMutation.isPending}
          aria-label="Promote to pool"
          title={poolFull ? "Pool is full" : locked ? "Pool is locked" : "Promote to pool"}
        >
          <MoveUpIcon />
        </button>
      )}
    </div>
  );
});
