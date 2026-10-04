import type { DayRange } from "@/components/moviepickarr/DateRange";
import type {
  FilterOptions,
  MovieFilters,
  PersonFilter,
  PersonOption,
} from "@/components/moviepickarr/lib";

import type { StatsWindow } from "@/types/Response";

/** Windows the stats UI renders; StatsWindow also allows 24h and 90d. */
const WINDOWS: StatsWindow[] = ["7d", "30d", "1y", "all-time", "custom"];
const DEFAULT_WINDOW: StatsWindow = "30d";

/** In step with FilterBar MAX_SELECTED and backend statsMaxPeopleFilterIDs. */
const MAX_PEOPLE = 25;

/**
 * The entire Stats view as URL search. Empty sentinels ("", 0, []) are
 * stripped by stripSearchParams, so the default view stays `/stats`.
 */
export interface StatsSearch {
  win: StatsWindow;
  /** Local YYYY-MM-DD, read only when win is "custom". */
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
 * What GET /stats filters by. One value travels from the URL through the
 * query key to the request (see StatsGetQueryOptions).
 */
export interface StatsFilters {
  genre?: string;
  actorIds?: number[];
  crewIds?: number[];
  addedByIds?: number[];
  releaseYear?: number;
  /** Decade floor year; mutually exclusive with releaseYear. */
  decade?: number;
}

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

/** Also the stripSearchParams target in router.tsx. */
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

/** Sorted, de-duplicated, capped: idListKey's canonical form, so equal
 *  filters share one cache entry and URL. Accepts a comma string too. */
function idList(v: unknown): number[] {
  const raw = Array.isArray(v) ? v : typeof v === "string" && v ? v.split(",") : [];
  const ids = raw.map(Number).filter((n) => Number.isInteger(n) && n > 0);
  return [...new Set(ids)].sort((a, b) => a - b).slice(0, MAX_PEOPLE);
}

/** Total, never-throwing validator for the route's search. */
export function validateStatsSearch(search: Record<string, unknown>): StatsSearch {
  const win = WINDOWS.includes(search.win as StatsWindow)
    ? (search.win as StatsWindow)
    : DEFAULT_WINDOW;
  // A crafted URL can set both year and decade: the exact year wins.
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

/** Local-date YYYY-MM-DD; toISOString would shift the day across timezones. */
export function ymd(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

const YMD = /^\d{4}-\d{2}-\d{2}$/;

/** Local midnight, or null if malformed. new Date(str) would parse as UTC. */
function parseYmd(s: string): Date | null {
  if (!YMD.test(s)) return null;
  const [y, m, d] = s.split("-").map(Number);
  const date = new Date(y, m - 1, d);
  return date.getFullYear() === y && date.getMonth() === m - 1 && date.getDate() === d
    ? date
    : null;
}

export function rangeFromSearch(search: StatsSearch): DayRange | null {
  const start = parseYmd(search.start);
  const end = parseYmd(search.end);
  return start && end ? { start, end } : null;
}

/** Falls back to the bare id as name until the options load. */
function people(ids: number[], options: PersonOption[]): PersonFilter[] {
  return ids.map((id) => ({
    id,
    name: options.find((o) => o.id === id)?.name ?? String(id),
  }));
}

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
