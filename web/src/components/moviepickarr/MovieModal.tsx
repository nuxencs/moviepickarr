import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ExternalLinkIcon, PencilIcon, Trash2Icon, XIcon } from "lucide-react";
import { Fragment, type ReactNode, useEffect, useLayoutEffect, useRef, useState } from "react";

import { APIClient } from "@/api/APIClient";
import { MeQueryOptions, MovieDetailQueryOptions, SettingsGetPoolStateQueryOptions } from "@/api/queries";

import { EditMovieDialog } from "@/components/EditMovieDialog";
import { Avatar, MetaChips } from "@/components/moviepickarr/Bits";
import { backdropBg, backdropUrl, externalLinks, fullDate, hueOf, posterUrl, profileUrl, tmdbPersonUrl } from "@/components/moviepickarr/lib";
import { Modal } from "@/components/moviepickarr/Modal";
import { isSelf } from "@/components/moviepickarr/ownership";
import { possessive } from "@/components/moviepickarr/possessive";
import { Poster } from "@/components/moviepickarr/Poster";
import { deleteLabel, deleteRefusalOf, isDeletable } from "@/components/moviepickarr/refusals";
import { SkeletonText } from "@/components/moviepickarr/Skeletons";
import { DeletionDialog } from "@/components/ui/deletion-dialog";
import { toast } from "@/components/ui/toast-api";

import type { CreditPerson, MovieDetail, MovieTile } from "@/types/Response";

/** First occurrence per person id (a writer credited for Writer AND Screenplay shows once). */
function dedupeById(people: CreditPerson[]): CreditPerson[] {
  const seen = new Set<number>();
  return people.filter((p) => {
    if (seen.has(p.id)) return false;
    seen.add(p.id);
    return true;
  });
}

/** Comma-separated credit names, each a link out to its TMDB person page. */
function PersonLinks({ people }: { people: CreditPerson[] }) {
  return (
    <>
      {people.map((p, i) => (
        <Fragment key={p.id}>
          {i > 0 && ", "}
          <a
            className="moviemodal__person"
            href={tmdbPersonUrl(p.id)}
            target="_blank"
            rel="noopener noreferrer"
          >
            {p.name}
          </a>
        </Fragment>
      ))}
    </>
  );
}

/** One credit line's worth of held space (`height: 1lh`), carrying a shorter
 *  skeleton bar inside it — so the row a landing credit will fill is already
 *  the right height rather than the height of the bar. */
function GhostCreditRow({ w }: { w: number }) {
  return (
    <span className="moviemodal__credits__ghost" aria-hidden="true">
      <SkeletonText w={w} h={12} />
    </span>
  );
}

/** Modal hero backdrop — the wide-format twin of `Poster`. The procedural
 *  duotone (backdropBg) is painted underneath as the instant first frame, so a
 *  slow TMDB CDN fetch cannot flash the surface through (pure white in light
 *  mode). The photograph becomes a full-width decorative layer after it loads.
 *  The layer overscans the scroll owner's safe width so native scrollbar chrome
 *  paints above artwork instead of an empty surface strip. */
