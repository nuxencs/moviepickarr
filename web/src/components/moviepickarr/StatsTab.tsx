import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import {
  ActivityIcon,
  CalendarDaysIcon,
  Clock3Icon,
  ExternalLinkIcon,
  FilmIcon as MovieIcon,
  HourglassIcon,
  StarIcon,
  TrophyIcon,
} from "lucide-react";
import { type CSSProperties, useCallback, useEffect, useId, useMemo, useRef, useState } from "react";

import { FilterOptionsQueryOptions, MoviesGetWatchedQueryOptions, StatsGetQueryOptions } from "@/api/queries";

import { Avatar } from "@/components/moviepickarr/Bits";
import { DateRangePopover } from "@/components/moviepickarr/DateRange";
import { shortRange } from "@/components/moviepickarr/dateRangeFormat";
import { FilterBar, FilterSelect } from "@/components/moviepickarr/FilterBar";
import {
  type FilterOptions,
  hasActiveFilters,
  hueOf,
  type MovieFilters,
  plural,
  profileUrl,
  tmdbPersonUrl,
  yearOf,
} from "@/components/moviepickarr/lib";
import { MovieModal } from "@/components/moviepickarr/MovieModal";
import { StatNumber } from "@/components/moviepickarr/numberRoll";
import { GENERAL_POSTER_SIZES, Poster } from "@/components/moviepickarr/Poster";
import { StatsBodySkeleton } from "@/components/moviepickarr/Skeletons";
import {
  filtersFromSearch,
  filtersToSearch,
  rangeFromSearch,
  statsFiltersFromSearch,
  ymd,
} from "@/components/moviepickarr/statsSearch";

import type {
  MovieTile,
  StatsHourCount,
  StatsNamedCount,
  StatsPersonCount,
  StatsWindow,
  StatsYearCount,
} from "@/types/Response";

import { useDismissible } from "@/hooks/useDismissible";
import { useFlipRail } from "@/hooks/useFlipRail";
import { useMovieModal } from "@/hooks/useMovieModalHistory";

// Stable reference, so the filters useMemo does not rerun while options load.
const EMPTY_FILTER_OPTIONS: FilterOptions = { genres: [], actors: [], crew: [], years: [], adders: [] };

const WINDOWS: { id: StatsWindow; label: string; calendar?: boolean }[] = [
  { id: "7d", label: "7d" },
  { id: "30d", label: "30d" },
  { id: "1y", label: "1y" },
  { id: "all-time", label: "All" },
  { id: "custom", label: "Custom", calendar: true },
];

/** A rolling plural(n, "movie"). */
function MovieCount({ value, animateOnMount }: { value: number; animateOnMount?: boolean }) {
  return <StatNumber value={value} animateOnMount={animateOnMount} suffix={` ${value === 1 ? "movie" : "movies"}`} />;
}

/** A rolling runtimeLabel. */
function RuntimeCount({ minutes, prefix, animateOnMount }: { minutes: number; prefix?: string; animateOnMount?: boolean }) {
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return (
    <>
      {prefix}
      {h > 0 && (
        <>
          <StatNumber value={h} animateOnMount={animateOnMount} suffix="h" />{" "}
        </>
      )}
      <StatNumber value={m} animateOnMount={animateOnMount} suffix="m" />
    </>
  );
}

