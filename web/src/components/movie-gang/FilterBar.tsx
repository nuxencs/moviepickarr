import { CheckIcon, ChevronDownIcon, SearchIcon, XIcon } from "lucide-react";
import {
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";

import {
  type FilterOptions,
  type MovieFilters,
  type PersonFilter,
  type PersonOption,
} from "@/components/movie-gang/lib";
import { exitDelayMs } from "@/components/movie-gang/Modal";

export interface FilterChoice<T extends string | number> {
  value: T;
  label: string;
  /** Row-styling hint for grouped lists: a selectable decade header vs. an
   *  indented year beneath it (the Release-year dropdown). */
  kind?: "decade" | "year";
}

type CloseReason = "select" | "escape" | "tab" | "outside" | "trigger";

/** People lists at least this long get the inline search field. */
const SEARCHABLE_FROM = 9;
/** Mirrors the backend's statsMaxPeopleFilterIDs — the stats endpoint rejects
 *  longer id lists, so the UI stops adding instead of erroring the page. */
const MAX_SELECTED = 25;

/**
 * Shared chip-trigger dropdown machinery behind FilterSelect/FilterMultiSelect
 * — the dropdown reuses the `.mg-menu` surface and mg-scaleIn/Out motion, but
 * (unlike the portalled Menu.tsx) anchors to its chip purely in CSS: it shares
 * the trigger's coordinate space, so it rides the large-screen `:root` zoom ramp
 * (§13) without the JS getBoundingClientRect placement that drifts under zoom.
 * Where supported it flips to the chip's right edge near the viewport edge. Long
 * lists opt into an inline `.field` search. Gold tint on the chip = active state.
 */
function FilterChipMenu<T extends string | number>({
  label,
  chipLabel,
  active,
  choices,
  isSelected,
  onPick,
  onClear,
  closeOnPick,
  multiselectable = false,
  searchable = false,
}: {
  label: string;
  /** Full trigger text (the wrappers bake the active value/count into it). */
  chipLabel: string;
  active: boolean;
  choices: FilterChoice<T>[];
  isSelected: (value: T) => boolean;
  onPick: (value: T) => void;
  onClear: () => void;
  /** Single-select closes on pick; multi-select stays open for more toggles. */
  closeOnPick: boolean;
  multiselectable?: boolean;
  searchable?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [closing, setClosing] = useState(false);
  const [query, setQuery] = useState("");

  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const closingRef = useRef(false);
  const timerRef = useRef<number | null>(null);
  const menuId = useId();
  // Unique anchor ident so the dropdown can tether to THIS chip — and flip to
  // its right edge on narrow screens — via CSS anchor positioning where it is
  // supported. Older engines fall back to the plain left-aligned drop (below).
  const anchorName = `--chip-anchor-${menuId.replace(/[^a-zA-Z0-9]/g, "")}`;

  const q = query.trim().toLowerCase();
  const visible = q ? choices.filter((c) => c.label.toLowerCase().includes(q)) : choices;

  const clearTimer = () => {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  };

  const openMenu = useCallback(() => {
    clearTimer();
    closingRef.current = false;
    setClosing(false);
    setQuery("");
    setOpen(true);
  }, []);

  const requestClose = useCallback((reason: CloseReason) => {
    if (closingRef.current) return;
    closingRef.current = true;
    setClosing(true);
    // Mirror the Menu: every dismissal but an outside click refocuses the chip.
    if (reason !== "outside") triggerRef.current?.focus();
    clearTimer();
    timerRef.current = window.setTimeout(() => {
      closingRef.current = false;
      setClosing(false);
      setOpen(false);
    }, exitDelayMs());
  }, []);

  // Focus the search field (when present) or the selected/first option on open.
  useLayoutEffect(() => {
    if (!open || closing) return;
    const menu = menuRef.current;
    const field = menu?.querySelector<HTMLInputElement>("input");
    if (field) {
      field.focus();
      return;
    }
    (
      menu?.querySelector<HTMLButtonElement>('[role="option"][aria-selected="true"]') ??
      menu?.querySelector<HTMLButtonElement>('[role="option"]:not([disabled])')
    )?.focus();
  }, [open, closing]);

  // Dismiss on Esc or outside click. The menu is CSS-anchored to its chip, so
  // it tracks the trigger through scroll/resize/zoom with no JS repositioning.
  useEffect(() => {
    if (!open || closing) return;

    const onPointerDown = (e: PointerEvent) => {
      const node = e.target as Node;
      if (menuRef.current?.contains(node) || triggerRef.current?.contains(node)) return;
      requestClose("outside");
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        requestClose("escape");
      }
    };

    document.addEventListener("pointerdown", onPointerDown, true);
    document.addEventListener("keydown", onKeyDown, true);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown, true);
      document.removeEventListener("keydown", onKeyDown, true);
    };
  }, [open, closing, requestClose]);

  useEffect(() => () => clearTimer(), []);

  const pick = (next: T) => {
    onPick(next);
    if (closeOnPick) requestClose("select");
  };

  const onMenuKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (!["ArrowDown", "ArrowUp", "Home", "End", "Tab"].includes(e.key)) return;
    const items = Array.from(
      menuRef.current?.querySelectorAll<HTMLButtonElement>('[role="option"]:not([disabled])') ?? [],
    );
    if (e.key === "Tab") {
      e.preventDefault();
      requestClose("tab");
      return;
    }
    if (items.length === 0) return;
    const current = items.indexOf(document.activeElement as HTMLButtonElement);
    // From the search field, ArrowDown enters the list; arrows otherwise cycle.
    if (current === -1) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        items[e.key === "ArrowDown" ? 0 : items.length - 1]?.focus();
      }
      return;
    }
    e.preventDefault();
    const next =
      e.key === "Home"
        ? 0
        : e.key === "End"
          ? items.length - 1
          : e.key === "ArrowDown"
            ? (current + 1) % items.length
            : (current - 1 + items.length) % items.length;
    items[next]?.focus();
  };

  return (
    // The wrapper owns positioning; the inner `.filterchip` keeps its
    // overflow-clipped rounded pill, which would otherwise crop the dropdown.
    <span className="filterchip-wrap" style={{ "--chip-anchor": anchorName } as CSSProperties}>
      <span className="filterchip" data-active={active}>
        <button
          ref={triggerRef}
          type="button"
          aria-haspopup="listbox"
          aria-expanded={open}
          aria-controls={open ? menuId : undefined}
          disabled={choices.length === 0 && !active}
          onClick={() => (open && !closingRef.current ? requestClose("trigger") : openMenu())}
          onKeyDown={(e) => {
            if (!open && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
              e.preventDefault();
              openMenu();
            }
          }}
        >
          {chipLabel}
          <ChevronDownIcon />
        </button>
        {active && (
          <button
            type="button"
            className="filterchip__clear"
            aria-label={`Clear ${label.toLowerCase()} filter`}
            onClick={() => {
              onClear();
              // Clearing unmounts this button — hand focus to the trigger so
              // keyboard users don't drop to <body>.
              triggerRef.current?.focus();
            }}
          >
            <XIcon />
          </button>
        )}
      </span>

      {/* Anchored to the chip in CSS (no portal) so it shares the trigger's
          coordinate space and rides the :root zoom ramp (§13) without JS
          placement drift. The listbox role sits on the option list itself — not
          this surface — so the search combobox and empty-state copy are not
          announced as stray "options". */}
      {open && (
        <div
          ref={menuRef}
          className={`mg-menu filtermenu${closing ? " mg-menu--closing" : ""}`}
          onKeyDown={onMenuKeyDown}
        >
          {searchable && (
            <label className="field">
              <SearchIcon />
              <input
                role="combobox"
                aria-expanded="true"
                aria-controls={menuId}
                aria-autocomplete="list"
                placeholder={`Search ${label.toLowerCase()}…`}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </label>
          )}
          <div
            className="filtermenu__list"
            id={menuId}
            role="listbox"
            aria-label={`Filter by ${label.toLowerCase()}`}
            aria-multiselectable={multiselectable || undefined}
          >
            {visible.map((choice) => {
              const selected = isSelected(choice.value);
              return (
                <button
                  key={choice.value}
                  type="button"
                  role="option"
                  aria-selected={selected}
                  className={`mg-menu__item${choice.kind ? ` filtermenu__item--${choice.kind}` : ""}`}
                  onClick={() => pick(choice.value)}
                >
                  {multiselectable && (
                    // Fixed-width check slot: selection reads as a mark, not
                    // color alone, and labels don't shift as it toggles.
                    <span className="filtermenu__check" aria-hidden="true">
                      {selected && <CheckIcon />}
                    </span>
                  )}
                  {choice.label}
                </button>
              );
            })}
          </div>
          {visible.length === 0 && <p className="filtermenu__empty">No matches</p>}
        </div>
      )}
    </span>
  );
}

