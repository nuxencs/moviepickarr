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
import {
  MovieCastScrollbar,
  MovieScrollbar,
} from "@/components/moviepickarr/MovieScrollbar";
import { isSelf } from "@/components/moviepickarr/ownership";
import { possessive } from "@/components/moviepickarr/possessive";
import { Poster } from "@/components/moviepickarr/Poster";
import { deleteLabel, deleteRefusalOf, isDeletable } from "@/components/moviepickarr/refusals";
import { SkeletonText } from "@/components/moviepickarr/Skeletons";
import { DeletionDialog } from "@/components/ui/deletion-dialog";
import { toast } from "@/components/ui/toast-api";

import type { CreditPerson, MovieDetail, MovieTile } from "@/types/Response";

/** A writer credited for Writer and Screenplay shows once. */
function dedupeById(people: CreditPerson[]): CreditPerson[] {
  const seen = new Set<number>();
  return people.filter((p) => {
    if (seen.has(p.id)) return false;
    seen.add(p.id);
    return true;
  });
}

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

/** Holds a full line height, so a landing credit does not shift the row. */
function GhostCreditRow({ w }: { w: number }) {
  return (
    <span className="moviemodal__credits__ghost" aria-hidden="true">
      <SkeletonText w={w} h={12} />
    </span>
  );
}

/** Modal hero backdrop. The duotone paints first so a slow TMDB fetch cannot
 *  flash white. The layer spans the scroll owner so the overlay scrollbar
 *  reserves no empty strip. */
