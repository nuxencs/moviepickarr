import { useQuery } from "@tanstack/react-query";
import { ActivityIcon, CalendarDaysIcon, Clock3Icon, FilmIcon, TrophyIcon } from "lucide-react";
import { useMemo, useState } from "react";

import { StatsGetQueryOptions } from "@/api/queries";

import { Avatar } from "@/components/movie-gang/Bits";
import { DateRangePopover, shortRange, type DayRange } from "@/components/movie-gang/DateRange";
import { plural } from "@/components/movie-gang/lib";

import type { StatsHourCount, StatsNamedCount, StatsWindow } from "@/types/Response";

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

  const timezone = useMemo(() => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC", []);

  const apiRange =
    win === "custom" && customRange?.start && customRange?.end
      ? { start: ymd(customRange.start), end: ymd(customRange.end) }
      : {};

  const { data: stats, isLoading, isError } = useQuery(
    StatsGetQueryOptions(win, timezone, apiRange.start, apiRange.end),
  );

  const count = stats?.selectedWindowCount ?? 0;
  const topUser = stats && count > 0 ? topNamed(stats.watchedByUser) : undefined;
  const topDay = stats && count > 0 ? topNamed(stats.weekdayActivity) : undefined;
  const primeHour = stats && count > 0 ? topHour(stats.hourActivity) : undefined;

  const rangeLabel = win === "custom" ? shortRange(customRange?.start, customRange?.end) : null;

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
      </div>

      {isError ? (
        <p className="empty text-destructive">Failed to load stats.</p>
      ) : isLoading || !stats ? (
        <p className="empty">Loading stats…</p>
      ) : (
        <>
          <div className="stat-strip">
            <StatItem icon={<FilmIcon size={15} />} label="In window" value={count} sub="movies watched" mono />
            <StatItem icon={<TrophyIcon size={15} />} label="Top picker" value={topUser?.name ?? "—"} sub={plural(topUser?.count ?? 0, "movie")} />
            <StatItem icon={<CalendarDaysIcon size={15} />} label="Busiest day" value={topDay?.name ?? "—"} sub={plural(topDay?.count ?? 0, "movie")} />
            <StatItem icon={<Clock3Icon size={15} />} label="Prime time" value={primeHour?.label ?? "—"} sub={plural(primeHour?.count ?? 0, "movie")} mono />
          </div>

          <PickedByMember rows={stats.watchedByUser} />

          <div className="two-col">
            <WeekdayActivity rows={stats.weekdayActivity} />
            <HourlyActivity hours={stats.hourActivity} />
          </div>
        </>
      )}
    </div>
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
                    width: `${(r.count / max) * 100}%`,
                    animationDelay: `${i * 0.08}s`,
                    background: "var(--accent)",
                    opacity: r.count === 0 ? 0.15 : 1,
                  }}
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
                  width: `${(r.count / max) * 100}%`,
                  animationDelay: `${i * 0.05}s`,
                  background: "var(--accent)",
                  opacity: r.count === 0 ? 0.15 : 1,
                }}
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
                {/* capped at 88% to leave headroom for the hover count */}
                <div
                  className="hcol__bar"
                  style={{ height: `${(entry.count / max) * 88}%`, opacity: entry.count === 0 ? 0.18 : 1 }}
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
