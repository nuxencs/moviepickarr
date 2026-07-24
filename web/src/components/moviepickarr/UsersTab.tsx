import { useMutation, useQuery } from "@tanstack/react-query";
import {
  LinkIcon,
  MoveDownIcon,
  MoveUpIcon,
  PencilIcon,
  PlusIcon,
  SearchIcon,
  Trash2Icon,
} from "lucide-react";
import { memo, useMemo, useState, useSyncExternalStore } from "react";

import { APIClient } from "@/api/APIClient";
import { MeQueryOptions, SettingsGetPoolLockQueryOptions, UsersGetAllQueryOptions } from "@/api/queries";

import { EditMovieDialog } from "@/components/EditMovieDialog";
import { Avatar } from "@/components/moviepickarr/Bits";
import { drawStore } from "@/components/moviepickarr/drawStore";
import { hueOf, plural } from "@/components/moviepickarr/lib";
import { Menu } from "@/components/moviepickarr/Menu";
import { isSelf } from "@/components/moviepickarr/ownership";
import { possessive } from "@/components/moviepickarr/possessive";
import { Poster } from "@/components/moviepickarr/Poster";
import { SearchModal } from "@/components/moviepickarr/SearchModal";
import { UsersBodySkeleton } from "@/components/moviepickarr/Skeletons";
import { DeletionDialog } from "@/components/ui/deletion-dialog";
import { toast } from "@/components/ui/toast-api";

import type { Movie, User } from "@/types/Response";

import { useToggle } from "@/hooks/hooks";

const POOL_SIZE = 3;

export function UsersTab() {
  const { data: users, isPending: usersPending, isError: usersError } = useQuery(UsersGetAllQueryOptions());
  // The session member drives the board's self-service gating via isSelf (see
  // ownership.ts): movie actions show only on your own board. Member onboarding
  // and removal live on the admin roster, so this page has no add-member form
  // and no delete action.
  const { data: me } = useQuery(MeQueryOptions());
  const [searchUser, setSearchUser] = useState<User | null>(null);

  return (
    <>
      <div className="block">
        <div className="sec-head">
          <div className="sec-title">
            <h2>Members</h2>
            <span className="sec-count">{plural(users?.length ?? 0, "person", "people")}</span>
          </div>
        </div>

        <div className="boards">
          {usersError ? (
            <p className="empty text-destructive">Failed to load members.</p>
          ) : usersPending ? (
            <UsersBodySkeleton />
          ) : users && users.length > 0 ? (
            users.map((user) => (
              <Board
                key={user.userID}
                user={user}
                isOwnBoard={isSelf(me?.id, user.userID)}
                onOpenSearch={() => setSearchUser(user)}
              />
            ))
          ) : (
            <p className="empty">No members yet</p>
          )}
        </div>
      </div>

      {searchUser && (
        <SearchModal userName={searchUser.name} onClose={() => setSearchUser(null)} />
      )}
    </>
  );
}

