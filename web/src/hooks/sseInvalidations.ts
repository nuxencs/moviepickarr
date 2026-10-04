// SSE event type -> query keys it stales. useSSE dispatch and resyncKeys() both
// read this table, so they cannot drift. Draw events also drive drawStore.

import { AuthKeys, MoviesKeys, SettingsKeys, StatsKeys, UsersKeys } from "@/api/query_keys";

import type { SSEEventType } from "@/types/SSEEvent";

type QueryKey = readonly unknown[];

export const SSE_INVALIDATIONS: Record<SSEEventType, QueryKey[]> = {
  // Adders are Stats filter options, so roster changes stale them.
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
    MoviesKeys.details(),
    StatsKeys.all,
  ],

  // One coalesced event per enrichment burst: every cache that embeds TMDB fields.
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

  // No pool: the server keeps the winner in pool reads until reveal, and
  // drawStore refreshes it when the reel lands.
  "movie:drawn": [
    UsersKeys.list(),
    MoviesKeys.current(),
    SettingsKeys.poolLock(),
    SettingsKeys.nextUp(),
  ],

  // Reveal is when the server drops the winner from the pool. Clients that ran
  // no reel (reduced motion, one candidate) have no land to refresh on.
  "movie:revealed": [
    UsersKeys.list(),
    MoviesKeys.listpool(),
    MoviesKeys.details(),
    SettingsKeys.poolLock(),
  ],

  // Releases the pool and gate too, in case this client missed the reveal.
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

/** The row for one event, or null for an unknown type (a newer server). */
export function invalidationsFor(type: string): QueryKey[] | null {
  return Object.prototype.hasOwnProperty.call(SSE_INVALIDATIONS, type)
    ? SSE_INVALIDATIONS[type as SSEEventType]
    : null;
}

/** Every key any SSE event can stale, deduped: the resync set. */
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
  // Resync must always include the pool, even if the rows change.
  const pool = MoviesKeys.listpool();
  if (!seen.has(JSON.stringify(pool))) keys.push(pool);
  return keys;
}
