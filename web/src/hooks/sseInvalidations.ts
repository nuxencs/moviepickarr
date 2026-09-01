/* ============================================================
   moviepickarr: the SSE invalidation table.

   ONE declarative map from every SSE event type to the query keys it stales.
   Both consumers read it, so the two can't drift apart anymore:

   - the per-event dispatch in useSSE walks the row for the event, and
   - resyncKeys() (the reconnect/refocus catch-up) is derived as the union of
     every row: resync re-pulls exactly what some missed event could have
     staled, by construction.

   Typed as Record<SSEEventType, …>, so adding an event type without deciding
   its invalidations is a compile error. Draw events additionally drive the
   draw machine (see useSSE); their rows here carry only the cache side. The
   pool key is the one special case: the server already keeps the winner in
   every pool read until reveal, so a draw does not stale the visible list.
   The draw machine still owns its land-time refresh for clients already
   animating, which is why "movie:drawn" does not list the pool here.
   ============================================================ */

import { AuthKeys, MoviesKeys, SettingsKeys, StatsKeys, UsersKeys } from "@/api/query_keys";

import type { SSEEventType } from "@/types/SSEEvent";

type QueryKey = readonly unknown[];

export const SSE_INVALIDATIONS: Record<SSEEventType, QueryKey[]> = {
  // Adders are part of the Stats filter options, so roster changes stale them.
  // The admin roster also lists every member, so it stales on create/delete too
  // (a second admin's create/remove/archive shows up without a manual refetch).
  "user:created": [
    UsersKeys.list(),
    UsersKeys.roster(),
    MoviesKeys.listpool(),
    MoviesKeys.current(),
    MoviesKeys.listwatched(),
    MoviesKeys.details(),
    MoviesKeys.filterOptions(),
    SettingsKeys.nextUp(),
    StatsKeys.all,
  ],
  "user:deleted": [
    UsersKeys.list(),
    UsersKeys.roster(),
    MoviesKeys.listpool(),
    MoviesKeys.current(),
    MoviesKeys.listwatched(),
    MoviesKeys.details(),
    MoviesKeys.filterOptions(),
    SettingsKeys.nextUp(),
    StatsKeys.all,
  ],
  "user:role-changed": [
    AuthKeys.me(),
    UsersKeys.list(),
    UsersKeys.roster(),
    SettingsKeys.nextUp(),
  ],

  "movie:added": [UsersKeys.list(), MoviesKeys.listpool()],
  "movie:deleted": [UsersKeys.list(), MoviesKeys.listpool(), MoviesKeys.details()],
  "movie:moved": [UsersKeys.list(), MoviesKeys.listpool(), MoviesKeys.details()],

  "movie:updated": [
    UsersKeys.list(),
    MoviesKeys.listpool(),
    MoviesKeys.current(),
    MoviesKeys.listwatched(),
    // Refresh any open detail modal for the edited movie.
    MoviesKeys.details(),
    StatsKeys.all,
  ],

  // One coalesced event per enrichment burst. Refresh every cache that embeds
  // enriched fields: the boards render posters (users), Stats aggregate TMDB
  // metadata (genres, people, runtime, rating), and the modal + filter
  // options read credits.
  "movies:enriched-batch": [
    UsersKeys.list(),
    MoviesKeys.listpool(),
    MoviesKeys.current(),
    MoviesKeys.wildcard(),
    MoviesKeys.listwatched(),
    MoviesKeys.details(),
    MoviesKeys.filterOptions(),
    StatsKeys.all,
  ],

  // The pool is deliberately absent: the draw machine holds its refresh until
  // the reel lands (see the header comment). Pool state is separate from that
  // animation: reduced-motion and one-candidate clients still need the
  // server-owned mutation gate immediately.
  "movie:drawn": [
    UsersKeys.list(),
    MoviesKeys.current(),
    SettingsKeys.poolLock(),
    SettingsKeys.nextUp(),
  ],

  // The reveal is when the server stops holding the winner in the pool (it
  // hands the drawn movie back in every pool read until then, so a missing tile
  // can't spoil the reel). Clients that never ran a reel — reduced motion, a
  // lone candidate — have no land to refresh on, and a local confirm can race
  // its own POST, so the broadcast is the one moment every client can trust.
  "movie:revealed": [
    UsersKeys.list(),
    MoviesKeys.listpool(),
    MoviesKeys.details(),
    SettingsKeys.poolLock(),
  ],

  // Watching is terminal even if this client missed the preceding reveal
  // frame, so this row also releases every pool projection and mutation gate.
  "movie:watched": [
    UsersKeys.list(),
    MoviesKeys.listpool(),
    MoviesKeys.current(),
    MoviesKeys.listwatched(),
    MoviesKeys.details(),
    MoviesKeys.filterOptions(),
    SettingsKeys.poolLock(),
    StatsKeys.all,
  ],

  "wildcard:selected": [
    UsersKeys.list(),
    MoviesKeys.listpool(),
    MoviesKeys.wildcard(),
    MoviesKeys.details(),
  ],
  "wildcard:canceled": [
    UsersKeys.list(),
    MoviesKeys.listpool(),
    MoviesKeys.wildcard(),
    MoviesKeys.details(),
  ],
  "wildcard:watched": [
    UsersKeys.list(),
    MoviesKeys.listpool(),
    MoviesKeys.wildcard(),
    MoviesKeys.listwatched(),
    MoviesKeys.details(),
    MoviesKeys.filterOptions(),
    StatsKeys.all,
  ],

  "settings:pool-lock-changed": [SettingsKeys.poolLock()],
  "settings:next-up-changed": [SettingsKeys.nextUp()],
};

/** The rows for one event, or null for an unknown type (a server ahead of
 *  this client), the caller logs it. */
export function invalidationsFor(type: string): QueryKey[] | null {
  return Object.prototype.hasOwnProperty.call(SSE_INVALIDATIONS, type)
    ? SSE_INVALIDATIONS[type as SSEEventType]
    : null;
}

/** Every key any SSE event can stale, deduped: the resync set. A reconnect
 *  can't know which events it missed, so it re-pulls the union. */
export function resyncKeys(): QueryKey[] {
  const seen = new Set<string>();
  const keys: QueryKey[] = [];
  for (const row of Object.values(SSE_INVALIDATIONS)) {
    for (const key of row) {
      const id = JSON.stringify(key);
      if (!seen.has(id)) {
        seen.add(id);
        keys.push(key);
      }
    }
  }
  // The pool rides in several rows already, but keep the derivation honest if
  // rows change: resync must always include it (guarded by the caller while a
  // reel is in flight).
  const pool = MoviesKeys.listpool();
  if (!seen.has(JSON.stringify(pool))) keys.push(pool);
  return keys;
}