function Board({
  user,
  isOwnBoard,
  onOpenSearch,
}: {
  user: User;
  isOwnBoard: boolean;
  onOpenSearch: () => void;
}) {
  const { data: isLocked } = useQuery(SettingsGetPoolLockQueryOptions());
  const [filter, setFilter] = useState("");

  const pool = useMemo(
    () => Object.values(user.currentPool).sort((a, b) => a.title.localeCompare(b.title)),
    [user.currentPool],
  );
  const stash = useMemo(
    () => Object.values(user.stash).sort((a, b) => a.title.localeCompare(b.title)),
    [user.stash],
  );
  const filteredStash = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return q ? stash.filter((m) => m.title.toLowerCase().includes(q)) : stash;
  }, [stash, filter]);

  const filled = pool.length;
  const poolFull = filled >= POOL_SIZE;
  const firstName = user.name.split(" ")[0];

  // Demote a pooled movie back to the stash. The move endpoint is directional
  // (target = destination) and idempotent, so a repeat click is a safe no-op.
  // Gated on isOwnBoard, so it only renders on your own board.
  const demoteMutation = useMutation({
    mutationFn: (movieID: number) => APIClient.board.moveMovie(movieID, "stash"),
    onError: () => toast.error("Failed to move movie"),
  });

  // The pool is frozen server-side while a draw is unrevealed: the drawn movie
  // is still shown as a pool tile, so demoting any of them has to be refused
  // (letting one tile through would say which movie was drawn). Match that here
  // instead of letting the click come back as a failed move.
  const drawInFlight = useSyncExternalStore(
    drawStore.subscribe,
    () => drawStore.getState().phase !== "idle",
  );

  return (
    <div className="board">
      <div className="board__head">
        <div className="board__id">
          <Avatar name={user.name} size={38} />
          <div>
            <div className="nm">{user.name}</div>
            <div className="ct">
              {stash.length} in stash · {filled}/{POOL_SIZE} pooled
            </div>
          </div>
        </div>
      </div>

      {isOwnBoard && (
        <button type="button" className="btn btn--ghost board__search" onClick={onOpenSearch}>
          <PlusIcon />
          Add to {possessive(firstName)} stash
        </button>
      )}

      <div className="poolbox">
        <div className="poolbox__label">
          <span className="l">Pool</span>
          <span className="l" style={{ color: poolFull ? "var(--accent)" : "var(--ink-3)" }}>
            {filled}/{POOL_SIZE}
          </span>
        </div>
        <div className="pool-slots">
          {Array.from({ length: POOL_SIZE }).map((_, i) => {
            const movie = pool[i];
            return movie ? (
              <div className="pslot pslot--filled" key={movie.movieID} title={movie.title}>
                <Poster title={movie.title} hue={hueOf(movie.title)} posterPath={movie.posterPath} showTitle={false} />
                {isOwnBoard && !isLocked && (
                  <button
                    type="button"
                    className="pslot__demote"
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
      </div>

      <div className="stash">
        <div className="stash__head">
          <h3>
            Stash <span className="sec-count">{stash.length}</span>
          </h3>
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
        <div className="stash__list">
          {filteredStash.length === 0 ? (
            <div className="empty">
              {filter ? `Nothing matches "${filter}"` : "Stash is empty"}
            </div>
          ) : (
            filteredStash.map((movie) => (
              <StashRow
                key={movie.movieID}
                movie={movie}
                poolFull={poolFull}
                locked={!!isLocked}
                isOwnBoard={isOwnBoard}
              />
            ))
          )}
        </div>
      </div>
    </div>
  );
}

// Memoized because the stash filter lives in the Board above: without it every
// keystroke re-renders every matching row, and a row is not cheap (three
// mutation hooks, a poster, a menu, two dialogs). The props are the movie object
// straight out of the query cache plus primitives, so the memo holds while typing.
const StashRow = memo(function StashRow({
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
  const [editOpen, toggleEdit] = useToggle(false);
  const [deleteOpen, toggleDelete] = useToggle(false);

  // The row only renders these actions on your own board (isOwnBoard).
  const moveMutation = useMutation({
    mutationFn: () => APIClient.board.moveMovie(movie.movieID, "pool"),
    onError: () => toast.error("Failed to move movie"),
  });
  const deleteMutation = useMutation({
    mutationFn: () => APIClient.board.deleteMovie(movie.movieID),
    onSuccess: () => toast.success(`${movie.title} deleted`),
    onError: () => toast.error("Failed to delete movie"),
  });
  const editMutation = useMutation({
    mutationFn: (payload: { title: string; link: string }) =>
      APIClient.board.updateMovie(movie.movieID, payload.title, payload.link),
    onSuccess: () => {
      toast.success(`${movie.title} updated`);
      toggleEdit();
    },
    onError: () => toast.error("Failed to update movie"),
  });

  return (
    <>
      <EditMovieDialog
        isOpen={editOpen}
        onClose={toggleEdit}
        initialTitle={movie.title}
        initialLink={movie.link}
        isSaving={editMutation.isPending}
        onSubmit={(payload) => editMutation.mutate({ title: payload.title, link: payload.link })}
      />
      <DeletionDialog
        isOpen={deleteOpen}
        onClose={toggleDelete}
        onConfirm={() => deleteMutation.mutate()}
        title="Delete movie"
        description={`Delete ${movie.title}? This cannot be undone.`}
        confirmText="Delete"
      />

      <div className="srow">
        <Poster title={movie.title} hue={hueOf(movie.title)} posterPath={movie.posterPath} showTitle={false} />
        <span className="sr-title">{movie.title}</span>
        <div className="sr-actions">
          {movie.link && (
            <a className="iconbtn" href={movie.link} target="_blank" rel="noopener noreferrer" aria-label="Open link">
              <LinkIcon />
            </a>
          )}
          {isOwnBoard && (
            <>
              <button
                type="button"
                className="iconbtn"
                onClick={() => moveMutation.mutate()}
                disabled={locked || poolFull || moveMutation.isPending}
                aria-label="Promote to pool"
                title={poolFull ? "Pool is full" : locked ? "Pool is locked" : "Promote to pool"}
              >
                <MoveUpIcon />
              </button>
              <Menu
                label="More actions"
                actions={[
                  { icon: <PencilIcon />, label: "Edit", onSelect: toggleEdit },
                  { icon: <Trash2Icon />, label: "Delete", onSelect: toggleDelete, danger: true },
                ]}
              />
            </>
          )}
        </div>
      </div>
    </>
  );
});
