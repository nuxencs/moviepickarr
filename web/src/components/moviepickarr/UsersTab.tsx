import { useMutation, useQuery } from "@tanstack/react-query";
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
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";

import { APIClient } from "@/api/APIClient";
import { MeQueryOptions, SettingsGetPoolLockQueryOptions, UsersGetAllQueryOptions } from "@/api/queries";

import { Avatar } from "@/components/moviepickarr/Bits";
import { drawStore } from "@/components/moviepickarr/drawStore";
import { hueOf, plural } from "@/components/moviepickarr/lib";
import { orderMembers, selectedMember } from "@/components/moviepickarr/membersSearch";
import { MovieModal } from "@/components/moviepickarr/MovieModal";
import { isSelf } from "@/components/moviepickarr/ownership";
import { membersStatus, type RosterOccupancy } from "@/components/moviepickarr/poolLock";
import { possessive } from "@/components/moviepickarr/possessive";
import { Poster } from "@/components/moviepickarr/Poster";
import { actionLabel, type ActionKind, refusalOf, type Refusal } from "@/components/moviepickarr/refusals";
import { SearchModal } from "@/components/moviepickarr/SearchModal";
import { Skeleton, UsersBodySkeleton } from "@/components/moviepickarr/Skeletons";
import { columnCount, filterStash, landingCell, missLine, nextCell } from "@/components/moviepickarr/stashWall";
import { toast } from "@/components/ui/toast-api";

import type { Movie, User } from "@/types/Response";
import type { RefObject } from "react";

import { useMovieModal } from "@/hooks/useMovieModalHistory";

import "@/components/moviepickarr/members.css";

const POOL_SIZE = 3;

/**
 * The width below which the pane is a screen of its own rather than a column
 * beside the rail (#236). Which screen you are on is CSS — this is read only to
 * decide where focus goes, since a push that takes the rail away has to hand
 * focus somewhere and a member switch beside the rail must not move it at all.
 * Keep in step with the media queries in members.css.
 */
const PUSH_WIDTH = "(max-width: 760px)";

