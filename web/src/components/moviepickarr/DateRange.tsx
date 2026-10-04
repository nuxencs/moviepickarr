import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react";
import { type RefObject, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

import { fmtRange } from "@/components/moviepickarr/dateRangeFormat";

const DOW = ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"];
const MONTHS = [
  "January", "February", "March", "April", "May", "June",
  "July", "August", "September", "October", "November", "December",
];

export interface DayRange {
  start: Date | null;
  end: Date | null;
}

function startOfMonth(d: Date) {
  return new Date(d.getFullYear(), d.getMonth(), 1);
}
function addMonths(d: Date, n: number) {
  return new Date(d.getFullYear(), d.getMonth() + n, 1);
}
function sameDay(a: Date | null, b: Date | null) {
  return !!a && !!b && a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
}
function dayKey(d: Date) {
  return d.getFullYear() * 10000 + d.getMonth() * 100 + d.getDate();
}
function monthGrid(year: number, month: number) {
  const startDow = new Date(year, month, 1).getDay();
  const gridStart = new Date(year, month, 1 - startDow);
  return Array.from({ length: 42 }, (_, i) => {
    const d = new Date(gridStart.getFullYear(), gridStart.getMonth(), gridStart.getDate() + i);
    return { date: d, inMonth: d.getMonth() === month };
  });
}

function MonthView({
  base,
  range,
  hover,
  onSelect,
  onHover,
}: {
  base: Date;
  range: DayRange;
  hover: Date | null;
  onSelect: (d: Date) => void;
  onHover: (d: Date) => void;
}) {
  const y = base.getFullYear();
  const m = base.getMonth();
  // Keeps the grid off the hover path, which fires on every mouse move.
  const cells = useMemo(() => monthGrid(y, m), [y, m]);
  const end = range.end || hover;
  const lo = range.start && end ? Math.min(dayKey(range.start), dayKey(end)) : null;
  const hi = range.start && end ? Math.max(dayKey(range.start), dayKey(end)) : null;

  return (
    <div className="dr-month">
      <div className="dr-month__label">
        {MONTHS[m]} {y}
      </div>
      <div className="dr-grid">
        {DOW.map((d) => (
          <div key={d} className="dr-dow">
            {d}
          </div>
        ))}
        {cells.map((c, i) => {
          const k = dayKey(c.date);
          const isStart = sameDay(c.date, range.start);
          const isEnd = !!range.end && sameDay(c.date, range.end);
          const inRange = lo !== null && hi !== null && k > lo && k < hi;
          const cls = ["dr-day"];
          if (!c.inMonth) cls.push("dr-day--muted");
          if (inRange) cls.push("dr-day--inrange");
          if (isStart) cls.push("dr-day--start");
          if (isEnd) cls.push("dr-day--end");
          return (
            <button
              type="button"
              key={i}
              className={cls.join(" ")}
              onClick={() => onSelect(c.date)}
              onMouseEnter={() => onHover(c.date)}
            >
              {c.date.getDate()}
            </button>
          );
        })}
      </div>
    </div>
  );
}

/**
 * Custom-range calendar popover. Shares the Menu's floating-surface lifecycle;
 * the parent keeps it mounted for `exitDelayMs()` while it animates out.
 */
export function DateRangePopover({
  id,
  initial,
  triggerRef,
  closing = false,
  isTopmost,
  onApply,
  onDismiss,
}: {
  id?: string;
  initial: DayRange | null;
  /** Ignored by outside-dismiss, so its own toggle does not double-fire. */
  triggerRef?: RefObject<HTMLButtonElement | null>;
  closing?: boolean;
  /** False once a surface opens on top and owns Esc and outside-clicks (#220). */
  isTopmost: () => boolean;
  onApply: (range: DayRange) => void;
  /** `restoreFocus` is false for an outside click: focus follows the click. */
  onDismiss: (restoreFocus: boolean) => void;
}) {
  const [base, setBase] = useState<Date>(() =>
    initial?.start ? startOfMonth(initial.start) : startOfMonth(new Date()),
  );
  const [range, setRange] = useState<DayRange>(initial ?? { start: null, end: null });
  const [hover, setHover] = useState<Date | null>(null);
  const ref = useRef<HTMLDivElement>(null);

  // preventScroll: focusing the absolutely positioned surface would jump the page.
  useLayoutEffect(() => {
    ref.current?.focus({ preventScroll: true });
  }, []);

  useEffect(() => {
    // Capture phase, matching Menu.tsx and FilterChipMenu.
    const onPointerDown = (e: PointerEvent) => {
      if (!isTopmost()) return;
      const node = e.target as Node;
      if (ref.current?.contains(node) || triggerRef?.current?.contains(node)) return;
      onDismiss(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && isTopmost()) {
        e.stopPropagation();
        onDismiss(true);
      }
    };
    document.addEventListener("pointerdown", onPointerDown, true);
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown, true);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [onDismiss, triggerRef, isTopmost]);

  const select = (date: Date) => {
    setRange((r) => {
      if (!r.start || (r.start && r.end)) return { start: date, end: null };
      if (dayKey(date) < dayKey(r.start)) return { start: date, end: r.start };
      return { start: r.start, end: date };
    });
  };

  const ready = !!range.start && !!range.end;

  return (
    <div
      className={`daterange${closing ? " daterange--closing" : ""}`}
      ref={ref}
      id={id}
      role="dialog"
      aria-label="Custom date range"
      tabIndex={-1}
    >
      <div className="daterange__head">
        <button type="button" className="iconbtn" onClick={() => setBase((b) => addMonths(b, -1))} aria-label="Previous month">
          <ChevronLeftIcon />
        </button>
        <span className="daterange__title">Custom range</span>
        <button type="button" className="iconbtn" onClick={() => setBase((b) => addMonths(b, 1))} aria-label="Next month">
          <ChevronRightIcon />
        </button>
      </div>
      <div className="daterange__months" onMouseLeave={() => setHover(null)}>
        <MonthView base={base} range={range} hover={hover} onSelect={select} onHover={setHover} />
        <MonthView base={addMonths(base, 1)} range={range} hover={hover} onSelect={select} onHover={setHover} />
      </div>
      <div className="daterange__foot">
        <span className="daterange__range">{fmtRange(range.start, range.end)}</span>
        <div className="daterange__btns">
          <button type="button" className="btn btn--ghost btn--sm" onClick={() => onDismiss(true)}>
            Cancel
          </button>
          <button type="button" className="btn btn--accent btn--sm" disabled={!ready} onClick={() => onApply(range)}>
            Apply
          </button>
        </div>
      </div>
    </div>
  );
}