/**
 * Chip-trigger single-select filter. Selecting the active option (or the
 * chip's clear X) clears the filter.
 */
export function FilterSelect<T extends string | number>({
  label,
  value,
  valueLabel,
  choices,
  onChange,
  searchable = false,
}: {
  label: string;
  value: T | null;
  /** Chip label for the active value when it is no longer among the choices
   *  (e.g. the filtered person dropped out of a refetched list). */
  valueLabel?: string;
  choices: FilterChoice<T>[];
  onChange: (value: T | null) => void;
  searchable?: boolean;
}) {
  const active = value !== null;
  const activeLabel = active
    ? choices.find((c) => c.value === value)?.label ?? valueLabel ?? String(value)
    : null;

  return (
    <FilterChipMenu
      label={label}
      chipLabel={active ? `${label} · ${activeLabel}` : label}
      active={active}
      choices={choices}
      searchable={searchable}
      isSelected={(v) => v === value}
      // Re-selecting the active option toggles the filter off.
      onPick={(v) => onChange(v === value ? null : v)}
      onClear={() => onChange(null)}
      closeOnPick
    />
  );
}

/**
 * Chip-trigger multi-select filter (the Actors/Crew people lists). Picking
 * toggles membership and keeps the menu open for more; the chip shows the
 * lone selection's name or a count ("Actors · 2"); the clear X clears the
 * whole group.
 */
