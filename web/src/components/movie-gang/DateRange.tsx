import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";

const DOW = ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"];
const MONTHS = [
  "January", "February", "March", "April", "May", "June",
  "July", "August", "September", "October", "November", "December",
];
const MON_SHORT = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

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

export function fmtRange(start: Date | null, end: Date | null): string {
  if (!start) return "Select a start date";
  const s = `${MON_SHORT[start.getMonth()]} ${start.getDate()}`;
  if (!end) return `${s} — …`;
  return `${s} — ${MON_SHORT[end.getMonth()]} ${end.getDate()}, ${end.getFullYear()}`;
}

/** Compact "May 5 – May 19" label for the stats eyebrow. */
export function shortRange(start?: Date | null, end?: Date | null): string | null {
  if (!start || !end) return null;
  return `${MON_SHORT[start.getMonth()]} ${start.getDate()} – ${MON_SHORT[end.getMonth()]} ${end.getDate()}`;
}

function MonthView({
  base,
  range,
  hover,
  onPick,
  onHover,
}: {
  base: Date;
  range: DayRange;
  hover: Date | null;
  onPick: (d: Date) => void;
  onHover: (d: Date) => void;
}) {
  const y = base.getFullYear();
  const m = base.getMonth();
  const cells = monthGrid(y, m);
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
              onClick={() => onPick(c.date)}
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

export function DateRangePopover({
  initial,
  onApply,
  onClose,
}: {
  initial: DayRange | null;
  onApply: (range: DayRange) => void;
  onClose: () => void;
}) {
  const [base, setBase] = useState<Date>(() =>
    initial?.start ? startOfMonth(initial.start) : startOfMonth(new Date()),
  );
  const [range, setRange] = useState<DayRange>(initial ?? { start: null, end: null });
  const [hover, setHover] = useState<Date | null>(null);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("mousedown", onDoc);
    window.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      window.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  const pick = (date: Date) => {
    setRange((r) => {
      if (!r.start || (r.start && r.end)) return { start: date, end: null };
      if (dayKey(date) < dayKey(r.start)) return { start: date, end: r.start };
      return { start: r.start, end: date };
    });
  };

  const ready = !!range.start && !!range.end;

  return (
    <div className="daterange" ref={ref}>
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
        <MonthView base={base} range={range} hover={hover} onPick={pick} onHover={setHover} />
        <MonthView base={addMonths(base, 1)} range={range} hover={hover} onPick={pick} onHover={setHover} />
      </div>
      <div className="daterange__foot">
        <span className="daterange__range">{fmtRange(range.start, range.end)}</span>
        <div className="daterange__btns">
          <button type="button" className="btn btn--ghost btn--sm" onClick={onClose}>
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