const isPushWidth = () => window.matchMedia(PUSH_WIDTH).matches;

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
 *
 * Below 760px that address grows a second half and becomes the mobile push
 * (#236). The two columns are two screens there: the rail is the whole screen,
 * where selecting a member opens their pool in place exactly as it does beside
 * the pane, and `stash` in the URL pushes that member's films over the top of
 * it. Two keys because the narrow layout has two levels — whose pool the rail
 * has open, and whether you have gone on to their wall — and one key saying
 * both would mean a phone could only reach a member by leaving the rail, with
 * nobody else's pool reachable at all.
 *
 * Both halves are the URL rather than a flag of their own, so a resize to
 * desktop lands on the same board rather than on a state built at 375px, and
 * `stash` simply does nothing up there: the pane is already beside the rail.
 * Which screen is drawn is CSS; the only thing read here is where focus goes
 * when the rail is taken away and put back.
 *
 * Every filled poster on either band is a button opening the movie modal, which
 * is the one way into a film's record from this page — never gated on whose
 * board it is, on the lock or on a draw. What a board you cannot act on is
 * missing is exactly one thing, the corner action on its tiles, so the absence
 * says "not your board" and nothing else.
 *
 * Absence means only that. A full pool, a locked round and a draw in flight are
 * all temporary, so they leave the control where it is and turn it inert with
 * the reason on it (see refusals.ts and TileAction) rather than taking it away.
 *
 * The page has one roving region and one not, deliberately (#235). The wall is
 * a run without a bound — sixty films is a hundred and twenty tab stops — so it
 * is a roving-tabindex list with arrow keys, two tab stops on your own board and
 * one on a guest's. The rail is a handful of rows, so it is a handful of plain
 * tab stops. Above both of those: focus moves only when the thing it is sitting
 * on goes away, and then to the nearest thing that is still there.
 */
export function UsersTab() {
  const { data: users, isPending: usersPending, isError: usersError } = useQuery(UsersGetAllQueryOptions());
  // The session member drives the board's self-service gating via isSelf (see
  // ownership.ts): movie actions show only on your own board. Member onboarding
  // and removal live on the admin roster, so this page has no add-member form
  // and no delete action.
  const { data: me } = useQuery(MeQueryOptions());
  const [searchUser, setSearchUser] = useState<User | null>(null);

  // Opening a film's record pushes a history entry, so browser Back closes it
  // (#196). The film handed over is the tile's own lean object and is not
  // re-derived from the roster as the Stats tab does: the modal lazy-loads the
  // full record itself and SSE invalidates that query, so the live source is
  // already the one inside the modal.
  const { selected: openMovie, isOpen, open, close, onClosed } = useMovieModal();

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
  const { member, stash } = useSearch({ from: "/_app/users" });
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

  // Switching member remounts the pane (it is keyed below), which takes the
  // focused wall tile with it and drops focus to the document. The pane cannot
  // remember that across its own remount, so the flag lives one level up: the
  // outgoing pane raises it on the way out and the incoming one lands focus on
  // its heading. A ref, not state — nothing renders differently for it.
  const paneLostFocus = useRef(false);

  // The pane's heading, held here rather than inside the pane: it is where the
  // push lands focus, and the pane is keyed on the member, so on a switch the
  // node the parent focuses is the incoming one (a parent's effect runs after
  // its children have mounted).
  const paneHeadingRef = useRef<HTMLHeadingElement>(null);
  // Every drawer's stash link, by member. The rail outlives the push — it is
  // display: none, not unmounted — so returning can put focus back on the
  // control that opened the screen being left. It is reachable when it is
  // wanted: coming back puts that member's own drawer open, and a shut drawer
  // is inert.
  const stashLinks = useRef(new Map<number, HTMLAnchorElement>());

  // What the URL named last render, so a navigation can be told from an
  // arrival: a cold deep link is not a push and must not move focus. One ref
  // for the three, because they are one thing — the address as it was — and
  // reading two of them from different renders would be the bug this exists to
  // avoid.
  const last = useRef({ pushed: !!stash, member, selectedID: selected?.userID });
  useEffect(() => {
    const was = last.current;
    last.current = { pushed: !!stash, member, selectedID: selected?.userID };
    if ((was.pushed === !!stash && was.member === member) || !isPushWidth()) return;
    // Onto a board: the rail has gone, so the screen's own heading takes focus
    // — the one guaranteed moment a screen-reader user meets the self-mark. A
    // pop between two boards is an entry too, and lands the same way.
    if (stash) {
      paneHeadingRef.current?.focus();
      return;
    }
    // Back to the rail, and only from the pushed screen: switching member on
    // the rail itself takes nothing away, so it moves nothing. Focus goes to
    // the stash link of the board you were on, which is the control you left
    // from.
    //
    // Only when that board is still the open one, which in a phone's own
    // history it always is. Resizing to desktop mid-stack, switching member
    // there and coming back can pop straight from one member's board to
    // another's rail, and the link in a shut drawer is inert: focusing it does
    // nothing at all, so this would report a restore it did not make.
    if (!was.pushed || was.selectedID === undefined || was.selectedID !== selected?.userID) return;
    stashLinks.current.get(was.selectedID)?.focus();
  }, [stash, member, selected?.userID]);
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
      {/* Pushed is the URL, not a flag: below 760 the head goes with the rail
          (see members.css). The whole head, `Members / 6 people` included, not
          just the status line — the pushed screen's title is the possessive
          heading, and two titles do not fit at 375px. */}
      <div className="block mem" data-pushed={!!stash}>
        {/* Split off the visible status span on purpose: this region carries
            the round and draw clauses only, never occupancy (see
            membersStatus). Empty means nothing to announce.

            Outside the head it is drawn beside, and deliberately: the head is
            removed on the pushed screen and a display: none live region
            announces nothing, so this — that screen's only round-state signal —
            sits where the removal cannot reach it. */}
        <span className="vis-hidden" role="status">
          {status.announce}
        </span>

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
                  onOpen={open}
                  stashLinks={stashLinks}
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
              drawInFlight={drawInFlight}
              onOpenSearch={() => setSearchUser(selected)}
              onOpen={open}
              lostFocus={paneLostFocus}
              headingRef={paneHeadingRef}
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

      {openMovie && (
        <MovieModal movie={openMovie} open={isOpen} onRequestClose={close} onClose={onClosed} />
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
  onOpen,
  stashLinks,
}: {
  user: User;
  active: boolean;
  isOwnBoard: boolean;
  isLocked: boolean;
  drawInFlight: boolean;
  onOpen: (movie: Movie) => void;
  /** The page's register of stash links by member, so returning from the mobile
   *  push can put focus back on the control it left from (#236). */
  stashLinks: RefObject<Map<number, HTMLAnchorElement>>;
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

  // Where focus goes when the last film leaves this member's pool: the row the
  // pool hangs off, which is the nearest thing to the slot that emptied and is
  // still on screen (#235).
  const linkRef = useRef<HTMLAnchorElement>(null);

  // The drawer's stash link, registered with the page: it is the control the
  // mobile push is made from, so it is the one focus comes back to (#236).
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
      {/* Always an explicit id, your own included: strip it off your own board
          and the URL you copy shows the recipient their board instead. Two
          URLs rendering the same view is the cheaper cost. A push, not a
          replace, so Back returns to the previous member. */}
      <Link
        to="/users"
        search={{ member: user.userID }}
        className="mem-row__link"
        aria-current={active ? "page" : undefined}
        ref={linkRef}
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
              onOpen={onOpen}
              rowLinkRef={linkRef}
            />

            {/* The way on to this member's films, below 760 only (members.css
                draws it there and nowhere else). It is the second half of the
                address, so it is a link and not a button: the pushed screen
                can be shared, Back leaves it, and the row above stays the
                thing that opens the pool. Tapping a member never leaves the
                rail, which is what keeps everybody else's pool reachable on a
                phone; going on to the wall is a deliberate second move.

                In every drawer rather than the open one alone, so the rail
                keeps a constant height across a switch. A shut drawer is
                inert, so this is only ever reachable on the board it belongs
                to — which is also why the name is `Stash 14` and does not
                repeat whose: exactly one of these is ever exposed, and the row
                directly above it carries the member. */}
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
 * A film's poster, as the button that opens its record. The same cell on both
 * bands and on every board: what the lock and the draw freeze is moving a film,
 * never reading one, so this is not gated on anything.
 *
 * It is a sibling of the corner action, never its parent — a poster that
 * contained the promote button would be a button inside a button. The native
 * tooltip and the authored name are the same string, so the shown and the
 * spoken names cannot drift; the image's alt would name it too, but only while
 * there is a photo to carry one.
 */
function PosterButton({
  movie,
  showArt = true,
  cell,
  tabIndex,
  onOpen,
}: {
  movie: Movie;
  /** Whether to fetch the art. False in a drawer nobody has opened (see RailRow). */
  showArt?: boolean;
  /** This poster's index in its band, so the band can find it again by number
   *  after the film that was there has moved (#235). Both bands use the same
   *  attribute: they are separate containers and each looks only inside itself. */
  cell?: number;
  /** -1 on every wall poster but the one holding the roving index. Left off in
   *  the pool, where three slots are three ordinary tab stops. */
  tabIndex?: number;
  onOpen: (movie: Movie) => void;
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
        <Poster title={movie.title} hue={hueOf(movie.title)} posterPath={movie.posterPath} showTitle={false} />
      )}
    </button>
  );
}

/**
 * Whether focus is still where the move that is landing left it: on the control
 * that is going, or dropped to the document because it has already gone.
 *
 * The page's rule is that focus moves only when the thing it is sitting on goes
 * away, and clicking a control is not a promise to stay on it — click promote,
 * then click into the search field, and the roster arriving a moment later must
 * not pull focus out of the field being typed in.
 */
function focusStillOn(band: HTMLElement | null): boolean {
  return document.activeElement === document.body || !!band?.contains(document.activeElement);
}

/**
 * The one corner action a tile carries: promote on a stash poster, demote on a
 * pool one. Rendered only on your own board, and only ever one per tile.
 *
 * A refusal keeps it and makes it inert with `aria-disabled` and a click that
 * returns early, never with native `disabled`. A natively disabled button
 * cannot take focus, which would make the focus reveal in members.css dead code
 * exactly when it matters and leave a keyboard user tabbing a locked wall
 * without ever meeting the action, let alone the reason for it. The reason
 * rides the accessible name and the tooltip together, where the focus and the
 * pointer already are; nothing is drawn on the tile, because every refusal here
 * is true of the whole wall at once (refusals.ts).
 */
function TileAction({
  kind,
  refusal,
  tabIndex,
  onActivate,
}: {
  kind: ActionKind;
  refusal: Refusal | null;
  /** -1 on every wall tile but the one holding the roving index, so Tab from
   *  the focused poster reaches its own corner action and then leaves the wall.
   *  Left off in the pool. A refusal never touches it: an inert control is
   *  still a control you can reach, which is where its reason is written. */
  tabIndex?: number;
  onActivate: () => void;
}) {
  const label = actionLabel(kind, refusal);
  return (
    <button
      type="button"
      className="mem-act"
      tabIndex={tabIndex}
      // Not `false` when it runs: the attribute is absent, so an allowed
      // control is the same markup it was before any of this.
      aria-disabled={refusal ? true : undefined}
      onClick={() => {
        if (refusal) return;
        onActivate();
      }}
      aria-label={label}
      title={label}
    >
      {/* No glyph swap for a refusal. A blocked mark was drawn and measured at
          26px, where it is legible and unambiguous — and it lost anyway: a full
          pool refuses every promote, so it stamped a forbidden sign across
          every poster on a wall stripped down to art, and read as though the
          films were barred rather than the destination full. */}
      {kind === "promote" ? <MoveUpIcon /> : <MoveDownIcon />}
    </button>
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
  onOpen,
  rowLinkRef,
}: {
  pool: Movie[];
  /** Whether this drawer has been open. The slots are always drawn — they are
   *  what holds every drawer to the same height — but the art inside a drawer
   *  nobody has opened is not fetched (see RailRow). */
  showArt: boolean;
  isOwnBoard: boolean;
  isLocked: boolean;
  drawInFlight: boolean;
  onOpen: (movie: Movie) => void;
  /** The member's own row, where focus goes when the last film leaves the pool. */
  rowLinkRef: RefObject<HTMLAnchorElement | null>;
}) {
  // Demote a pooled movie back to the stash. The move endpoint is directional
  // (target = destination) and idempotent, so a repeat click is a safe no-op —
  // which is why an in-flight move does not disable the button. Gated on
  // isOwnBoard, so it only renders on your own board.
  const demoteMutation = useMutation({
    mutationFn: ({ movieID }: { movieID: number; slot: number }) =>
      APIClient.board.moveMovie(movieID, "stash"),
    // The band is about to lose the control the click is on, so note where it
    // was. Which slot, not which element: the one that lands there is a
    // different node, and this one is on its way out of the DOM.
    //
    // On the way out rather than on the way back: the move and the roster are
    // separate round trips, and the roster arriving over SSE first is ordinary.
    // Noted on success, a fast SSE would beat the note and focus would sit on
    // nothing. A failed move drops it again, having moved nothing.
    onMutate: (moved) => {
      landing.current = moved;
    },
    onError: () => {
      landing.current = null;
      toast.error("Failed to move movie");
    },
  });

  const bandRef = useRef<HTMLDivElement>(null);
  const landing = useRef<{ movieID: number; slot: number } | null>(null);

  // A demote that has landed: the roster has come back over SSE without the
  // film in it, so the slot that held it now holds the next one along. Keyed on
  // the film being gone rather than on the request returning — the two are
  // separate round trips, and focusing between them would land on the tile that
  // is about to unmount and drop focus to the document.
  useEffect(() => {
    const moved = landing.current;
    if (!moved || pool.some((movie) => movie.movieID === moved.movieID)) return;
    landing.current = null;
    if (!focusStillOn(bandRef.current)) return;
    const to = landingCell(moved.slot, pool.length);
    // An empty slot is not focusable and the pool does not reflow around one,
    // so an emptied pool hands focus back to the row it hangs off.
    if (to === null) {
      rowLinkRef.current?.focus();
      return;
    }
    bandRef.current?.querySelector<HTMLElement>(`.mem-open[data-cell="${to}"]`)?.focus();
  }, [pool, rowLinkRef]);

  // Read for the rule's sake and never true of a demote: the way out of a full
  // pool is exactly this control, so it is refusalOf that drops it, not here.
  const poolFull = pool.length >= POOL_SIZE;

  return (
    <div className="mem-pool" ref={bandRef}>
      {Array.from({ length: POOL_SIZE }).map((_, i) => {
        const movie = pool[i];
        return movie ? (
          <div className="pslot pslot--filled" key={movie.movieID}>
            <PosterButton movie={movie} showArt={showArt} cell={i} onOpen={onOpen} />
            {isOwnBoard && (
              <TileAction
                kind="demote"
                refusal={refusalOf({ kind: "demote", isLocked, drawInFlight, poolFull })}
                onActivate={() => demoteMutation.mutate({ movieID: movie.movieID, slot: i })}
              />
            )}
          </div>
        ) : (
          // The one cell on either board that answers nothing, and identical on
          // both: there is no film here to open. Movies reach the pool by being
          // promoted from the stash, not added directly here — a clickable "+"
          // misleadingly implied a direct pool add (it opened the stash search).
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
 *
 * The wall is a roving-tabindex list and not a `role="grid"` (#235). Six columns
 * is a CSS artifact of the container-derived cell width and the wall is an A-Z
 * list of films, so announcing grid coordinates would be describing the
 * stylesheet. One cell holds tabindex 0 at a time; the arrows move it, so the
 * whole wall is two tab stops on your own board (the poster, then its corner
 * action) and one on a guest's, whatever it is holding.
 */
function StashPane({
  user,
  isOwnBoard,
  isLocked,
  drawInFlight,
  onOpenSearch,
  onOpen,
  lostFocus,
  headingRef,
}: {
  user: User;
  isOwnBoard: boolean;
  isLocked: boolean;
  /** Passed down whole rather than pre-judged: a draw does not refuse a promote,
   *  and refusalOf is the one place that decides so. */
  drawInFlight: boolean;
  onOpenSearch: () => void;
  onOpen: (movie: Movie) => void;
  /** Raised on the way out when focus was inside this pane, and read on the way
   *  in: the pane is keyed on the member, so a switch is an unmount. */
  lostFocus: RefObject<boolean>;
  /** The pane's heading, held by the page: it is where focus goes when a tile
   *  is taken from under it, and where the mobile push lands (#236). */
  headingRef: RefObject<HTMLHeadingElement | null>;
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

  // The cell holding the wall's one tab stop. Clamped rather than trusted: the
  // roster arrives over SSE, so the wall can lose the cell the index names
  // without anybody touching the keyboard.
  const [roving, setRoving] = useState(0);
  const cell = Math.min(roving, Math.max(cells - 1, 0));

  const gridRef = useRef<HTMLDivElement>(null);
  /** The poster in a given cell, which is the cell's roving element. */
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

  // Back to the first cell on a filter change, as on a member switch (which is
  // a remount, so it costs nothing here). That lands Tab out of the field on
  // Add on your own board and on the first match on a guest's, which is the
  // right emphasis in both cases. Keeping the index across a filter would mean
  // matching identity between two filtered lists to land on "reset" anyway in
  // the common case. The index only: focus stays in the field being typed in.
  useEffect(() => setRoving(0), [filter]);

  // Whatever focus lands on inside the wall takes the index with it, so the
  // arrows always move from the cell you are actually on. Without this a
  // pointer and the keyboard disagree the moment they are mixed: click a
  // poster, press an arrow, and focus jumps from wherever the index was last
  // left. The corner action counts as its own tile's cell.
  const onWallFocus = (e: React.FocusEvent<HTMLDivElement>) => {
    const target = e.target as HTMLElement;
    const marked = target.hasAttribute("data-cell")
      ? target
      : target.closest(".mem-tile")?.querySelector("[data-cell]");
    const index = Number(marked?.getAttribute("data-cell"));
    if (Number.isInteger(index)) setRoving(index);
  };

  const onWallKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    // Chords belong to the browser: Home with a modifier is not this wall's.
    if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
    const columns = gridRef.current
      ? columnCount(getComputedStyle(gridRef.current).gridTemplateColumns)
      : 1;
    const to = nextCell(e.key, cell, cells, columns);
    if (to === null) return;
    // Only for a move that lands: an arrow the wall refuses is left to the
    // scroller, which is the ordinary thing for a key nothing answered.
    e.preventDefault();
    focusCell(to);
  };

  // A promote that has landed. Same shape and the same reason as the pool's
  // (see PoolSlots): keyed on the film being gone from the wall rather than on
  // the request returning, or focus lands on a tile that is about to unmount.
  const landing = useRef<{ movieID: number; cell: number } | null>(null);
  const onPromoting = useCallback((movieID: number | null, from: number) => {
    landing.current = movieID === null ? null : { movieID, cell: from };
  }, []);
  useEffect(() => {
    const moved = landing.current;
    if (!moved || filteredStash.some((movie) => movie.movieID === moved.movieID)) return;
    landing.current = null;
    if (!focusStillOn(gridRef.current)) return;
    const to = landingCell(moved.cell, cells);
    // The poster taking the vacated cell, never that cell's corner action: the
    // third promote fills the pool, so it would strand you on a control that
    // has just been refused.
    if (to === null) {
      headingRef.current?.focus();
      return;
    }
    focusCell(to);
    // headingRef is a prop now (the page holds it, so the push can land on it),
    // which is why it is listed: it is a ref object and never changes.
  }, [filteredStash, cells, focusCell, headingRef]);

  // Whether focus is inside this pane at all, which is what says a tile
  // unmounting under it was a loss rather than a departure. React's onFocus and
  // onBlur are focusin and focusout, so they bubble and cover every control.
  const holdsFocus = useRef(false);
  // Removing the focused node fires no blur, so focus goes to the document with
  // nothing to say it left. That is the one case worth recovering, and the
  // pane's heading is where it goes: a click on a rail row moves focus to the
  // row itself, which is a departure and is left alone.
  useEffect(() => {
    if (!holdsFocus.current || document.activeElement !== document.body) return;
    holdsFocus.current = false;
    headingRef.current?.focus();
  });
  useEffect(() => {
    if (!lostFocus.current) return;
    lostFocus.current = false;
    headingRef.current?.focus();
    // The other half of the same rule, across the remount a member switch is.
    // Mount only; the effect above owns every later loss.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(
    () => () => {
      if (holdsFocus.current) lostFocus.current = true;
    },
    [lostFocus],
  );

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
    <section
      className="mem-pane"
      aria-labelledby={headingID}
      onFocus={() => {
        holdsFocus.current = true;
      }}
      onBlur={() => {
        holdsFocus.current = false;
      }}
    >
      {/* The mobile push's back bar, drawn only below 760 (members.css) and so
          always in the DOM: which screen this is is a media query, and a render
          condition on it would have to survive a resize.

          It carries the way back and the pips, and nothing else. The pips are
          the only occupancy signal on this screen — the rail is not here, a
          promote fills a pool one screen away, and the move is silent unless it
          fails — which is why they take role="img" and a label. Nothing is
          pinned: the app scrolls under a fixed 63px nav, so top: 0 parks a
          sticky bar behind it, and a bar with a background would be a permanent
          band of chrome across a screen made of art. Going back for the pips is
          one flick. */}
      <div className="mem-backbar">
        {/* Plain history-back. The router's history exposes no can-go-back, so
            a cold deep link exits the app — what Back does on any deep-linked
            detail screen. */}
        <button type="button" className="mem-back" onClick={() => router.history.back()}>
          <ArrowLeftIcon />
          All members
        </button>
        <PoolPips filled={pooled} />
      </div>

      <div className="mem-stash">
        <div className="mem-stash__head">
          {/* The positive self-mark, and the last thing on the pane saying whose
              board you are looking at. Emphasis is symmetric: the possessive
              token takes the ink and the noun steps back, in both directions, so
              lifting "Your" alone is never a self-mark rendered in colour. One
              line with an end ellipsis and a title — a heading that wrapped
              would start one member's wall a line lower than another's. */}
          <div className="mem-stash__id">
            {/* Focusable but never a tab stop: it is where focus goes when the
                thing holding it is taken away, and the pane it names is the
                nearest thing that is still there (#235). It is also where the
                mobile push lands, which is the one guaranteed moment a
                screen-reader user meets the self-mark (#236) — so the
                tabIndex={-1} is load-bearing, not dead code. */}
            <h3
              id={headingID}
              className="mem-stash__title"
              title={`${who} stash`}
              ref={headingRef}
              tabIndex={-1}
            >
              <span className="mem-stash__who">{who}</span> stash
            </h3>
            {/* The count, on the pushed screen only (members.css hides it above
                760). Beside the rail the row carries it in every state, and a
                second copy in the pane put "who has the deepest stash" across
                two type scales 600px apart; pushed, the rail is another screen
                and the heading is where the number has to be. */}
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
          {/* The keys are handled on the wall rather than per cell, so they
              answer from the corner action too: an arrow from there moves to
              the next poster the same way it does from a poster. */}
          <div
            className={`mem-wall${emptyLine ? " mem-wall--empty" : ""}`}
            ref={gridRef}
            onKeyDown={onWallKeyDown}
            onFocus={onWallFocus}
          >
            {addTile && (
              // No label under it: a dashed cell with a plus in it is not
              // ambiguous, and spelling it out was the only text left in the
              // pane, which made it read as a heading rather than as a cell.
              // Icon-only, so the name is authored.
              // Inside the roving list rather than a stop before it, which is
              // what puts Tab out of the field on Add without costing the wall
              // a third tab stop.
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
              // Not a tab stop at all: a wall with no matches has no cells, so
              // Tab out of the field goes straight past it to the line here.
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
                    isOwnBoard={isOwnBoard}
                    onOpen={onOpen}
                    onPromoting={onPromoting}
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

// Memoized because the stash filter lives in the pane above: without it every
// keystroke re-renders every surviving tile, and at 60 films that is 60 posters
// and 60 mutation hooks. The props are the movie object straight out of the
// query cache plus primitives, plus an onOpen that is stable across renders
// (useMovieModal's useCallback), so the memo holds while typing.
const StashTile = memo(function StashTile({
  movie,
  cell,
  roving,
  poolFull,
  locked,
  drawInFlight,
  isOwnBoard,
  onOpen,
  onPromoting,
}: {
  movie: Movie;
  /** This tile's index in the wall, which counts the add tile as a cell. */
  cell: number;
  /** Whether this tile holds the wall's tab stop (see StashPane). */
  roving: boolean;
  poolFull: boolean;
  locked: boolean;
  drawInFlight: boolean;
  isOwnBoard: boolean;
  onOpen: (movie: Movie) => void;
  /** A promote leaving this cell, and a null film withdrawing one that failed.
   *  Stable across renders, so the memo holds while the filter is typed in. */
  onPromoting: (movieID: number | null, cell: number) => void;
}) {
  // Promote to the pool: the one control the tile carries, and only on your own
  // board. Edit and delete are not here — they live in the movie modal, which
  // the poster itself opens. Idempotent like the demote, so an in-flight move
  // does not disable it either.
  const moveMutation = useMutation({
    mutationFn: () => APIClient.board.moveMovie(movie.movieID, "pool"),
    // The tile is about to leave the wall, so the pane is told which cell it
    // was in. It cannot land focus itself: it will not be here to do it. On the
    // way out, for the reason the demote's onMutate gives.
    onMutate: () => onPromoting(movie.movieID, cell),
    onError: () => {
      onPromoting(null, cell);
      toast.error("Failed to move movie");
    },
  });

  return (
    <div className="mem-tile">
      <PosterButton movie={movie} cell={cell} tabIndex={roving ? 0 : -1} onOpen={onOpen} />
      {isOwnBoard && (
        <TileAction
          kind="promote"
          refusal={refusalOf({ kind: "promote", isLocked: locked, drawInFlight, poolFull })}
          tabIndex={roving ? 0 : -1}
          onActivate={() => moveMutation.mutate()}
        />
      )}
    </div>
  );
});