export function FilterMultiSelect<T extends string | number>({
  label,
  values,
  valueLabels,
  choices,
  onChange,
  searchable = false,
}: {
  label: string;
  values: T[];
  /** Labels for selected values no longer among the choices (see valueLabel). */
  valueLabels?: ReadonlyMap<T, string>;
  choices: FilterChoice<T>[];
  onChange: (values: T[]) => void;
  searchable?: boolean;
}) {
  const active = values.length > 0;
  const labelFor = (v: T) =>
    choices.find((c) => c.value === v)?.label ?? valueLabels?.get(v) ?? String(v);
  const chipLabel = !active
    ? label
    : values.length === 1
      ? `${label} · ${labelFor(values[0])}`
      : `${label} · ${values.length}`;

  return (
    <FilterChipMenu
      label={label}
      chipLabel={chipLabel}
      active={active}
      choices={choices}
      searchable={searchable}
      multiselectable
      isSelected={(v) => values.includes(v)}
      onPick={(v) => {
        if (values.includes(v)) {
          onChange(values.filter((x) => x !== v));
        } else if (values.length < MAX_SELECTED) {
          onChange([...values, v]);
        }
      }}
      onClear={() => onChange([])}
      closeOnPick={false}
    />
  );
}

/** Decade floor of a year — 1994 ⇒ 1990. */
const decadeOf = (year: number) => Math.floor(year / 10) * 10;

/**
 * The Release-year chip: a single-select dropdown that groups the available
 * years under selectable decade headers. Picking a decade ("1990s") filters the
 * whole decade; picking a year filters that exact year; the two are mutually
 * exclusive — picking one clears the other, and re-picking the active value
 * clears it. Values are kind-tagged (`d:`/`y:`) so a decade and a same-numbered
 * year never collide in the listbox.
 */