function topNamed(items: StatsNamedCount[]) {
  return [...items].sort((a, b) => b.count - a.count || a.name.localeCompare(b.name))[0];
}
function topHour(items: StatsHourCount[]) {
  return [...items].sort((a, b) => b.count - a.count || a.hour - b.hour)[0];
}
export function StatsTab() {
  // useSearch takes the route id (pathless `_app` layout), useNavigate the URL.
  const search = useSearch({ from: "/_app/stats" });
  const navigate = useNavigate({ from: "/stats" });
  const rangeId = useId();
  const customRef = useRef<HTMLButtonElement>(null);

  // hideNow is for when the view changes out from under the popover.
  const range = useDismissible({ restoreFocusTo: customRef });
  const { dismiss: dismissRange } = range;

  const closeRange = useCallback(
    (restoreFocus: boolean, after?: () => void) => dismissRange({ restoreFocus, after }),
    [dismissRange],
  );

  // A history entry per open modal, so browser Back closes it (#196).
  const { selected, isOpen, open, close, onClosed } = useMovieModal();

  // The below-fold panels carry most of the mount cost, so they mount only as
  // they near the viewport. Latches true, so filter changes never re-gate them.
  const [panelsVisible, setPanelsVisible] = useState(false);
  const panelsAnchorRef = useRef<HTMLDivElement>(null);

  const win = search.win;
  const timezone = useMemo(() => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC", []);

  const customRange = useMemo(() => rangeFromSearch(search), [search]);
  const apiRange =
    win === "custom" && customRange?.start && customRange?.end
      ? { start: ymd(customRange.start), end: ymd(customRange.end) }
      : {};

  const { data: stats, isLoading, isError } = useQuery(
    StatsGetQueryOptions(win, timezone, apiRange, statsFiltersFromSearch(search)),
  );

  // The watched list is lean (no credits), so filter options come from the server.
  const { data: watched } = useQuery(MoviesGetWatchedQueryOptions());
  const { data: filterOptionsData } = useQuery(FilterOptionsQueryOptions());
  const filterOptions: FilterOptions = filterOptionsData ?? EMPTY_FILTER_OPTIONS;

  // The IO callback runs after first paint, so even an in-view anchor keeps the
  // panels off the initial-paint path.
  useEffect(() => {
    if (panelsVisible) return;
    if (!stats || (stats.selectedWindowCount ?? 0) === 0) return;
    const el = panelsAnchorRef.current;
    if (!el) return;
    if (typeof IntersectionObserver === "undefined") {
      setPanelsVisible(true);
      return;
    }
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          setPanelsVisible(true);
          io.disconnect();
        }
      },
      { rootMargin: "200px 0px" },
    );
    io.observe(el);
    return () => io.disconnect();
  }, [panelsVisible, stats]);
  const filters = useMemo(() => filtersFromSearch(search, filterOptions), [search, filterOptions]);
  const watchYears = useMemo(() => {
    const years = new Set<number>();
    for (const movie of watched ?? []) {
      if (movie.watchedAt) years.add(new Date(movie.watchedAt).getFullYear());
    }
    return [...years].sort((a, b) => b - a);
  }, [watched]);

  // Join matched ids to the cached watched list: no second fetch, no count drift.
  const watchedById = useMemo(() => {
    const map = new Map<number, MovieTile>();
    for (const movie of watched ?? []) map.set(movie.movieID, movie);
    return map;
  }, [watched]);
  const matchedMovies = useMemo(
    () =>
      (stats?.matchedMovieIDs ?? [])
        .map((id) => watchedById.get(id))
        .filter((m): m is MovieTile => m !== undefined),
    [stats, watchedById],
  );
  // Render the open modal from the live list so an SSE refetch flows into it.
  const selectedLive = selected ? watchedById.get(selected.movieID) ?? selected : null;

  // Watch year is sugar over a custom range of exactly Jan 1 - Dec 31.
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

  const setFilters = (next: MovieFilters) =>
    navigate({ search: (prev) => ({ ...prev, ...filtersToSearch(next) }) });

  const onWatchYear = (year: number | null) => {
    range.hideNow();
    if (year === null) {
      navigate({ search: (prev) => ({ ...prev, win: "all-time", start: "", end: "" }) });
      return;
    }
    navigate({
      search: (prev) => ({
        ...prev,
        win: "custom",
        start: ymd(new Date(year, 0, 1)),
        end: ymd(new Date(year, 11, 31)),
      }),
    });
  };

  const togglePerson = (key: "actors" | "crew") => (person: StatsPersonCount) => {
    const ids = search[key];
    const next = (
      ids.includes(person.personId)
        ? ids.filter((id) => id !== person.personId)
        : [...ids, person.personId]
    ).sort((a, b) => a - b);
    navigate({ search: (prev) => ({ ...prev, [key]: next }) });
  };
  const activeActorIds = useMemo(() => new Set(search.actors), [search.actors]);
  const activeCrewIds = useMemo(() => new Set(search.crew), [search.crew]);

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
      if (range.open && !range.closing) {
        closeRange(true);
      } else {
        // show() also cancels an in-flight close, so a fast re-click is not lost.
        range.show();
      }
      return;
    }
    range.hideNow();
    navigate({ search: (prev) => ({ ...prev, win: id, start: "", end: "" }) });
  };

  return (
    <div className="mg-rise">
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

      <div className="statsfilters">
        <div className="win-control">
          <div className="seg">
            {WINDOWS.map((w) => {
              const isCustom = w.id === "custom";
              return (
                <button
                  key={w.id}
                  type="button"
                  ref={isCustom ? customRef : undefined}
                  data-active={win === w.id || (isCustom && range.open)}
                  aria-haspopup={isCustom ? "dialog" : undefined}
                  aria-expanded={isCustom ? range.open : undefined}
                  aria-controls={isCustom && range.open ? rangeId : undefined}
                  onClick={() => onWin(w.id)}
                >
                  {w.calendar && <CalendarDaysIcon />}
                  {w.label}
                </button>
              );
            })}
          </div>
          {range.open && (
            <DateRangePopover
              id={rangeId}
              triggerRef={customRef}
              closing={range.closing}
              isTopmost={range.isTopmost}
              initial={customRange}
              onDismiss={closeRange}
              onApply={(r) =>
                closeRange(true, () =>
                  navigate({
                    search: (prev) => ({
                      ...prev,
                      win: "custom",
                      start: r.start ? ymd(r.start) : "",
                      end: r.end ? ymd(r.end) : "",
                    }),
                  }),
                )
              }
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
        <StatsBodySkeleton />
      ) : (
        <>
          <div className="stat-strip">
            <StatItem icon={<MovieIcon size={15} />} label="In window" value={<StatNumber value={count} animateOnMount />} sub="movies watched" mono />
            <StatItem
              icon={<HourglassIcon size={15} />}
              label="Hours watched"
              value={<StatNumber value={Math.round(stats.runtime.totalMinutes / 60)} suffix="h" animateOnMount />}
              sub={stats.runtime.averageMinutes > 0 ? <RuntimeCount minutes={stats.runtime.averageMinutes} prefix="avg " animateOnMount /> : undefined}
              mono
            />
            <StatItem
              icon={<StarIcon size={15} />}
              label="Avg rating"
              value={
                stats.averageRating > 0 ? (
                  <StatNumber value={stats.averageRating} format={{ minimumFractionDigits: 1, maximumFractionDigits: 1 }} animateOnMount />
                ) : (
                  "—"
                )
              }
              sub="TMDB average"
              mono
            />
            <StatItem icon={<TrophyIcon size={15} />} label="Most-watched adder" value={topUser?.name ?? "—"} sub={<MovieCount value={topUser?.count ?? 0} animateOnMount />} />
            <StatItem icon={<CalendarDaysIcon size={15} />} label="Busiest day" value={topDay?.name ?? "—"} sub={<MovieCount value={topDay?.count ?? 0} animateOnMount />} />
            <StatItem
              icon={<Clock3Icon size={15} />}
              label="Prime time"
              value={primeHour ? <StatNumber value={primeHour.hour} format={{ minimumIntegerDigits: 2 }} suffix=":00" animateOnMount /> : "—"}
              sub={<MovieCount value={primeHour?.count ?? 0} animateOnMount />}
              mono
            />
          </div>

          {/* Owns the one empty state; count 0 drops every panel below. */}
          <MatchedMoviesRail movies={matchedMovies} count={count} filtered={filtered} onSelect={open} />

          <div ref={panelsAnchorRef} aria-hidden="true" className="stats-panels-anchor" />

          {count > 0 && panelsVisible && (
            <>
              <AddedByMember rows={stats.watchedByUser} />

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

      {selectedLive && (
        <MovieModal movie={selectedLive} open={isOpen} onRequestClose={close} onClose={onClosed} />
      )}
    </div>
  );
}

/** The movies behind the "In window" KPI; the heading count is the server's. */
function MatchedMoviesRail({
  movies,
  count,
  filtered,
  onSelect,
}: {
  movies: MovieTile[];
  count: number;
  filtered: boolean;
  onSelect: (movie: MovieTile) => void;
}) {
  const { containerRef, entries, itemProps } = useFlipRail<MovieTile>(movies, (m) => String(m.movieID));
  return (
    <section className="statsec statsec--flush">
      <h3 className="statsec__title">
        Movies in Filter View · <StatNumber value={count} />
      </h3>
      {entries.length === 0 ? (
        // count > 0 with no posters: the watched list is still catching up.
        <p className="empty">
          {count > 0
            ? "Loading movies…"
            : filtered
              ? "No movies match the current filter view."
              : "No movies watched in this window yet."}
        </p>
      ) : (
        <div className="movierail" ref={containerRef}>
          {entries.map(({ key, item: movie, exiting }) => {
            const sub = [yearOf(movie.releaseDate), movie.addedByName].filter(Boolean).join(" · ");
            return (
              <button
                type="button"
                className="movietile"
                key={key}
                data-flip-exit={exiting || undefined}
                {...itemProps(key)}
                onClick={() => onSelect(movie)}
                title={movie.title}
              >
                <Poster
                  title={movie.title}
                  hue={hueOf(movie.title)}
                  posterPath={movie.posterPath}
                  showTitle={false}
                  sizes={GENERAL_POSTER_SIZES}
                />
                <span className="movietile__meta">
                  <span className="movietile__title">{movie.title}</span>
                  <span className="movietile__sub">{sub}</span>
                </span>
              </button>
            );
          })}
        </div>
      )}
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
  sub?: React.ReactNode;
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

function AddedByMember({ rows }: { rows: StatsNamedCount[] }) {
  const max = Math.max(...rows.map((r) => r.count), 1);
  const { containerRef, entries, itemProps } = useFlipRail<StatsNamedCount>(rows, (r) => r.name);
  return (
    <section className="statsec">
      <h3 className="statsec__title">Most-watched adders</h3>
      {entries.length === 0 ? (
        <p className="empty">No watched movies in this window.</p>
      ) : (
        <div className="bar-rows" ref={containerRef}>
          {entries.map(({ key, item: r, exiting }, i) => (
            <div className="barrow" key={key} data-flip-exit={exiting || undefined} {...itemProps(key)}>
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
                // Touch reveals counts only for active hours, not a row of zeros.
                data-empty={entry.count === 0 ? "" : undefined}
                title={`${entry.count} at ${hh}:00`}
              >
                <span className="hcol__n">{entry.count}</span>
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
            <span key={entry.hour}>{entry.hour % 6 === 0 ? String(entry.hour).padStart(2, "0") : ""}</span>
          ))}
        </div>
      </div>
    </section>
  );
}

/** In step with the `--donut-*` ramp in index.css. The legend carries the
 *  mapping, so color is never the only channel. */
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
  // Keyed by personId only, so a count change does not re-animate the rail.
  const { containerRef, entries, itemProps } = useFlipRail<StatsPersonCount>(people, (p) => String(p.personId));
  if (entries.length === 0) return null;
  return (
    <section className="statsec">
      <h3 className="statsec__title">{title}</h3>
      <div className="peoplerail" ref={containerRef}>
        {entries.map(({ key, item: p, exiting }) => {
          const active = activeIds.has(p.personId);
          return (
            <div className="castcard peoplecard" key={key} data-active={active} data-flip-exit={exiting || undefined} {...itemProps(key)}>
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
              {/* Not nested in the toggle: a click here must not flip the filter. */}
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
  // Skipped decades get zero columns: a gap is information.
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
