import { useQuery } from "@tanstack/react-query";
import {
  ActivityIcon,
  CalendarDaysIcon,
  Clock3Icon,
  ExternalLinkIcon,
  FilmIcon,
  HourglassIcon,
  StarIcon,
  TrophyIcon,
} from "lucide-react";
import { type CSSProperties, useMemo, useState } from "react";

import { MoviesGetWatchedQueryOptions, StatsGetQueryOptions } from "@/api/queries";

import { Avatar } from "@/components/movie-gang/Bits";
import { DateRangePopover, shortRange, type DayRange } from "@/components/movie-gang/DateRange";
import { FilterBar, FilterSelect } from "@/components/movie-gang/FilterBar";
import {
  filterOptionsFrom,
  hasActiveFilters,
  hueOf,
  type MovieFilters,
  NO_FILTERS,
  plural,
  profileUrl,
  runtimeLabel,
  tmdbPersonUrl,
  yearOf,
} from "@/components/movie-gang/lib";
import { MovieModal } from "@/components/movie-gang/MovieModal";
import { Poster } from "@/components/movie-gang/Poster";

import type {
  Movie,
  StatsHourCount,
  StatsNamedCount,
  StatsPersonCount,
  StatsWindow,
  StatsYearCount,
} from "@/types/Response";

const WINDOWS: { id: StatsWindow; label: string; calendar?: boolean }[] = [
  { id: "7d", label: "7d" },
  { id: "30d", label: "30d" },
  { id: "1y", label: "1y" },
  { id: "all-time", label: "All" },
  { id: "custom", label: "Custom", calendar: true },
];

function topNamed(items: StatsNamedCount[]) {
  return [...items].sort((a, b) => b.count - a.count || a.name.localeCompare(b.name))[0];
}
function topHour(items: StatsHourCount[]) {
  return [...items].sort((a, b) => b.count - a.count || a.hour - b.hour)[0];
}
function ymd(d: Date) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

