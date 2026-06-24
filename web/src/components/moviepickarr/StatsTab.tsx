import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
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
import { type CSSProperties, useCallback, useEffect, useId, useMemo, useRef, useState } from "react";

import { MoviesGetWatchedQueryOptions, StatsGetQueryOptions } from "@/api/queries";

import { Avatar } from "@/components/moviepickarr/Bits";
import { DateRangePopover } from "@/components/moviepickarr/DateRange";
import { shortRange } from "@/components/moviepickarr/dateRangeFormat";
import { exitDelayMs } from "@/components/moviepickarr/exitDelay";
import { FilterBar, FilterSelect } from "@/components/moviepickarr/FilterBar";
import {
  filterOptionsFrom,
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
import { Poster } from "@/components/moviepickarr/Poster";
import {
  filtersFromSearch,
  filtersToSearch,
  rangeFromSearch,
  ymd,
} from "@/components/moviepickarr/statsSearch";

import type {
  Movie,
  StatsHourCount,
  StatsNamedCount,
  StatsPersonCount,
  StatsWindow,
  StatsYearCount,
} from "@/types/Response";

import { useFlipRail } from "@/hooks/useFlipRail";

const WINDOWS: { id: StatsWindow; label: string; calendar?: boolean }[] = [
  { id: "7d", label: "7d" },
  { id: "30d", label: "30d" },
  { id: "1y", label: "1y" },
  { id: "all-time", label: "All" },
  { id: "custom", label: "Custom", calendar: true },
];

/** A rolling count plus its noun ("4 movies") — the number animates, the noun is a
 *  static suffix. Replaces plural(n, "movie") wherever the count should roll. */
function MovieCount({ value, animateOnMount }: { value: number; animateOnMount?: boolean }) {
  return <StatNumber value={value} animateOnMount={animateOnMount} suffix={` ${value === 1 ? "movie" : "movies"}`} />;
}

/** Rolling "Xh Ym" runtime (mirrors `runtimeLabel`: minutes unpadded, hours dropped
 *  under an hour). Each part animates independently; `prefix` is static lead text. */
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
  const search = useSearch({ from: "/stats" });
  const navigate = useNavigate({ from: "/stats" });
  const [showPicker, setShowPicker] = useState(false);
  // The custom-range popover shares the floating-surface exit motion: closePicker
  // flags it closing (so CSS plays daterange--closing), restores focus to the
  // trigger, then unmounts after exitDelayMs() — the same lockstep the Menu/Modal
  // use, and 0ms under reduced motion.
  const [pickerClosing, setPickerClosing] = useState(false);
  const pickerClosingRef = useRef(false);
  const pickerTimer = useRef<number | null>(null);
  const pickerId = useId();
  const customRef = useRef<HTMLButtonElement>(null);

  const closePicker = useCallback((restoreFocus: boolean, after?: () => void) => {
    if (pickerClosingRef.current) return;
    pickerClosingRef.current = true;
    setPickerClosing(true);
    if (restoreFocus) customRef.current?.focus();
    if (pickerTimer.current !== null) window.clearTimeout(pickerTimer.current);
    pickerTimer.current = window.setTimeout(() => {
      pickerClosingRef.current = false;
      pickerTimer.current = null;
      setPickerClosing(false);
      setShowPicker(false);
      after?.();
    }, exitDelayMs());
  }, []);

  // Hard-hide without the exit animation (when the view changes out from under the
  // popover — picking another preset or a watch year). Resets the closing guard so
  // a later open isn't blocked.
  const hidePickerNow = useCallback(() => {
    if (pickerTimer.current !== null) {
      window.clearTimeout(pickerTimer.current);
      pickerTimer.current = null;
    }
    pickerClosingRef.current = false;
    setPickerClosing(false);
    setShowPicker(false);
  }, []);

  useEffect(
    () => () => {
      if (pickerTimer.current !== null) window.clearTimeout(pickerTimer.current);
    },
    [],
  );

  // The modal is local state. A genre/year chip inside it is a same-route
  // /stats→/stats nav that never unmounts StatsTab, so the chip itself drives
  // the close (via MetaChips' onNavigate → the modal's animated `close`) rather
  // than this component reacting to the search change.
  const [selected, setSelected] = useState<Movie | null>(null);

  const win = search.win;
  const timezone = useMemo(() => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC", []);

  // The whole filter state is driven by the URL search params (see statsSearch).
  const customRange = useMemo(() => rangeFromSearch(search), [search]);
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
      search.genre || undefined,
      search.actors,
      search.crew,
      search.pickers,
      search.year || undefined,
      search.decade || undefined,
    ),
  );

  // Filter options + watch years come from the already-cached watched list —
  // the same tiny dataset the Movies tab shows, no extra endpoint.
  const { data: watched } = useQuery(MoviesGetWatchedQueryOptions());
  const filterOptions = useMemo(() => filterOptionsFrom(watched ?? []), [watched]);
  // Resolve the URL's id lists back into {id, name} chips for the FilterBar.
  const filters = useMemo(() => filtersFromSearch(search, filterOptions), [search, filterOptions]);
  const watchYears = useMemo(() => {
    const years = new Set<number>();
    for (const movie of watched ?? []) {
      if (movie.watchedAt) years.add(new Date(movie.watchedAt).getFullYear());
    }
    return [...years].sort((a, b) => b - a);
  }, [watched]);

  // Join the matched ids the stats endpoint returns back to the cached watched
  // movies, so the films-in-filter-view rail renders posters without a second
  // fetch and its count can never drift from the "In window" KPI.
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

  // Every filter mutation writes to the URL; the components below are unchanged.
  const setFilters = (next: MovieFilters) =>
    navigate({ search: (prev) => ({ ...prev, ...filtersToSearch(next) }) });

  const onWatchYear = (year: number | null) => {
    hidePickerNow();
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

  // Rail clicks drill the whole page down: actors toggle into the actors
  // filter, directors into the crew filter (any-of within a group).
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
      if (showPicker && !pickerClosingRef.current) {
        closePicker(true);
      } else {
        // Open — or interrupt an in-flight close and re-open, so a fast re-click
        // during the exit fade isn't swallowed. Clearing the timer is load-bearing:
        // otherwise the original close timer still fires and slams it shut again.
        if (pickerTimer.current !== null) {
          window.clearTimeout(pickerTimer.current);
          pickerTimer.current = null;
        }
        pickerClosingRef.current = false;
        setPickerClosing(false);
        setShowPicker(true);
      }
      return;
    }
    hidePickerNow();
    navigate({ search: (prev) => ({ ...prev, win: id, start: "", end: "" }) });
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
          <div className="seg">
            {WINDOWS.map((w) => {
              const isCustom = w.id === "custom";
              return (
                <button
                  key={w.id}
                  type="button"
                  ref={isCustom ? customRef : undefined}
                  data-active={win === w.id || (isCustom && showPicker)}
                  aria-haspopup={isCustom ? "dialog" : undefined}
                  aria-expanded={isCustom ? showPicker : undefined}
                  aria-controls={isCustom && showPicker ? pickerId : undefined}
                  onClick={() => onWin(w.id)}
                >
                  {w.calendar && <CalendarDaysIcon />}
                  {w.label}
                </button>
              );
            })}
          </div>
          {showPicker && (
            <DateRangePopover
              id={pickerId}
              triggerRef={customRef}
              closing={pickerClosing}
              initial={customRange}
              onDismiss={closePicker}
              onApply={(r) =>
                closePicker(true, () =>
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
        <p className="empty">Loading stats…</p>
      ) : (
        <>
          <div className="stat-strip">
            <StatItem icon={<FilmIcon size={15} />} label="In window" value={<StatNumber value={count} animateOnMount />} sub="movies watched" mono />
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
            <StatItem icon={<TrophyIcon size={15} />} label="Top picker" value={topUser?.name ?? "—"} sub={<MovieCount value={topUser?.count ?? 0} animateOnMount />} />
            <StatItem icon={<CalendarDaysIcon size={15} />} label="Busiest day" value={topDay?.name ?? "—"} sub={<MovieCount value={topDay?.count ?? 0} animateOnMount />} />
            <StatItem
              icon={<Clock3Icon size={15} />}
              label="Prime time"
              value={primeHour ? <StatNumber value={primeHour.hour} format={{ minimumIntegerDigits: 2 }} suffix=":00" animateOnMount /> : "—"}
              sub={<MovieCount value={primeHour?.count ?? 0} animateOnMount />}
              mono
            />
          </div>

          {/* The films rail always renders — it owns the single empty state for the
              filter view (it is the visual expansion of the "In window" KPI). When
              the count is zero every downstream section drops away with it: zeroed
              member bars and empty charts under an empty filter view are noise, not
              information. */}
          <MatchedMoviesRail movies={matchedMovies} count={count} filtered={filtered} onSelect={setSelected} />

          {count > 0 && (
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
  filtered,
  onSelect,
}: {
  movies: Movie[];
  count: number;
  filtered: boolean;
  onSelect: (movie: Movie) => void;
}) {
  // FLIP the matched-films rail: films present in both windows glide to their new
  // spot (the 30d prefix of a 1y set has a zero delta and stays put), new films
  // pop in, dropped films fade out then the rail tightens. Keyed by id; item data
  // resolves live so an SSE refetch flows in without re-animating.
  const { containerRef, entries, itemProps } = useFlipRail<Movie>(movies, (m) => String(m.movieID));
  return (
    // Flush under the KPI strip (which already closes with a bottom rule) — the
    // rail is the expansion of the "In window" count, not a separate section.
    <section className="statsec statsec--flush">
      <h3 className="statsec__title">
        Films in Filter View · <StatNumber value={count} />
      </h3>
      {entries.length === 0 ? (
        // count > 0 with no posters means the cached watched list is still
        // catching up to the stats count (transient); count 0 is a genuinely
        // empty filter view, worded by whether a filter is narrowing it.
        <p className="empty">
          {count > 0
            ? "Loading films…"
            : filtered
              ? "No films match the current filter view."
              : "No films watched in this window yet."}
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

function PickedByMember({ rows }: { rows: StatsNamedCount[] }) {
  const max = Math.max(...rows.map((r) => r.count), 1);
  // FLIP the leaderboard: when a window change reranks members, the rows glide to
  // their new rank rather than snapping; new members pop in, dropped ones fade
  // out then the column tightens. Bar widths still tween via the b-fill CSS.
  const { containerRef, entries, itemProps } = useFlipRail<StatsNamedCount>(rows, (r) => r.name);
  return (
    <section className="statsec">
      <h3 className="statsec__title">Picked by member</h3>
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
              <div className="b-val"><StatNumber value={r.count} /></div>
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
            <div className="b-val"><StatNumber value={r.count} /></div>
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
              <span className="donut-legend__count"><StatNumber value={s.count} /></span>
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
  // FLIP the ranking: when a window change reranks people, cards glide between
  // ranks; new entries pop in, dropped ones fade out then the rail tightens.
  // Keyed by personId only (not count) — a count change rolls the card's
  // NumberFlow live without re-animating the rail; order changes drive the glide.
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
                  <span className="castcard__role"><MovieCount value={p.count} /></span>
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
