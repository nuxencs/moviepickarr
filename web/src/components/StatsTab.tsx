import { useQuery } from "@tanstack/react-query";
import { ActivityIcon, CalendarDaysIcon, Clock3Icon, UsersIcon } from "lucide-react";
import { useMemo, useState } from "react";

import { StatsGetQueryOptions } from "@/api/queries";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { TimeRangePicker, type TimeRangeValue } from "@/components/ui/time-range-picker";

import type { StatsHourCount, StatsNamedCount, StatsWindow } from "@/types/Response";
import type { DateRange } from "react-day-picker";

function topNamedItem(items: StatsNamedCount[]): StatsNamedCount | undefined {
  return [...items].sort((a, b) => b.count - a.count || a.name.localeCompare(b.name))[0];
}

function topHourItem(items: StatsHourCount[]): StatsHourCount | undefined {
  return [...items].sort((a, b) => b.count - a.count || a.hour - b.hour)[0];
}

function formatDateInput(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function addDays(date: Date, days: number): Date {
  const next = new Date(date);
  next.setDate(next.getDate() + days);
  return next;
}

function defaultCustomRange(): DateRange {
  const to = new Date();
  const from = addDays(to, -30);
  return { from, to };
}

function toApiRange(range?: DateRange): { start?: string; end?: string } {
  if (!range?.from || !range?.to) {
    return {};
  }
  return {
    start: formatDateInput(range.from),
    end: formatDateInput(range.to),
  };
}

export function StatsTab() {
  const [selectedWindow, setSelectedWindow] = useState<StatsWindow>("30d");
  const [customRange, setCustomRange] = useState<DateRange>(() => defaultCustomRange());

  const timezone = useMemo(
    () => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
    [],
  );

  const customApiRange = selectedWindow === "custom" ? toApiRange(customRange) : {};

  const { data: stats, isLoading, isError } = useQuery(
    StatsGetQueryOptions(
      selectedWindow,
      timezone,
      customApiRange.start,
      customApiRange.end,
    ),
  );

  const selectedCount = stats?.selectedWindowCount ?? 0;
  const topWeekday = useMemo(
    () => (stats && selectedCount > 0 ? topNamedItem(stats.weekdayActivity) : undefined),
    [selectedCount, stats],
  );
  const topUser = useMemo(
    () => (stats && selectedCount > 0 ? topNamedItem(stats.watchedByUser) : undefined),
    [selectedCount, stats],
  );
  const topHour = useMemo(
    () => (stats && selectedCount > 0 ? topHourItem(stats.hourActivity) : undefined),
    [selectedCount, stats],
  );
  const userMax = useMemo(
    () => Math.max(...(stats?.watchedByUser.map((item) => item.count) ?? [1]), 1),
    [stats],
  );
  const weekdayMax = useMemo(
    () => Math.max(...(stats?.weekdayActivity.map((item) => item.count) ?? [1]), 1),
    [stats],
  );
  const hourMax = useMemo(
    () => Math.max(...(stats?.hourActivity.map((item) => item.count) ?? [1]), 1),
    [stats],
  );

  const selectedWindowLabel = selectedWindow === "custom"
    ? `${stats?.customRangeStart ?? customApiRange.start} to ${stats?.customRangeEnd ?? customApiRange.end}`
    : selectedWindow;

  function onChangeWindow(value: TimeRangeValue) {
    setSelectedWindow(value.period);
    if (value.startDate && value.endDate) {
      setCustomRange({ from: value.startDate, to: value.endDate });
    }
  }

  return (
    <div className="p-4 pt-0 grid gap-4">
      <Card>
        <CardHeader className="gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle className="flex items-center gap-2">
              <ActivityIcon className="size-5"/>
              Watch Stats
            </CardTitle>
            <CardDescription className="mt-1">
              Timezone: {stats?.timezone ?? timezone}
            </CardDescription>
          </div>

          <TimeRangePicker
            value={{
              period: selectedWindow,
              startDate: customRange.from,
              endDate: customRange.to,
            }}
            onChange={onChangeWindow}
          />
        </CardHeader>
      </Card>

      {isLoading && (
        <Card>
          <CardContent className="pt-6 text-center text-muted-foreground">
            Loading stats...
          </CardContent>
        </Card>
      )}

      {isError && (
        <Card>
          <CardContent className="pt-6 text-center text-destructive">
            Failed to load stats.
          </CardContent>
        </Card>
      )}

      {!isLoading && !isError && stats && (
        <>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <Card>
              <CardHeader className="pb-2">
                <CardDescription className="flex items-center gap-2">
                  <ActivityIcon className="size-4"/>
                  Selected Window
                </CardDescription>
              </CardHeader>
              <CardContent>
                <p className="text-3xl font-bold">{selectedCount}</p>
                <p className="text-xs text-muted-foreground">movies watched</p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription className="flex items-center gap-2">
                  <UsersIcon className="size-4"/>
                  Most Active User
                </CardDescription>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-bold">{topUser?.name ?? "n/a"}</p>
                <p className="text-xs text-muted-foreground">{topUser?.count ?? 0} movies</p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription className="flex items-center gap-2">
                  <CalendarDaysIcon className="size-4"/>
                  Most Active Day
                </CardDescription>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-bold">{topWeekday?.name ?? "n/a"}</p>
                <p className="text-xs text-muted-foreground">{topWeekday?.count ?? 0} movies</p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription className="flex items-center gap-2">
                  <Clock3Icon className="size-4"/>
                  Most Active Hour
                </CardDescription>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-bold">{topHour?.label ?? "n/a"}</p>
                <p className="text-xs text-muted-foreground">{topHour?.count ?? 0} movies</p>
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Watched By User</CardTitle>
              <CardDescription>For selected window ({selectedWindowLabel})</CardDescription>
            </CardHeader>
            <CardContent className="grid gap-3">
              {stats.watchedByUser.length === 0 && (
                <p className="text-center text-muted-foreground">No watched movies in this window.</p>
              )}
              {stats.watchedByUser.map((entry) => {
                const width = (entry.count / userMax) * 100;
                return (
                  <div key={entry.name} className="grid gap-1">
                    <div className="flex items-center justify-between text-sm">
                      <span>{entry.name}</span>
                      <span className="text-muted-foreground">{entry.count}</span>
                    </div>
                    <div className="h-2 rounded-full bg-muted">
                      <div
                        className="h-2 rounded-full bg-primary transition-[width] duration-500"
                        style={{ width: `${width}%` }}
                      />
                    </div>
                  </div>
                );
              })}
            </CardContent>
          </Card>

          <div className="grid gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Weekday Activity</CardTitle>
              </CardHeader>
              <CardContent className="grid gap-2">
                {stats.weekdayActivity.map((entry) => {
                  const width = (entry.count / weekdayMax) * 100;
                  return (
                    <div key={entry.name} className="grid gap-1">
                      <div className="flex items-center justify-between text-sm">
                        <span>{entry.name}</span>
                        <span className="text-muted-foreground">{entry.count}</span>
                      </div>
                      <div className="h-2 rounded-full bg-muted">
                        <div
                          className="h-2 rounded-full bg-emerald-500 transition-[width] duration-500"
                          style={{ width: `${width}%` }}
                        />
                      </div>
                    </div>
                  );
                })}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Hourly Activity</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="grid grid-cols-6 gap-2 sm:grid-cols-8 lg:grid-cols-12">
                  {stats.hourActivity.map((entry) => {
                    const height = entry.count === 0 ? 0 : Math.max(8, (entry.count / hourMax) * 100);
                    return (
                      <div key={entry.hour} className="rounded-lg border border-border/80 bg-muted/30 p-2">
                        <p className="text-xs font-medium">{entry.count}</p>
                        <div className="mt-2 flex h-16 items-end">
                          <div
                            className="w-full rounded-sm bg-amber-500 transition-[height] duration-500"
                            style={{ height: `${height}%` }}
                          />
                        </div>
                        <p className="mt-2 text-[11px] text-muted-foreground">{entry.label.slice(0, 2)}</p>
                      </div>
                    );
                  })}
                </div>
              </CardContent>
            </Card>
          </div>
        </>
      )}
    </div>
  );
}