function HeroBackdrop({
  hue,
  src,
  /** True while `backdropPath` is in flight: hold the duotone, not a stand-in. */
  pending,
  /** `src` is a poster stand-in: darken it so the rail poster does not repeat. */
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
  const scrollRef = useRef<HTMLDivElement>(null);

  // Read a cached image's `complete` before paint so a reopen does not re-flash.
  useLayoutEffect(() => {
    const img = imgRef.current;
    setFailed(false);
    setLoaded(Boolean(img?.complete && img.naturalWidth > 0));
  }, [src]);

  const url = failed ? null : src;
  const loading = pending || (url !== null && !loaded);
  // A failed stand-in shows the duotone, which takes the normal scrim.
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
  // 1px overlap: WebKit and Gecko can round the shared edge differently.
  const fadeHeight = "calc(var(--moviemodal-hero-height) + 1px)";
  const backgroundSize = photograph
    ? washing
      ? `100% 100%, 100% ${fadeHeight}, 100% var(--moviemodal-hero-height), 100% var(--moviemodal-hero-height), 100% auto`
      : `100% 100%, 100% ${fadeHeight}, 100% var(--moviemodal-hero-height), 100% auto`
    : `100% 100%, 100% ${fadeHeight}, 100% var(--moviemodal-hero-height), 100% var(--moviemodal-hero-height), 100% var(--moviemodal-hero-height), 100% var(--moviemodal-hero-height)`;

  return (
    <MovieScrollbar viewportRef={scrollRef}>
      <div ref={scrollRef} className="modal__scroll moviemodal__scroll">
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
    </MovieScrollbar>
  );
}

/**
 * Rename and delete, adder-only. Drawn from the movie in hand, not a prop, so
 * every surface that opens the modal offers the same actions (#237).
 */
function MovieActions({
  movie,
  /** Child dialogs mount only while this holds, so browser Back closes both. */
  open,
  /** The same path as every dismissal, so the history entry pops once. */
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

  // Only a pool movie's delete reads the pool gates.
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
    // A race with the server's own refusal (a lock mid-confirm) lands here.
    onError: () => toast.error("Failed to delete movie"),
  });

  return (
    <div className="moviemodal__actions">
      <button type="button" className="moviemodal__act" onClick={() => setEditOpen(true)} title="Edit">
        <PencilIcon />
        Edit
      </button>

      {/* Stays while a lifecycle read is unknown; its refusal blocks the confirm. */}
      {isDeletable(movie.status) && (
        <button
          type="button"
          className="moviemodal__act moviemodal__act--danger"
          // Not `disabled`: it must stay focusable to speak its reason.
          aria-disabled={refusal ? true : undefined}
          onClick={() => {
            if (refusal) return;
            setDeleteOpen(true);
          }}
          // Reason not in the visible label: it would wrap in the 172px rail.
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

export function MovieModal({
  movie,
  open,
  onRequestClose,
  onClose,
}: {
  movie: MovieTile;
  /** False once the backing history entry is gone, which plays the exit (#196). */
  open: boolean;
  onRequestClose: () => void;
  onClose: () => void;
}) {
  // List payloads are lean: load the full record; `movie` renders meanwhile.
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
  // Skeletons only while pending: a settled empty field renders nothing.
  const detailLoading = isPending;
  const recordStateKnown = !detailIsFetching && !detailIsError;

  const { data: me } = useQuery(MeQueryOptions());
  // Waits for the detail: the lean tile has no status, which decides Delete.
  const canAct = detail !== undefined && isSelf(me?.id, detail.addedByID);

  const hue = hueOf(m.title);
  const links = externalLinks(m);
  const cast = detail?.cast ?? [];
  const crew = detail?.crew ?? [];
  const directors = dedupeById(crew.filter((p) => p.job === "Director"));
  const writers = dedupeById(crew.filter((p) => p.job === "Writer" || p.job === "Screenplay"));
  const hasCredits = directors.length > 0 || writers.length > 0;
  // The poster stands in only once the detail confirms no backdrop; else it
  // would flash before the real one. w185 is enough under the dark wash.
  const heroBackdrop = detail?.backdropPath ? backdropUrl(detail.backdropPath) : null;
  const heroStandIn =
    heroBackdrop || detailLoading || !m.posterPath ? null : posterUrl(m.posterPath, "w185");
  const heroSrc = heroBackdrop ?? heroStandIn;

  return (
    // Capped (#177): the close X stays put while the hero scrolls under it.
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
              <div className="moviemodal__rail">
                <Poster
                  title={m.title}
                  hue={hue}
                  posterPath={m.posterPath}
                  showTitle={!m.posterPath}
                />

                {/* `display: contents` in the column layout; in the narrow row
                    layout it bottom-aligns links and actions to the poster together. */}
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
                {/* Replacing the modal's history entry closes it (see MetaChips). */}
                <MetaChips movie={m} replace />

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
                      {directors.length === 0 && <GhostCreditRow w={186} />}
                      {writers.length === 0 && <GhostCreditRow w={150} />}
                    </div>
                  )}

                  <div className="moviemodal__credits moviemodal__by">
                    <span>
                      {/* Replace, as the chips do (#238): a push would leave Back
                          on an entry with no modal. Archived adders have no
                          board to link. */}
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
              <MovieCastScrollbar>
                {cast.map((p) => (
                  <a
                    className="castcard"
                    key={p.id}
                    href={tmdbPersonUrl(p.id)}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <div className="castcard__photo">
                      {/* Avatar falls back to initials on a dead profile_path. */}
                      <Avatar name={p.name} src={profileUrl(p.profilePath)} />
                    </div>
                    <span className="castcard__caption">
                      <span className="castcard__name">{p.name}</span>
                      {p.character && <span className="castcard__role">{p.character}</span>}
                    </span>
                  </a>
                ))}
              </MovieCastScrollbar>
            ) : detailLoading ? (
              <MovieCastScrollbar hiddenFromAccessibility>
                {Array.from({ length: 9 }).map((_, i) => (
                  <div className="castcard" key={i}>
                    <div className="castcard__photo skel" />
                    <span className="castcard__caption">
                      <SkeletonText w="80%" h={11} />
                      <SkeletonText w="55%" h={11} />
                    </span>
                  </div>
                ))}
              </MovieCastScrollbar>
            ) : null}
          </HeroBackdrop>
        </>
      )}
    </Modal>
  );
}
