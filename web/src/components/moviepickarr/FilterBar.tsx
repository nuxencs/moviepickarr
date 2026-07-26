import { useVirtualizer } from "@tanstack/react-virtual";
import { CheckIcon, ChevronDownIcon, SearchIcon, XIcon } from "lucide-react";
import {
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  useCallback,
  useDeferredValue,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import {
  type FilterOptions,
  type MovieFilters,
  type PersonFilter,
  type PersonOption,
} from "@/components/moviepickarr/lib";
import { filterChoices } from "@/components/moviepickarr/search";

import { useDismissible } from "@/hooks/useDismissible";
import { virtualRowStyle } from "@/hooks/useGridMetrics";

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
/** Starting row height for the virtualized option list; real heights are
 *  measured once each option renders. */
const OPTION_HEIGHT = 32;
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
  onSelect,
  onClear,
  closeOnSelect,
  multiselectable = false,
  searchable = false,
}: {
  label: string;
  /** Full trigger text (the wrappers bake the active value/count into it). */
  chipLabel: string;
  active: boolean;
  choices: FilterChoice<T>[];
  isSelected: (value: T) => boolean;
  onSelect: (value: T) => void;
  onClear: () => void;
  /** Single-select closes on select; multi-select stays open for more toggles. */
  closeOnSelect: boolean;
  multiselectable?: boolean;
  searchable?: boolean;
}) {
  const [query, setQuery] = useState("");
  // Index of the option holding roving focus; -1 while focus sits in the search
  // field. Tracked by index (not by DOM order) because virtualization means the
  // option the keyboard is moving to may not be rendered yet.
  const [activeIndex, setActiveIndex] = useState(-1);
  const pendingFocus = useRef(false);

  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const menuId = useId();

  const { open, closing, show, dismiss, isTopmost } = useDismissible({ restoreFocusTo: triggerRef });
  // Unique anchor ident so the dropdown can tether to THIS chip — and flip to
  // its right edge on narrow screens — via CSS anchor positioning where it is
  // supported. Older engines fall back to the plain left-aligned drop (below).
  const anchorName = `--chip-anchor-${menuId.replace(/[^a-zA-Z0-9]/g, "")}`;

  // The people lists that opt into search are long by definition (1500+ actors
  // on a mature library), so the query is deferred into the filter memo — typing
  // never blocks on it — and the matches render through a virtualizer, which
  // puts a screenful of option buttons in the DOM instead of all of them.
  const deferredQuery = useDeferredValue(query);
  const matches = useMemo(() => filterChoices(choices, deferredQuery), [choices, deferredQuery]);

  const virtualizer = useVirtualizer({
    count: matches.length,
    getScrollElement: () => listRef.current,
    estimateSize: () => OPTION_HEIGHT,
    overscan: 8,
  });
  const rendered = virtualizer.getVirtualItems();

  // Move roving focus to an option by index: scroll it into view, then focus it
  // once the virtualizer has rendered it (the effect below).
  const focusOption = useCallback(
    (index: number) => {
      if (index < 0 || index >= matches.length) return;
      virtualizer.scrollToIndex(index);
      setActiveIndex(index);
      pendingFocus.current = true;
    },
    [matches.length, virtualizer],
  );

  useEffect(() => {
    const list = listRef.current;
    if (!list) return;
    const option = list.querySelector<HTMLButtonElement>(`[data-index="${activeIndex}"]`);
    if (pendingFocus.current) {
      if (option) {
        option.focus();
        pendingFocus.current = false;
      }
      return;
    }
    // The focused option can be scrolled out of the rendered window by the
    // mouse, which unmounts it and would drop focus to <body> — out of reach of
    // the menu's key handling. Hand focus back to the list itself.
    // Unmounting the focused element drops focus to <body>, so that — not a
    // still-in-the-list activeElement — is what this recovers from. A click
    // elsewhere lands on a real element and is left alone.
    if (activeIndex !== -1 && !option && document.activeElement === document.body) {
      list.focus();
      setActiveIndex(-1);
    }
    // `rendered` changes identity whenever the virtual window moves, which is
    // exactly when a pending focus can land or an active option can vanish.
  }, [activeIndex, rendered]);

  const openMenu = useCallback(() => {
    setQuery("");
    setActiveIndex(-1);
    show();
  }, [show]);

  // Mirror the Menu: every dismissal but an outside click refocuses the chip.
  const requestClose = useCallback(
    (reason: CloseReason) => dismiss({ restoreFocus: reason !== "outside" }),
    [dismiss],
  );

  // Focus the search field (when present) or the selected/first option on open.
  useLayoutEffect(() => {
    if (!open || closing) return;
    const field = menuRef.current?.querySelector<HTMLInputElement>("input");
    if (field) {
      field.focus();
      return;
    }
    // `choices`, not `matches`: opening resets the query, and the deferred copy
    // still holds the last session's on this render.
    const selectedIndex = choices.findIndex((c) => isSelected(c.value));
    focusOption(selectedIndex === -1 ? 0 : selectedIndex);
    // Only on open: re-running as the list changes would steal focus mid-typing.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, closing]);

  // Dismiss on Esc or outside click. The menu is CSS-anchored to its chip, so
  // it tracks the trigger through scroll/resize/zoom with no JS repositioning.
  useEffect(() => {
    if (!open || closing) return;

    const onPointerDown = (e: PointerEvent) => {
      if (!isTopmost()) return;
      const node = e.target as Node;
      if (menuRef.current?.contains(node) || triggerRef.current?.contains(node)) return;
      requestClose("outside");
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && isTopmost()) {
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
  }, [open, closing, requestClose, isTopmost]);


  const select = (next: T) => {
    onSelect(next);
    if (closeOnSelect) requestClose("select");
  };

  const onMenuKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (!["ArrowDown", "ArrowUp", "Home", "End", "Tab"].includes(e.key)) return;
    if (e.key === "Tab") {
      e.preventDefault();
      requestClose("tab");
      return;
    }
    const last = matches.length - 1;
    if (last < 0) return;
    // From the search field (activeIndex -1) the arrows enter the list at the
    // top/bottom, while Home/End stay caret keys for the text being typed. From
    // an option, Home/End jump the list and the arrows cycle.
    if (activeIndex === -1) {
      if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
      e.preventDefault();
      return focusOption(e.key === "ArrowDown" ? 0 : last);
    }
    e.preventDefault();
    if (e.key === "Home") return focusOption(0);
    if (e.key === "End") return focusOption(last);
    focusOption(
      e.key === "ArrowDown"
        ? (activeIndex + 1) % matches.length
        : (activeIndex - 1 + matches.length) % matches.length,
    );
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
          onClick={() => (open && !closing ? requestClose("trigger") : openMenu())}
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
                name="filter-search"
                aria-label={`Search ${label.toLowerCase()}`}
                placeholder={`Search ${label.toLowerCase()}…`}
                value={query}
                onChange={(e) => {
                  setQuery(e.target.value);
                  // A narrower list renumbers the options; focus goes back to
                  // the field until the next arrow key.
                  setActiveIndex(-1);
                }}
              />
            </label>
          )}
          <div
            ref={listRef}
            // Focusable so a scrolled-away option can hand focus back here
            // rather than to <body>, keeping the arrow keys working.
            tabIndex={-1}
            className="filtermenu__list"
            id={menuId}
            role="listbox"
            aria-label={`Filter by ${label.toLowerCase()}`}
            aria-multiselectable={multiselectable || undefined}
          >
            {/* Presentational sizer: it holds the full scroll height while only
                the visible options exist, and is ignored by the a11y tree so
                the options still read as direct children of the listbox. */}
            <div
              role="presentation"
              // flexShrink: the list is a column flex box, which would otherwise
              // squash the sizer to the menu's height and cap the scroll range.
              style={{ position: "relative", flexShrink: 0, height: virtualizer.getTotalSize() }}
            >
              {rendered.map((row) => {
                const choice = matches[row.index];
                const selected = isSelected(choice.value);
                return (
                  <button
                    key={choice.value}
                    data-index={row.index}
                    ref={virtualizer.measureElement}
                    type="button"
                    role="option"
                    aria-selected={selected}
                    aria-setsize={matches.length}
                    aria-posinset={row.index + 1}
                    className={`mg-menu__item${choice.kind ? ` filtermenu__item--${choice.kind}` : ""}`}
                    style={virtualRowStyle(row.start)}
                    onClick={() => select(choice.value)}
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
          </div>
          {matches.length === 0 && <p className="filtermenu__empty">No matches</p>}
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
      onSelect={(v) => onChange(v === value ? null : v)}
      onClear={() => onChange(null)}
      closeOnSelect
    />
  );
}

/**
 * Chip-trigger multi-select filter (the Actors/Crew people lists). Choosing
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
      onSelect={(v) => {
        if (values.includes(v)) {
          onChange(values.filter((x) => x !== v));
        } else if (values.length < MAX_SELECTED) {
          onChange([...values, v]);
        }
      }}
      onClear={() => onChange([])}
      closeOnSelect={false}
    />
  );
}

const personChoices = (people: PersonOption[]): FilterChoice<number>[] =>
  people.map((p) => ({ value: p.id, label: p.name }));

/** Decade floor of a year — 1994 ⇒ 1990. */
const decadeOf = (year: number) => Math.floor(year / 10) * 10;

/**
 * The Release-year chip: a single-select dropdown that groups the available
 * years under selectable decade headers. Choosing a decade ("1990s") filters the
 * whole decade; choosing a year filters that exact year; the two are mutually
 * exclusive — choosing one clears the other, and re-choosing the active value
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
  // changes — so both decades and their years stay descending. Memoized so the
  // menu's filter memo isn't invalidated by every parent render.
  const choices = useMemo(() => {
    const out: FilterChoice<string>[] = [];
    let lastDecade: number | null = null;
    for (const y of years) {
      const d = decadeOf(y);
      if (d !== lastDecade) {
        lastDecade = d;
        out.push({ value: `d:${d}`, label: `${d}s`, kind: "decade" });
      }
      out.push({ value: `y:${y}`, label: String(y), kind: "year" });
    }
    return out;
  }, [years]);

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
      onSelect={(v) => {
        const n = Number(v.slice(2));
        if (v.startsWith("d:")) {
          onChange({ decade: decade === n ? null : n, year: null });
        } else {
          onChange({ year: year === n ? null : n, decade: null });
        }
      }}
      onClear={() => onChange({ year: null, decade: null })}
      closeOnSelect
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
  // Choice lists are memoized on their source options: a stable array identity
  // is what lets each menu's filter memo survive an unrelated parent render.
  const genreChoices = useMemo(() => options.genres.map((g) => ({ value: g, label: g })), [options.genres]);
  const actorChoices = useMemo(() => personChoices(options.actors), [options.actors]);
  const crewChoices = useMemo(() => personChoices(options.crew), [options.crew]);
  const adderChoices = useMemo(() => personChoices(options.adders), [options.adders]);

  // Map selected ids back to {id, name} pairs, preferring the option list and
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
        choices={genreChoices}
        onChange={(genre) => onChange({ ...value, genre })}
      />
      <FilterMultiSelect
        label="Actors"
        searchable={options.actors.length >= SEARCHABLE_FROM}
        values={value.actors.map((p) => p.id)}
        valueLabels={new Map(value.actors.map((p) => [p.id, p.name]))}
        choices={actorChoices}
        onChange={(ids) => onChange({ ...value, actors: toPersonFilters(ids, options.actors, value.actors) })}
      />
      <FilterMultiSelect
        label="Crew"
        searchable={options.crew.length >= SEARCHABLE_FROM}
        values={value.crew.map((p) => p.id)}
        valueLabels={new Map(value.crew.map((p) => [p.id, p.name]))}
        choices={crewChoices}
        onChange={(ids) => onChange({ ...value, crew: toPersonFilters(ids, options.crew, value.crew) })}
      />
      <FilterMultiSelect
        label="Added by"
        searchable={options.adders.length >= SEARCHABLE_FROM}
        values={value.adders.map((p) => p.id)}
        valueLabels={new Map(value.adders.map((p) => [p.id, p.name]))}
        choices={adderChoices}
        onChange={(ids) => onChange({ ...value, adders: toPersonFilters(ids, options.adders, value.adders) })}
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