function ReleaseYearSelect({
  label,
  years,
  year,
  decade,
  onChange,
}: {
  label: string;
  /** Available release years, newest-first (as `filterOptionsFrom` returns). */
  years: number[];
  year: number | null;
  decade: number | null;
  onChange: (next: { year: number | null; decade: number | null }) => void;
}) {
  // Walk the newest-first years, emitting a decade header each time the decade
  // changes — so both decades and their years stay descending.
  const choices: FilterChoice<string>[] = [];
  let lastDecade: number | null = null;
  for (const y of years) {
    const d = decadeOf(y);
    if (d !== lastDecade) {
      lastDecade = d;
      choices.push({ value: `d:${d}`, label: `${d}s`, kind: "decade" });
    }
    choices.push({ value: `y:${y}`, label: String(y), kind: "year" });
  }

  const active = year !== null || decade !== null;
  const activeValue = decade !== null ? `d:${decade}` : year !== null ? `y:${year}` : null;
  const activeLabel = decade !== null ? `${decade}s` : year !== null ? String(year) : null;

  return (
    <FilterChipMenu
      label={label}
      chipLabel={active ? `${label} · ${activeLabel}` : label}
      active={active}
      choices={choices}
      isSelected={(v) => v === activeValue}
      onPick={(v) => {
        const n = Number(v.slice(2));
        if (v.startsWith("d:")) {
          onChange({ decade: decade === n ? null : n, year: null });
        } else {
          onChange({ year: year === n ? null : n, decade: null });
        }
      }}
      onClear={() => onChange({ year: null, decade: null })}
      closeOnPick
    />
  );
}

/**
 * The stats filter chips — Genre / Actors / Crew / Release year — bound to a
 * `MovieFilters` value. `children` renders as extra leading chips (the stats
 * watch-year quick-select); `yearLabel` lets stats relabel its chip "Release
 * year" beside it.
 */
export function FilterBar({
  options,
  value,
  onChange,
  yearLabel = "Year",
  className,
  children,
}: {
  options: FilterOptions;
  value: MovieFilters;
  onChange: (filters: MovieFilters) => void;
  yearLabel?: string;
  className?: string;
  children?: ReactNode;
}) {
  const personChoices = (people: PersonOption[]): FilterChoice<number>[] =>
    people.map((p) => ({ value: p.id, label: p.name }));

  // Map picked ids back to {id, name} pairs, preferring the option list and
  // falling back to the already-selected entry (so names survive a person
  // dropping out of a refetched list).
  const toPersonFilters = (ids: number[], people: PersonOption[], selected: PersonFilter[]) =>
    ids.map((id) => ({
      id,
      name:
        people.find((p) => p.id === id)?.name ??
        selected.find((p) => p.id === id)?.name ??
        String(id),
    }));

  return (
    <div className={`filterbar${className ? ` ${className}` : ""}`}>
      {children}
      <FilterSelect
        label="Genre"
        value={value.genre}
        choices={options.genres.map((g) => ({ value: g, label: g }))}
        onChange={(genre) => onChange({ ...value, genre })}
      />
      <FilterMultiSelect
        label="Actors"
        searchable={options.actors.length >= SEARCHABLE_FROM}
        values={value.actors.map((p) => p.id)}
        valueLabels={new Map(value.actors.map((p) => [p.id, p.name]))}
        choices={personChoices(options.actors)}
        onChange={(ids) => onChange({ ...value, actors: toPersonFilters(ids, options.actors, value.actors) })}
      />
      <FilterMultiSelect
        label="Crew"
        searchable={options.crew.length >= SEARCHABLE_FROM}
        values={value.crew.map((p) => p.id)}
        valueLabels={new Map(value.crew.map((p) => [p.id, p.name]))}
        choices={personChoices(options.crew)}
        onChange={(ids) => onChange({ ...value, crew: toPersonFilters(ids, options.crew, value.crew) })}
      />
      <FilterMultiSelect
        label="Picked by"
        searchable={options.pickers.length >= SEARCHABLE_FROM}
        values={value.pickers.map((p) => p.id)}
        valueLabels={new Map(value.pickers.map((p) => [p.id, p.name]))}
        choices={personChoices(options.pickers)}
        onChange={(ids) => onChange({ ...value, pickers: toPersonFilters(ids, options.pickers, value.pickers) })}
      />
      <ReleaseYearSelect
        label={yearLabel}
        years={options.years}
        year={value.year}
        decade={value.decade}
        onChange={({ year, decade }) => onChange({ ...value, year, decade })}
      />
    </div>
  );
}
