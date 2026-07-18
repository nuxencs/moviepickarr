import type { DayRange } from "@/components/moviepickarr/DateRange";
import type {
  FilterOptions,
  MovieFilters,
  PersonFilter,
  PersonOption,
} from "@/components/moviepickarr/lib";

import type { StatsWindow } from "@/types/Response";

/** Windows reachable from the stats UI. The StatsWindow type also allows
 *  24h/90d, but no preset renders them — unknown values fall back to 30d. */
const WINDOWS: StatsWindow[] = ["7d", "30d", "1y", "all-time", "custom"];
const DEFAULT_WINDOW: StatsWindow = "30d";

/** Mirrors FilterBar's MAX_SELECTED / the backend statsMaxPeopleFilterIDs cap;
 *  longer id lists are rejected server-side, so the URL is capped on parse too. */
const MAX_PEOPLE = 25;

/**
 * The entire Stats view, encoded in the URL so every filter is shareable,
 * deep-linkable, and survives reload + back/forward. Sentinel "empty" values
 * ("" / 0 / []) are stripped from the URL by stripSearchParams, so the default
 * view stays a clean `/stats`.
 */
export interface StatsSearch {
  win: StatsWindow;
  /** Custom-range bounds (local YYYY-MM-DD); only used when win === "custom". */
  start: string;
  end: string;
  genre: string;
  actors: number[];
  crew: number[];
  adders: number[];
  year: number;
  decade: number;
}

/**
 * The wire-facing stats filter selection: exactly what GET /stats filters by,
 * in id form. ONE value travels from the URL search through the query key to
 * the request (see StatsGetQueryOptions, whose canonical serializer feeds
 * both). Adding a filter dimension means extending this type, the codec
 * here, the chip, and the wire field, instead of threading a new positional
 * parameter through every layer.
 */
export interface StatsFilters {
  genre?: string;
  actorIds?: number[];
  crewIds?: number[];
  addedByIds?: number[];
  releaseYear?: number;
  /** Decade floor (1990 ⇒ 1990–1999); mutually exclusive with releaseYear. */
  decade?: number;
}

/** Project the URL search onto the wire filter value (empty → undefined). */
export function statsFiltersFromSearch(search: StatsSearch): StatsFilters {
  return {
    genre: search.genre || undefined,
    actorIds: search.actors,
    crewIds: search.crew,
    addedByIds: search.adders,
    releaseYear: search.year || undefined,
    decade: search.decade || undefined,
  };
}

/** Default value of every param — also the strip-from-URL target (router.tsx). */
export const statsSearchDefaults: StatsSearch = {
  win: DEFAULT_WINDOW,
  start: "",
  end: "",
  genre: "",
  actors: [],
  crew: [],
  adders: [],
  year: 0,
  decade: 0,
};

const posInt = (v: unknown): number => {
  const n = Number(v);
  return Number.isInteger(n) && n > 0 ? n : 0;
};

/** Coerce a search value (a parsed array, or a comma string from a hand-edited
 *  URL) into a sorted, de-duplicated, capped id list — matching idListKey's
 *  canonical form so equivalent filters share one cache entry + history URL. */
function idList(v: unknown): number[] {
  const raw = Array.isArray(v) ? v : typeof v === "string" && v ? v.split(",") : [];
  const ids = raw.map(Number).filter((n) => Number.isInteger(n) && n > 0);
  return [...new Set(ids)].sort((a, b) => a - b).slice(0, MAX_PEOPLE);
}

/**
 * Total, never-throwing validator — TanStack Router runs it on every navigation
 * and its return type is the route's typed search. Always returns a full object
 * (with defaults) so deep links and empty URLs resolve cleanly.
 */
export function validateStatsSearch(search: Record<string, unknown>): StatsSearch {
  const win = WINDOWS.includes(search.win as StatsWindow)
    ? (search.win as StatsWindow)
    : DEFAULT_WINDOW;
  // year and decade are mutually exclusive (the UI invariant); if a crafted URL
  // sets both, keep the exact year and drop the decade.
  const year = posInt(search.year);
  const decade = year ? 0 : posInt(search.decade);
  return {
    win,
    start: typeof search.start === "string" ? search.start : "",
    end: typeof search.end === "string" ? search.end : "",
    genre: typeof search.genre === "string" ? search.genre : "",
    actors: idList(search.actors),
    crew: idList(search.crew),
    adders: idList(search.adders),
    year,
    decade,
  };
}

/** Local-date YYYY-MM-DD (NOT toISOString, which shifts the day across timezones). */
export function ymd(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

const YMD = /^\d{4}-\d{2}-\d{2}$/;

/** Parse a local YYYY-MM-DD back to local midnight; null if malformed. Uses the
 *  numeric constructor (not new Date(str), which parses as UTC). */
function parseYmd(s: string): Date | null {
  if (!YMD.test(s)) return null;
  const [y, m, d] = s.split("-").map(Number);
  const date = new Date(y, m - 1, d);
  return date.getFullYear() === y && date.getMonth() === m - 1 && date.getDate() === d
    ? date
    : null;
}

/** The custom day-range the URL carries, or null when unset/incomplete. */
export function rangeFromSearch(search: StatsSearch): DayRange | null {
  const start = parseYmd(search.start);
  const end = parseYmd(search.end);
  return start && end ? { start, end } : null;
}

/** Resolve id lists to {id, name} chips using the cached watched-derived
 *  options; falls back to the bare id until the watched list loads. */
function people(ids: number[], options: PersonOption[]): PersonFilter[] {
  return ids.map((id) => ({
    id,
    name: options.find((o) => o.id === id)?.name ?? String(id),
  }));
}

/** Build the MovieFilters the FilterBar + rails render from the URL search. */
export function filtersFromSearch(search: StatsSearch, options: FilterOptions): MovieFilters {
  return {
    genre: search.genre || null,
    year: search.year || null,
    decade: search.decade || null,
    actors: people(search.actors, options.actors),
    crew: people(search.crew, options.crew),
    adders: people(search.adders, options.adders),
  };
}

/** Serialize a FilterBar MovieFilters change back into URL search params. */
export function filtersToSearch(
  f: MovieFilters,
): Pick<StatsSearch, "genre" | "actors" | "crew" | "adders" | "year" | "decade"> {
  return {
    genre: f.genre ?? "",
    actors: f.actors.map((p) => p.id),
    crew: f.crew.map((p) => p.id),
    adders: f.adders.map((p) => p.id),
    year: f.year ?? 0,
    decade: f.decade ?? 0,
  };
}
