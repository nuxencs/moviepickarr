import { CalendarDaysIcon, ChevronDownIcon } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

import type { DateRange } from "react-day-picker";

import { cn } from "@/lib/utils";

export type TimeRangePeriod = "24h" | "7d" | "30d" | "90d" | "1y" | "all-time" | "custom";

export interface TimeRangeValue {
  period: TimeRangePeriod;
  startDate?: Date;
  endDate?: Date;
}

interface TimeRangePickerProps {
  value: TimeRangeValue;
  onChange: (value: TimeRangeValue) => void;
  className?: string;
}

const presets: Array<{ value: Exclude<TimeRangePeriod, "custom">; label: string }> = [
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
  { value: "1y", label: "1y" },
  { value: "all-time", label: "All" },
];

const presetLabelByValue: Record<Exclude<TimeRangePeriod, "custom">, string> = {
  "24h": "24h",
  "7d": "7d",
  "30d": "30d",
  "90d": "90d",
  "1y": "1y",
  "all-time": "All",
};

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

function toDateRange(value: TimeRangeValue): DateRange {
  if (value.startDate && value.endDate) {
    return { from: value.startDate, to: value.endDate };
  }
  return defaultCustomRange();
}

function toCustomLabel(value: TimeRangeValue): string {
  if (value.period !== "custom" || !value.startDate) {
    return "Custom";
  }

  const fromLabel = value.startDate.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });

  if (!value.endDate) {
    return fromLabel;
  }

  const toLabel = value.endDate.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });

  return `${fromLabel} - ${toLabel}`;
}

function toWindowLabel(value: TimeRangeValue): string {
  if (value.period === "custom") {
    return "Custom";
  }
  return presetLabelByValue[value.period];
}

export function TimeRangePicker({ value, onChange, className }: TimeRangePickerProps) {
  const [customPopoverOpen, setCustomPopoverOpen] = useState(false);
  const [draftRange, setDraftRange] = useState<DateRange>(() => toDateRange(value));

  function onSelectPreset(period: Exclude<TimeRangePeriod, "custom">) {
    setCustomPopoverOpen(false);
    onChange({ ...value, period });
  }

  function onOpenCustomPicker() {
    setDraftRange(toDateRange(value));
    setCustomPopoverOpen(true);
  }

  function applyCustomRange() {
    if (!draftRange?.from || !draftRange?.to) {
      return;
    }

    onChange({
      period: "custom",
      startDate: draftRange.from,
      endDate: draftRange.to,
    });
    setCustomPopoverOpen(false);
  }

  function cancelCustomRange() {
    setDraftRange(toDateRange(value));
    setCustomPopoverOpen(false);
  }

  return (
    <div className={cn("w-full sm:w-auto", className)}>
      <div className="sm:hidden">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" className="w-full justify-between">
              <span className="inline-flex items-center gap-2 truncate">
                <CalendarDaysIcon className="size-4 shrink-0" />
                <span className="truncate">{toWindowLabel(value)}</span>
              </span>
              <ChevronDownIcon className="size-4 opacity-70" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-[var(--radix-dropdown-menu-trigger-width)]">
            {presets.map((preset) => (
              <DropdownMenuItem
                key={preset.value}
                className={cn(value.period === preset.value ? "bg-accent text-accent-foreground" : "")}
                onSelect={() => onSelectPreset(preset.value)}
              >
                {preset.label}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <Popover
        open={customPopoverOpen}
        onOpenChange={(open) => {
          if (open) {
            setDraftRange(toDateRange(value));
          }
          setCustomPopoverOpen(open);
        }}
      >
        <div className="hidden items-center gap-1 rounded-2xl bg-muted/70 p-1 sm:inline-flex">
          {presets.map((preset) => {
            const isActive = value.period === preset.value;
            return (
              <button
                key={preset.value}
                type="button"
                onClick={() => onSelectPreset(preset.value)}
                className={cn(
                  "inline-flex h-10 shrink-0 items-center rounded-xl px-4 text-xl font-semibold tracking-tight transition-colors",
                  isActive
                    ? "bg-background text-foreground shadow"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {preset.label}
              </button>
            );
          })}

          <PopoverTrigger asChild>
            <button
              type="button"
              onClick={onOpenCustomPicker}
              className={cn(
                "inline-flex h-10 shrink-0 items-center gap-2 rounded-xl px-4 text-xl font-semibold tracking-tight transition-colors",
                value.period === "custom"
                  ? "bg-background text-foreground shadow"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              <CalendarDaysIcon className="size-5" />
              <span>{toCustomLabel(value)}</span>
            </button>
          </PopoverTrigger>
        </div>

        <PopoverContent align="end" className="w-auto max-w-[calc(100vw-2rem)] p-3">
          <div className="grid gap-3">
            <div className="rounded-lg border border-border/80 bg-muted/30">
              <Calendar
                mode="range"
                numberOfMonths={2}
                selected={draftRange}
                onSelect={(range) => setDraftRange(range ?? { from: undefined, to: undefined })}
                defaultMonth={draftRange?.from}
                disabled={{ after: new Date() }}
                weekStartsOn={0}
              />
            </div>

            <div className="flex items-center justify-end gap-2">
              <Button variant="outline" onClick={cancelCustomRange}>Cancel</Button>
              <Button onClick={applyCustomRange} disabled={!draftRange?.from || !draftRange?.to}>
                Apply
              </Button>
            </div>
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}