function HeroBackdrop({
  hue,
  src,
  /** True while the detail that carries `backdropPath` is still in flight. The
   *  duotone holds with its shimmer rather than resolving to a stand-in we may
   *  be about to replace. */
  pending,
  /** What `src` actually is. A poster in the wide hero is a stand-in, and the
  *  rail shows that same poster sharp a few pixels below. A dark wash makes it
  *  read as a colour field instead of the poster printed twice. */
  wash = false,
  children,
}: {
  hue: number;
  src: string | null;
  pending: boolean;
  wash?: boolean;
  children: ReactNode;
}) {
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);
  const imgRef = useRef<HTMLImageElement>(null);

  // Sync `loaded` from a cached image's `complete` before paint so a reopened
  // modal (or an SSE-swapped backdrop) doesn't re-flash the placeholder.
  useLayoutEffect(() => {
    const img = imgRef.current;
    setFailed(false);
    setLoaded(Boolean(img?.complete && img.naturalWidth > 0));
  }, [src]);

  const url = failed ? null : src;
  const loading = pending || (url !== null && !loaded);
  // A poster that 404s leaves the duotone showing. The duotone uses the normal
  // scrim because the deeper one only supports a loaded stand-in.
  const washing = wash && url !== null;
  const photograph = url !== null && loaded ? `url(${JSON.stringify(url)})` : null;
  const backdrop = photograph
    ? washing
      ? `linear-gradient(rgba(8, 9, 14, 0.48), rgba(8, 9, 14, 0.48)), ${photograph}`
      : photograph
    : backdropBg(hue);
  const surfaceMask =
    "linear-gradient(to bottom, transparent 0 var(--moviemodal-hero-height), var(--surface) var(--moviemodal-hero-height) 100%)";
  const bottomFade = "linear-gradient(0deg, var(--surface), transparent 72%)";
  const sideFade = washing
    ? "linear-gradient(95deg, rgba(8, 9, 14, 0.68), rgba(8, 9, 14, 0.18) 60%)"
    : "linear-gradient(95deg, rgba(8, 9, 14, 0.5), transparent 60%)";
  const backgroundImage = `${surfaceMask}, ${bottomFade}, ${sideFade}, ${backdrop}`;
  // Overlap the fade with the opaque body mask by one CSS pixel. WebKit and
  // Gecko can otherwise round their shared edge to different device pixels.
  const fadeHeight = "calc(var(--moviemodal-hero-height) + 1px)";
  const backgroundSize = photograph
    ? washing
      ? `100% 100%, 100% ${fadeHeight}, 100% var(--moviemodal-hero-height), 100% var(--moviemodal-hero-height), 100% auto`
      : `100% 100%, 100% ${fadeHeight}, 100% var(--moviemodal-hero-height), 100% auto`
    : `100% 100%, 100% ${fadeHeight}, 100% var(--moviemodal-hero-height), 100% var(--moviemodal-hero-height), 100% var(--moviemodal-hero-height), 100% var(--moviemodal-hero-height)`;

  return (
    <div className="modal__scroll moviemodal__scroll">
      <div
        className="moviemodal__backdrop"
        style={{ backgroundImage, backgroundSize }}
        aria-hidden="true"
      />
      <div className="moviemodal__hero">
        {url && (
          <img
            ref={imgRef}
            className={`moviemodal__hero__preload${wash ? " moviemodal__hero__preload--wash" : ""}`}
            src={url}
            alt=""
            hidden
            onLoad={() => setLoaded(true)}
            onError={() => setFailed(true)}
          />
        )}
        {loading && <div className="moviemodal__hero__shimmer" aria-hidden="true" />}
      </div>
      {children}
    </div>
  );
}

/**
 * Rename and delete, at the foot of the modal's rail.
 *
 * Whether it is drawn at all is derived from the movie in hand, not handed down
 * as a prop (#237): the alternative rule reads, to a member, "you may rename a
 * movie you added, if you opened it from Members" — the same movie on the same
 * surface reached by the same gesture from the pool wall would offer nothing,
 * and no part of the interface could account for the difference.
 *
 * Both actions are adder-only server-side on both endpoints, with no admin
 * override, so a guest's record simply opens without this block: absence is the
 * expression of permission here, the same as it is on a board that isn't yours.
 *
 * Edit is two capabilities in one dialog and the weaker one is what Members
 * wants: a rename, of a string the poster wall does not even show. The link
 * field is the load-bearing half — writing it re-points the movie's IMDb
 * identity and re-enriches it — so it stays, whatever the dialog's flat copy
 * makes of it.
 */
