import { useMutation, useQuery } from "@tanstack/react-query";
import {
  LinkIcon,
  MoveDownIcon,
  MoveUpIcon,
  PencilIcon,
  PlusIcon,
  SearchIcon,
  Trash2Icon,
  UsersIcon,
} from "lucide-react";
import { FormEvent, useMemo, useState } from "react";

import { APIClient } from "@/api/APIClient";
import { SettingsGetPoolLockQueryOptions, UsersGetAllQueryOptions } from "@/api/queries";

import { EditMovieDialog } from "@/components/EditMovieDialog";
import { Avatar } from "@/components/moviepickarr/Bits";
import { hueOf, plural } from "@/components/moviepickarr/lib";
import { Menu } from "@/components/moviepickarr/Menu";
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
  const [name, setName] = useState("");
  const [searchUser, setSearchUser] = useState<User | null>(null);

  const createMutation = useMutation({
    mutationFn: () => APIClient.users.create(name.trim()),
    onSuccess: () => {
      toast.success(`Member ${name.trim()} created`);
      setName("");
    },
    onError: () => {
      toast.error("Failed to create member");
      setName("");
    },
  });

  const handleCreate = (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim() || createMutation.isPending) return;
    createMutation.mutate();
  };

  return (
    <>
      <div className="block">
        <div className="sec-head">
          <div className="sec-title">
            <h2>Members</h2>
            <span className="sec-count">{plural(users?.length ?? 0, "person", "people")}</span>
          </div>
          <form className="user-add" onSubmit={handleCreate}>
            <label className="field user-add__field">
              <UsersIcon />
              <input
                name="new-user-name"
                aria-label="Add a new member by name"
                placeholder="Add someone…"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={createMutation.isPending}
              />
            </label>
            <button type="submit" className="btn btn--accent" disabled={!name.trim() || createMutation.isPending}>
              <PlusIcon />
              Add member
            </button>
          </form>
        </div>

        <div className="boards">
          {usersError ? (
            <p className="empty text-destructive">Failed to load members.</p>
          ) : usersPending ? (
            <UsersBodySkeleton />
          ) : users && users.length > 0 ? (
            users.map((user) => (
              <Board key={user.userID} user={user} onOpenSearch={() => setSearchUser(user)} />
            ))
          ) : (
            <p className="empty">No members yet</p>
          )}
        </div>
      </div>

      {searchUser && (
        <SearchModal userID={searchUser.userID} userName={searchUser.name} onClose={() => setSearchUser(null)} />
      )}
    </>
  );
}

function Board({ user, onOpenSearch }: { user: User; onOpenSearch: () => void }) {
  const { data: isLocked } = useQuery(SettingsGetPoolLockQueryOptions());
  const [filter, setFilter] = useState("");
  const [deleteOpen, toggleDelete] = useToggle(false);

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

  const deleteUserMutation = useMutation({
    mutationFn: () => APIClient.users.delete(user.userID),
    onSuccess: () => toast.success(`Member ${user.name} deleted`),
    onError: () => toast.error("Failed to delete member"),
  });

  // Demote a pooled movie back to the stash. The move endpoint is directional
  // (target = destination) and idempotent, so a repeat click is a safe no-op.
  const demoteMutation = useMutation({
    mutationFn: (movieID: number) => APIClient.users.moveMovie(user.userID, movieID, "stash"),
    onError: () => toast.error("Failed to move movie"),
  });

  return (
    <div className="board">
      <DeletionDialog
        isOpen={deleteOpen}
        onClose={toggleDelete}
        onConfirm={() => deleteUserMutation.mutate()}
        title="Delete member"
        description={`Delete ${user.name}? This cannot be undone.`}
        confirmText="Delete"
      />

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
        <button type="button" className="iconbtn iconbtn--danger" onClick={toggleDelete} aria-label="Delete member">
          <Trash2Icon />
        </button>
      </div>

      <button type="button" className="btn btn--ghost board__search" onClick={onOpenSearch}>
        <PlusIcon />
        Add to {firstName}'s stash
      </button>

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
                {!isLocked && (
                  <button
                    type="button"
                    className="pslot__demote"
                    onClick={() => demoteMutation.mutate(movie.movieID)}
                    disabled={demoteMutation.isPending}
                    aria-label="Move back to stash"
                    title="Move back to stash"
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
              aria-label={`Search ${firstName}'s stash`}
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
              <StashRow key={movie.movieID} user={user} movie={movie} poolFull={poolFull} locked={!!isLocked} />
            ))
          )}
        </div>
      </div>
    </div>
  );
}

function StashRow({
  user,
  movie,
  poolFull,
  locked,
}: {
  user: User;
  movie: Movie;
  poolFull: boolean;
  locked: boolean;
}) {
  const [editOpen, toggleEdit] = useToggle(false);
  const [deleteOpen, toggleDelete] = useToggle(false);

  const moveMutation = useMutation({
    mutationFn: () => APIClient.users.moveMovie(user.userID, movie.movieID, "pool"),
    onError: () => toast.error("Failed to move movie"),
  });
  const deleteMutation = useMutation({
    mutationFn: () => APIClient.users.deleteMovie(user.userID, movie.movieID),
    onSuccess: () => toast.success(`${movie.title} deleted`),
    onError: () => toast.error("Failed to delete movie"),
  });
  const editMutation = useMutation({
    mutationFn: (payload: { title: string; link: string }) =>
      APIClient.users.updateMovie(user.userID, movie.movieID, payload.title, payload.link),
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
        </div>
      </div>
    </>
  );
}