export function StatsTab() {
  const [win, setWin] = useState<StatsWindow>("30d");
  const [showPicker, setShowPicker] = useState(false);
  const [customRange, setCustomRange] = useState<DayRange | null>(null);
  const [filters, setFilters] = useState<MovieFilters>(NO_FILTERS);
  const [selected, setSelected] = useState<Movie | null>(null);

  const timezone = useMemo(() => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC", []);

  const apiRange =
    win === "custom" && customRange?.start && customRange?.end
      ? { start: ymd(customRange.start), end: ymd(customRange.end) }
      : {};

  const { data: stats, isLoading, isError } = useQuery(
    StatsGetQueryOptions(
      win,
      timezone,
      apiRange.start,
      apiRange.end,
      filters.genre ?? undefined,
      filters.actors.map((p) => p.id),
      filters.crew.map((p) => p.id),
      filters.pickers.map((p) => p.id),
      filters.year ?? undefined,
      filters.decade ?? undefined,
    ),
  );

  // Filter options + watch years come from the already-cached watched list —
  // the same tiny dataset the Movies tab shows, no extra endpoint.
  const { data: watched } = useQuery(MoviesGetWatchedQueryOptions());
  const filterOptions = useMemo(() => filterOptionsFrom(watched ?? []), [watched]);
  const watchYears = useMemo(() => {
    const years = new Set<number>();
    for (const movie of watched ?? []) {
      if (movie.watchedAt) years.add(new Date(movie.watchedAt).getFullYear());
    }
    return [...years].sort((a, b) => b - a);
  }, [watched]);

  // Join the matched ids the stats endpoint returns back to the cached watched
  // movies, so the films-in-window rail renders posters without a second fetch
  // and its count can never drift from the "In window" KPI.
  const watchedById = useMemo(() => {
    const map = new Map<number, Movie>();
    for (const movie of watched ?? []) map.set(movie.movieID, movie);
    return map;
  }, [watched]);
  const matchedMovies = useMemo(
    () =>
      (stats?.matchedMovieIDs ?? [])
        .map((id) => watchedById.get(id))
        .filter((m): m is Movie => m !== undefined),
    [stats, watchedById],
  );
  // Render the open modal from the live list so an SSE refetch flows into it.
  const selectedLive = selected ? watchedById.get(selected.movieID) ?? selected : null;

  // The watch-year quick-select is sugar over the custom window: it reads as
  // "selected" only while the active custom range is exactly Jan 1 – Dec 31.
  const watchYear =
    win === "custom" &&
    customRange?.start &&
    customRange.end &&
    customRange.start.getFullYear() === customRange.end.getFullYear() &&
    customRange.start.getMonth() === 0 &&
    customRange.start.getDate() === 1 &&
    customRange.end.getMonth() === 11 &&
    customRange.end.getDate() === 31
      ? customRange.start.getFullYear()
      : null;

  const onWatchYear = (year: number | null) => {
    setShowPicker(false);
    if (year === null) {
      setWin("all-time");
      setCustomRange(null);
      return;
    }
    setCustomRange({ start: new Date(year, 0, 1), end: new Date(year, 11, 31) });
    setWin("custom");
  };

  // Rail clicks drill the whole page down: actors toggle into the actors
  // filter, directors into the crew filter (any-of within a group).
  const togglePerson = (key: "actors" | "crew") => (person: StatsPersonCount) =>
    setFilters((f) => ({
      ...f,
      [key]: f[key].some((p) => p.id === person.personId)
        ? f[key].filter((p) => p.id !== person.personId)
        : [...f[key], { id: person.personId, name: person.name }],
    }));
  const activeActorIds = useMemo(() => new Set(filters.actors.map((p) => p.id)), [filters.actors]);
  const activeCrewIds = useMemo(() => new Set(filters.crew.map((p) => p.id)), [filters.crew]);

  const filtered = hasActiveFilters(filters);
  const count = stats?.selectedWindowCount ?? 0;
  const topUser = stats && count > 0 ? topNamed(stats.watchedByUser) : undefined;
  const topDay = stats && count > 0 ? topNamed(stats.weekdayActivity) : undefined;
  const primeHour = stats && count > 0 ? topHour(stats.hourActivity) : undefined;

  const rangeLabel =
    win === "custom"
      ? watchYear !== null
        ? String(watchYear)
        : shortRange(customRange?.start, customRange?.end)
      : null;

  const onWin = (id: StatsWindow) => {
    if (id === "custom") {
      setShowPicker((p) => !p);
      return;
    }
    setWin(id);
    setShowPicker(false);
  };

  return (
    <div className="block">
      <div className="stats-head">
        <div className="sec-title items-center">
          <ActivityIcon size={20} style={{ color: "var(--accent)" }} />
          <div>
            <h2 className="m-0">Watch stats</h2>
            <div className="eyebrow mt-1">
              Timezone · {stats?.timezone ?? timezone}
              {rangeLabel ? ` · ${rangeLabel}` : ""}
            </div>
          </div>
        </div>
      </div>

      {/* One filter system: the time-range presets and the metadata chips sit
          in a single row, sharing the 30px rhythm and gold active states. */}
      <div className="statsfilters">
        <div className="win-control">
          <div className="seg seg--accent">
            {WINDOWS.map((w) => (
              <button
                key={w.id}
                type="button"
                data-active={win === w.id || (w.id === "custom" && showPicker)}
                onClick={() => onWin(w.id)}
              >
                {w.calendar && <CalendarDaysIcon />}
                {w.label}
              </button>
            ))}
          </div>
          {showPicker && (
            <DateRangePopover
              initial={customRange}
              onClose={() => setShowPicker(false)}
              onApply={(r) => {
                setCustomRange(r);
                setWin("custom");
                setShowPicker(false);
              }}
            />
          )}
        </div>

        <FilterBar
          options={filterOptions}
          value={filters}
          onChange={setFilters}
          yearLabel="Release year"
        >
          <FilterSelect
            label="Watch year"
            value={watchYear}
            choices={watchYears.map((y) => ({ value: y, label: String(y) }))}
            onChange={onWatchYear}
          />
        </FilterBar>
      </div>

      {isError ? (
        <p className="empty text-destructive">Failed to load stats.</p>
      ) : isLoading || !stats ? (
        <p className="empty">Loading stats…</p>
      ) : (
        <>
          <div className="stat-strip">
            <StatItem icon={<FilmIcon size={15} />} label="In window" value={count} sub="movies watched" mono />
            <StatItem
              icon={<HourglassIcon size={15} />}
              label="Hours watched"
              value={`${Math.round(stats.runtime.totalMinutes / 60)}h`}
              sub={stats.runtime.averageMinutes > 0 ? `avg ${runtimeLabel(stats.runtime.averageMinutes)}` : undefined}
              mono
            />
            <StatItem
              icon={<StarIcon size={15} />}
              label="Avg rating"
              value={stats.averageRating > 0 ? stats.averageRating.toFixed(1) : "—"}
              sub="TMDB average"
              mono
            />
            <StatItem icon={<TrophyIcon size={15} />} label="Top picker" value={topUser?.name ?? "—"} sub={plural(topUser?.count ?? 0, "movie")} />
            <StatItem icon={<CalendarDaysIcon size={15} />} label="Busiest day" value={topDay?.name ?? "—"} sub={plural(topDay?.count ?? 0, "movie")} />
            <StatItem icon={<Clock3Icon size={15} />} label="Prime time" value={primeHour?.label ?? "—"} sub={plural(primeHour?.count ?? 0, "movie")} mono />
          </div>

          {count > 0 && <MatchedMoviesRail movies={matchedMovies} count={count} onSelect={setSelected} />}

          {count === 0 && filtered ? (
            <p className="empty">No watched movies match these filters.</p>
          ) : (
            <>
              <PickedByMember rows={stats.watchedByUser} />

              <div className="two-col">
                <WeekdayActivity rows={stats.weekdayActivity} />
                <HourlyActivity hours={stats.hourActivity} />
              </div>

              {(stats.topGenres.length > 0 || stats.releaseYears.length > 0) && (
                <div className="two-col">
                  <TopGenres rows={stats.topGenres} />
                  <ReleaseDecades years={stats.releaseYears} />
                </div>
              )}

              <PeopleRail
                title="Most watched directors"
                people={stats.topDirectors}
                activeIds={activeCrewIds}
                onToggle={togglePerson("crew")}
              />
              <PeopleRail
                title="Most watched actors"
                people={stats.topActors}
                activeIds={activeActorIds}
                onToggle={togglePerson("actors")}
              />
            </>
          )}
        </>
      )}

      {selectedLive && <MovieModal movie={selectedLive} onClose={() => setSelected(null)} />}
    </div>
  );
}

/**
 * Horizontal rail of the films behind the current window/filters — the concrete
 * answer to the "In window" KPI. Posters reuse the Movies-tab tile visuals;
 * clicking one opens the detail modal. The heading count comes from the KPI
 * (the authoritative server count), so the two always agree.
 */
function MatchedMoviesRail({
  movies,
  count,
  onSelect,
}: {
  movies: Movie[];
  count: number;
  onSelect: (movie: Movie) => void;
}) {
  if (movies.length === 0) return null;
  return (
    // Flush under the KPI strip (which already closes with a bottom rule) — the
    // rail is the expansion of the "In window" count, not a separate section.
    <section className="statsec statsec--flush">
      <h3 className="statsec__title">Films in this window · {count}</h3>
      <div className="movierail">
        {movies.map((movie) => {
          const sub = [yearOf(movie.releaseDate), movie.addedByName].filter(Boolean).join(" · ");
          return (
            <button
              type="button"
              className="movietile"
              key={movie.movieID}
              onClick={() => onSelect(movie)}
              title={movie.title}
            >
              <Poster
                title={movie.title}
                hue={hueOf(movie.title)}
                posterPath={movie.posterPath}
                voteAverage={movie.voteAverage}
                showTitle={false}
              />
              <span className="movietile__meta">
                <span className="movietile__title">{movie.title}</span>
                <span className="movietile__sub">{sub}</span>
              </span>
            </button>
          );
        })}
      </div>
    </section>
  );
}

function StatItem({
  icon,
  label,
  value,
  sub,
  mono,
}: {
  icon: React.ReactNode;
  label: string;
  value: React.ReactNode;
  sub?: string;
  mono?: boolean;
}) {
  return (
    <div className="statitem">
      <div className="statitem__top">
        {icon}
        <span>{label}</span>
      </div>
      <div className={`statitem__val${mono ? " mono" : ""}`}>{value}</div>
      {sub && <div className="statitem__sub">{sub}</div>}
    </div>
  );
}

function PickedByMember({ rows }: { rows: StatsNamedCount[] }) {
  const max = Math.max(...rows.map((r) => r.count), 1);
  return (
    <section className="statsec">
      <h3 className="statsec__title">Picked by member</h3>
      {rows.length === 0 ? (
        <p className="empty">No watched movies in this window.</p>
      ) : (
        <div className="bar-rows">
          {rows.map((r, i) => (
            <div className="barrow" key={r.name}>
              <div className="b-name">
                <Avatar name={r.name} size={22} />
                <span>{r.name}</span>
              </div>
              <div className="b-track">
                <div
                  className="b-fill"
                  style={{
                    "--p": r.count / max,
                    animationDelay: `${i * 0.08}s`,
                    background: "var(--accent)",
                    opacity: r.count === 0 ? 0.15 : 1,
                  } as CSSProperties}
                />
              </div>
              <div className="b-val">{r.count}</div>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function WeekdayActivity({ rows }: { rows: StatsNamedCount[] }) {
  const max = Math.max(...rows.map((r) => r.count), 1);
  return (
    <section className="statsec">
      <h3 className="statsec__title">Weekday activity</h3>
      <div className="bar-rows" style={{ gap: 12 }}>
        {rows.map((r, i) => (
          <div className="barrow barrow--dow" key={r.name}>
            <div className="b-name">{r.name.slice(0, 3)}</div>
            <div className="b-track">
              <div
                className="b-fill"
                style={{
                  "--p": r.count / max,
                  animationDelay: `${i * 0.05}s`,
                  background: "var(--accent)",
                  opacity: r.count === 0 ? 0.15 : 1,
                } as CSSProperties}
              />
            </div>
            <div className="b-val">{r.count}</div>
          </div>
        ))}
      </div>
    </section>
  );
}

function HourlyActivity({ hours }: { hours: StatsHourCount[] }) {
  const max = Math.max(...hours.map((h) => h.count), 1);
  return (
    <section className="statsec">
      <h3 className="statsec__title">Hourly activity</h3>
      <div className="hourchart">
        <div className="hourchart__bars">
          {hours.map((entry) => {
            const hh = String(entry.hour).padStart(2, "0");
            return (
              <div
                className="hcol"
                key={entry.hour}
                // Marks empty hours so touch (hover:none) reveals counts for
                // active hours only — revealing all 24 would be a row of zeros.
                data-empty={entry.count === 0 ? "" : undefined}
                title={`${entry.count} at ${hh}:00`}
              >
                <span className="hcol__n">{entry.count}</span>
                {/* bar height is calc(--p * 88%); grows/tweens via index.css */}
                <div
                  className="hcol__bar"
                  style={{ "--p": entry.count / max, opacity: entry.count === 0 ? 0.18 : 1 } as CSSProperties}
                />
              </div>
            );
          })}
        </div>
        <div className="hourchart__axis">
          {hours.map((entry) => (
            // Label only every 6th hour to keep the axis legible across 24 columns.
            <span key={entry.hour}>{entry.hour % 6 === 0 ? String(entry.hour).padStart(2, "0") : ""}</span>
          ))}
        </div>
      </div>
    </section>
  );
}

/** Top genres donut: top segments + "Other". The disc is decorative (one
 *  accent, stepped alphas — see the `--donut-*` ramp in index.css); the
 *  legend beside it carries the actual mapping, so color is never the only
 *  channel. */
const DONUT_SEGMENTS = 6;

function TopGenres({ rows }: { rows: StatsNamedCount[] }) {
  if (rows.length === 0) return null;
  const top = rows.slice(0, DONUT_SEGMENTS);
  const otherCount = rows.slice(DONUT_SEGMENTS).reduce((sum, r) => sum + r.count, 0);
  const segments = [
    ...top.map((r, i) => ({ name: r.name, count: r.count, color: `var(--donut-${i + 1})` })),
    ...(otherCount > 0 ? [{ name: "Other", count: otherCount, color: "var(--donut-other)" }] : []),
  ];
  const total = segments.reduce((sum, s) => sum + s.count, 0);

  let acc = 0;
  const stops = segments.map((s) => {
    const from = (acc / total) * 100;
    acc += s.count;
    return `${s.color} ${from}% ${(acc / total) * 100}%`;
  });

  return (
    <section className="statsec">
      <h3 className="statsec__title">Top genres</h3>
      <div className="genredonut">
        <div
          className="donut"
          aria-hidden="true"
          style={{ background: `conic-gradient(${stops.join(", ")})` }}
        />
        <ul className="donut-legend">
          {segments.map((s) => (
            <li key={s.name}>
              <span className="donut-legend__swatch" style={{ background: s.color }} aria-hidden="true" />
              <span className="donut-legend__name">{s.name}</span>
              <span className="donut-legend__count">{s.count}</span>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

/**
 * Horizontally scrolling rail of clickable people cards (castcard visuals).
 * Clicking a card toggles that person in/out of the drill-down filter; the
 * corner link opens their TMDB page without touching the filter.
 */
function PeopleRail({
  title,
  people,
  activeIds,
  onToggle,
}: {
  title: string;
  people: StatsPersonCount[];
  activeIds: ReadonlySet<number>;
  onToggle: (person: StatsPersonCount) => void;
}) {
  if (people.length === 0) return null;
  return (
    <section className="statsec">
      <h3 className="statsec__title">{title}</h3>
      <div className="peoplerail">
        {people.map((p) => {
          const active = activeIds.has(p.personId);
          return (
            <div className="castcard peoplecard" key={p.personId} data-active={active}>
              <button
                type="button"
                className="peoplecard__toggle"
                aria-pressed={active}
                title={active ? `Stop filtering by ${p.name}` : `Filter stats by ${p.name}`}
                onClick={() => onToggle(p)}
              >
                <div className="castcard__photo">
                  <Avatar name={p.name} src={profileUrl(p.profilePath)} />
                </div>
                <span className="castcard__caption">
                  <span className="castcard__name">{p.name}</span>
                  <span className="castcard__role">{plural(p.count, "movie")}</span>
                </span>
              </button>
              {/* Sibling, never nested in the toggle — its own tab stop, and a
                  click here must not flip the filter. */}
              <a
                className="peoplecard__ext"
                href={tmdbPersonUrl(p.personId)}
                target="_blank"
                rel="noopener noreferrer"
                aria-label={`Open ${p.name} on TMDB`}
              >
                <ExternalLinkIcon />
              </a>
            </div>
          );
        })}
      </div>
    </section>
  );
}

function ReleaseDecades({ years }: { years: StatsYearCount[] }) {
  // Bucket the per-year histogram into decades client-side ("1990s"), filling
  // skipped decades with zero columns so the timeline reads chronologically —
  // a gap is information, not something to collapse away.
  const buckets = new Map<number, number>();
  for (const y of years) {
    const decade = Math.floor(y.year / 10) * 10;
    buckets.set(decade, (buckets.get(decade) ?? 0) + y.count);
  }
  if (buckets.size === 0) return null;
  const decades = [...buckets.keys()];
  const first = Math.min(...decades);
  const last = Math.max(...decades);
  const rows: { decade: number; count: number }[] = [];
  for (let d = first; d <= last; d += 10) {
    rows.push({ decade: d, count: buckets.get(d) ?? 0 });
  }
  const max = Math.max(...rows.map((r) => r.count), 1);

  return (
    <section className="statsec">
      <h3 className="statsec__title">Release decades</h3>
      <div className="hourchart hourchart--decades">
        <div className="hourchart__bars">
          {rows.map((r) => (
            <div
              className="hcol"
              key={r.decade}
              data-empty={r.count === 0 ? "" : undefined}
              title={`${plural(r.count, "movie")} from the ${r.decade}s`}
            >
              {/* counts always visible here (few columns) — see hourchart--decades */}
              <span className="hcol__n">{r.count}</span>
              <div
                className="hcol__bar"
                style={{ "--p": r.count / max, opacity: r.count === 0 ? 0.18 : 1 } as CSSProperties}
              />
            </div>
          ))}
        </div>
        <div className="hourchart__axis">
          {rows.map((r) => (
            <span key={r.decade}>{r.decade}s</span>
          ))}
        </div>
      </div>
    </section>
  );
}