function MovieActions({
  movie,
  /** The modal's own open-ness. A child dialog is mounted only while it holds,
   *  so browser Back — which withdraws it — closes both at once and "Back
   *  closes the modal" stays one rule. */
  open,
  /** Delete lands on the movie's record, so its success takes the record away:
   *  same path as every other dismissal, so the history entry is popped once. */
  onDeleted,
  recordStateKnown,
}: {
  movie: MovieDetail;
  open: boolean;
  onDeleted: () => void;
  /** False while the lifecycle-bearing detail is refreshing or failed. */
  recordStateKnown: boolean;
}) {
  const [editOpen, setEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);

  // Only pooled movies need either pool gate. Stash deletes ignore both, while
  // current and watched movies never offer Delete. This also avoids a settings
  // read when the modal opens on an unrelated lifecycle state.
  const needsPoolState = movie.status === "pool";
  const {
    data: poolState,
    isError: poolStateError,
    isFetching: poolStateFetching,
  } = useQuery(SettingsGetPoolStateQueryOptions(needsPoolState));
  const poolStateKnown =
    !needsPoolState ||
    (poolState !== undefined && !poolStateError && !poolStateFetching);
  const isLocked = !!poolState?.poolLocked;
  const drawInFlight = !!poolState?.drawInProgress;
  const refusal = deleteRefusalOf({
    status: movie.status,
    isLocked: !!isLocked,
    drawInFlight,
    stateKnown: recordStateKnown && poolStateKnown,
  });
  const label = deleteLabel(refusal);

  const editMutation = useMutation({
    mutationFn: (payload: { title: string; link: string }) =>
      APIClient.board.updateMovie(movie.movieID, payload.title, payload.link),
    onSuccess: () => {
      toast.success(`${movie.title} updated`);
      setEditOpen(false);
    },
    onError: () => toast.error("Failed to update movie"),
  });

  const deleteMutation = useMutation({
    mutationFn: () => APIClient.board.deleteMovie(movie.movieID),
    onSuccess: () => {
      toast.success(`${movie.title} deleted`);
      onDeleted();
    },
    // The refusals above are restated from the server, not enforced by it a
    // second time, so a race (someone else locks the round mid-confirm) lands
    // here — after the destructive confirm, which is the one place a toast is
    // the right report.
    onError: () => toast.error("Failed to delete movie"),
  });

  return (
    <div className="moviemodal__actions">
      <button type="button" className="moviemodal__act" onClick={() => setEditOpen(true)} title="Edit">
        <PencilIcon />
        Edit
      </button>

      {/* Keep the control in place while either lifecycle read is unknown.
          Its authored refusal prevents a stale open default from reaching the
          destructive confirmation. */}
      {isDeletable(movie.status) && (
        <button
          type="button"
          className="moviemodal__act moviemodal__act--danger"
          // Inert in place, never natively disabled: a disabled button can't
          // take focus, and the reason the control won't run is written on the
          // control (see TileAction in UsersTab).
          aria-disabled={refusal ? true : undefined}
          onClick={() => {
            if (refusal) return;
            setDeleteOpen(true);
          }}
          // The row says "Delete"; the reason a refused one won't run rides on
          // the accessible name and the tooltip rather than in the label, which
          // would wrap the word to a second line at the rail's 172px.
          aria-label={label}
          title={label}
        >
          <Trash2Icon />
          Delete
        </button>
      )}

      <EditMovieDialog
        isOpen={open && editOpen}
        onClose={() => setEditOpen(false)}
        initialTitle={movie.title}
        initialLink={movie.link}
        isSaving={editMutation.isPending}
        onSubmit={(payload) => editMutation.mutate({ title: payload.title, link: payload.link })}
      />

      <DeletionDialog
        isOpen={open && deleteOpen}
        pending={deleteMutation.isPending}
        onClose={() => setDeleteOpen(false)}
        onConfirm={() => deleteMutation.mutate()}
        title="Delete movie"
        description={`Delete "${movie.title}"? This can't be undone.`}
      />
    </div>
  );
}

/** A movie's own record: backdrop, a rail of poster + links out, the credits
 *  with the attribution beside them, overview, and the cast strip — and, for
 *  the member who added it, the two actions on the movie itself (see
 *  MovieActions). */
export function MovieModal({
  movie,
  open,
  onRequestClose,
  onClose,
}: {
  movie: MovieTile;
  /** False once the backing history entry is gone, which plays the exit (#196). */
  open: boolean;
  /** Every dismiss gesture goes here, so all four pop the same entry. */
  onRequestClose: () => void;
  onClose: () => void;
}) {
  // The list payloads are lean (no cast/crew/overview/backdrop), so lazy-load the
  // full record on open. `movie` (the tile's lean object) renders instantly while
  // the detail loads, then the enriched fields fill in. SSE enrichment events
  // invalidate this query, so an open modal updates live too.
  const {
    data: detail,
    error: detailError,
    isError: detailIsError,
    isFetching: detailIsFetching,
    isPending,
  } = useQuery(MovieDetailQueryOptions(movie.movieID));
  const detailNotFound =
    (detailError as { status?: unknown } | null)?.status === 404;
  useEffect(() => {
    if (open && detailNotFound) onRequestClose();
  }, [detailNotFound, onRequestClose, open]);
  const m = detail ?? movie;
  // Heavy fields (overview/credits/cast) live only in the detail payload; while
  // it loads, the lean tile object lacks them — show skeletons in their place so
  // the body fills in progressively instead of popping in all at once. A field
  // that's genuinely empty (query settled, not pending) renders nothing rather
  // than a perma-skeleton; cached detail still shows immediately.
  const detailLoading = isPending;
  const recordStateKnown = !detailIsFetching && !detailIsError;

  const { data: me } = useQuery(MeQueryOptions());
  // Both actions are adder-only server-side, so the block belongs to the adder
  // and to nobody else. It waits for the status, which is a detail field: the
  // lean tile object has none, and it is what decides whether delete is offered
  // at all. So the pair arrives with the rest of the detail, the way the
  // credits and the overview do.
  const canAct = detail !== undefined && isSelf(me?.id, detail.addedByID);

  const hue = hueOf(m.title);
  const links = externalLinks(m);
  const cast = detail?.cast ?? [];
  const crew = detail?.crew ?? [];
  const directors = dedupeById(crew.filter((p) => p.job === "Director"));
  const writers = dedupeById(crew.filter((p) => p.job === "Writer" || p.job === "Screenplay"));
  const hasCredits = directors.length > 0 || writers.length > 0;
  // HeroBackdrop always paints the procedural duotone base. This is the photo
  // it adds to the scroll owner's background (real backdrop, else a poster stand-in).
  //
  // The stand-in waits for the detail. `backdropPath` is a detail field, so a
  // lean tile object has a poster and no backdrop for as long as the fetch takes.
  // Reading that as "this movie has no backdrop" puts the poster in the
  // wide hero for a moment, then swaps it for the real backdrop. The duotone
  // holds instead, and the poster only stands in once we know there is nothing
  // else coming.
  //
  // The dark overlay mutes detail in the stand-in, so w185 provides enough
  // resolution without fetching a larger poster for a decorative background.
  const heroBackdrop = detail?.backdropPath ? backdropUrl(detail.backdropPath) : null;
  const heroStandIn =
    heroBackdrop || detailLoading || !m.posterPath ? null : posterUrl(m.posterPath, "w185");
  const heroSrc = heroBackdrop ?? heroStandIn;

  return (
    // Capped (#177): the surface caps at the window height and scrolls inside
    // itself, so a long record centers in the window instead of dragging the
    // blurred page with it, and the close X — pinned to the surface, outside
    // `.modal__scroll` — stays put while the hero scrolls under it.
    <Modal
      label={m.title}
      onClose={onClose}
      open={open}
      onRequestClose={onRequestClose}
      className="modal--movie"
      capped
    >
      {(close) => (
        <>
          <button type="button" className="iconbtn moviemodal__close" onClick={close} aria-label="Close">
            <XIcon />
          </button>

          <HeroBackdrop
            hue={hue}
            src={heroSrc}
            pending={detailLoading}
            wash={heroStandIn !== null}
          >
            <div className="moviemodal__body">
              {/* The rail: identity, then the links out as reference material
                  attached to the movie — quiet mono lines, not three buttons. */}
              <div className="moviemodal__rail">
                <Poster
                  title={m.title}
                  hue={hue}
                  posterPath={m.posterPath}
                  showTitle={!m.posterPath}
                />

                {/* `display: contents` in the rail's column, so the links and the
                    actions stack under the poster as if this weren't here. It
                    exists for the narrow layout, where the rail is a row and the
                    two blocks go side by side: the wrapper is what bottom-aligns
                    them to the poster together, so the actions start on the first
                    link instead of on their own bottom edge. */}
                <div className="moviemodal__railfoot">
                  {links.length > 0 && (
                    <div className="moviemodal__links">
                      {links.map((link) => (
                        <a key={link.label} href={link.href} target="_blank" rel="noopener noreferrer">
                          <ExternalLinkIcon />
                          {link.label}
                        </a>
                      ))}
                    </div>
                  )}

                  {detail && canAct && (
                    <MovieActions
                      movie={detail}
                      open={open}
                      onDeleted={onRequestClose}
                      recordStateKnown={recordStateKnown}
                    />
                  )}
                </div>
              </div>

              <div className="moviemodal__info">
                <h3>{m.title}</h3>
                {/* The chips navigate over the modal's own history entry, which
                    is what closes it: on /stats that's a same-route search
                    change, so the surface stays mounted and animates out over
                    the freshly-filtered view (see MetaChips). */}
                <MetaChips movie={m} replace />

                {/* "Directed by" and "Added by" are the same kind of line — who
                    is responsible for this — so they read as one block split by
                    a rule, instead of the attribution trailing the overview
                    where it belonged to nothing. */}
                <div className="moviemodal__credit">
                  {(hasCredits || detailLoading) && (
                    <div className="moviemodal__credits">
                      {directors.length > 0 && (
                        <span>
                          Directed by <PersonLinks people={directors} />
                        </span>
                      )}
                      {writers.length > 0 && (
                        <span>
                          Written by <PersonLinks people={writers} />
                        </span>
                      )}
                      {/* Credits arrive with the lazy detail, so reserve the rows
                          still missing at full line height — the way the overview
                          and the cast strip already do — instead of letting the
                          block grow under the reader when they land. */}
                      {directors.length === 0 && <GhostCreditRow w={186} />}
                      {writers.length === 0 && <GhostCreditRow w={150} />}
                    </div>
                  )}

                  <div className="moviemodal__credits moviemodal__by">
                    <span>
                      {/* The way from a movie to whoever stashed it (#238), at
                          the address the rail established. Replace, like the
                          chips above and for the same reason: the entry it
                          leaves is the modal's own, and a push would return to
                          an entry whose page renders no modal at all. Archived
                          adders keep their credit but have no active board, so
                          their name stays plain text. */}
                      Added by{" "}
                      {m.addedByArchived ? (
                        <span className="moviemodal__person">{m.addedByName}</span>
                      ) : (
                        <Link
                          to="/users"
                          search={{ member: m.addedByID }}
                          className="moviemodal__person"
                          title={`See ${possessive(m.addedByName)} board`}
                          replace
                        >
                          {m.addedByName}
                        </Link>
                      )}
                      {m.addedAt && ` · ${fullDate(m.addedAt)}`}
                    </span>
                    {m.watchedAt && <span>Watched {fullDate(m.watchedAt)}</span>}
                  </div>
                </div>

                {detail?.tagline && <p className="moviemodal__tag">"{detail.tagline}"</p>}
                {detail?.overview ? (
                  <p className="moviemodal__overview">{detail.overview}</p>
                ) : detailLoading ? (
                  <div className="moviemodal__overview" aria-hidden="true">
                    <SkeletonText w="100%" />
                    <SkeletonText w="100%" style={{ marginTop: 7 }} />
                    <SkeletonText w="62%" style={{ marginTop: 7 }} />
                  </div>
                ) : null}
              </div>
            </div>

            {cast.length > 0 ? (
              <div className="castrow">
                {cast.map((p) => (
                  <a
                    className="castcard"
                    key={p.id}
                    href={tmdbPersonUrl(p.id)}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <div className="castcard__photo">
                      {/* Avatar carries the photo so a dead profile_path falls
                          back to the initials instead of a broken image. */}
                      <Avatar name={p.name} src={profileUrl(p.profilePath)} />
                    </div>
                    <span className="castcard__caption">
                      <span className="castcard__name">{p.name}</span>
                      {p.character && <span className="castcard__role">{p.character}</span>}
                    </span>
                  </a>
                ))}
              </div>
            ) : detailLoading ? (
              <div className="castrow" aria-hidden="true">
                {Array.from({ length: 9 }).map((_, i) => (
                  <div className="castcard" key={i}>
                    {/* The 2:3 frame already; `skel` just adds the shimmer sweep. */}
                    <div className="castcard__photo skel" />
                    <span className="castcard__caption">
                      <SkeletonText w="80%" h={11} />
                      <SkeletonText w="55%" h={11} />
                    </span>
                  </div>
                ))}
              </div>
            ) : null}
          </HeroBackdrop>
        </>
      )}
    </Modal>
  );
}
