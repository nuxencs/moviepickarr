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
  /** Row style in the Release-year list: decade header or indented year. */
  kind?: "decade" | "year";
}

type CloseReason = "select" | "escape" | "tab" | "outside" | "trigger";

/** People lists at least this long get the inline search field. */
const SEARCHABLE_FROM = 9;
/** Estimate only; rendered options are measured. */
const OPTION_HEIGHT = 32;
/** In step with backend statsMaxPeopleFilterIDs: longer lists get rejected. */
const MAX_SELECTED = 25;

/**
 * Chip-trigger dropdown behind the filter selects. Unlike the portalled
 * Menu.tsx, it anchors to its chip in CSS, with no JS placement.
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
  chipLabel: string;
  active: boolean;
  choices: FilterChoice<T>[];
  isSelected: (value: T) => boolean;
  onSelect: (value: T) => void;
  onClear: () => void;
  closeOnSelect: boolean;
  multiselectable?: boolean;
  searchable?: boolean;
}) {
  const [query, setQuery] = useState("");
  // Roving focus by index (-1 is the search field), since a virtualized option
  // may not be rendered yet.
  const [activeIndex, setActiveIndex] = useState(-1);
  const pendingFocus = useRef(false);

  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const menuId = useId();

  const { open, closing, show, dismiss, isTopmost } = useDismissible({ restoreFocusTo: triggerRef });
  // Per-chip CSS anchor; engines without anchor positioning get a left-aligned drop.
  const anchorName = `--chip-anchor-${menuId.replace(/[^a-zA-Z0-9]/g, "")}`;

  // Searchable lists can hold 1500+ people: defer the query, virtualize the rows.
  const deferredQuery = useDeferredValue(query);
  const matches = useMemo(() => filterChoices(choices, deferredQuery), [choices, deferredQuery]);

  const virtualizer = useVirtualizer({
    count: matches.length,
    getScrollElement: () => listRef.current,
    estimateSize: () => OPTION_HEIGHT,
    overscan: 8,
  });
  const rendered = virtualizer.getVirtualItems();

  // Focus lands in the effect below, once the virtualizer renders the option.
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
    // A mouse scroll can unmount the focused option, dropping focus to <body>
    // out of reach of the menu's keys. A click elsewhere is left alone.
    if (activeIndex !== -1 && !option && document.activeElement === document.body) {
      list.focus();
      setActiveIndex(-1);
    }
    // `rendered` changes whenever the virtual window moves.
  }, [activeIndex, rendered]);

  const openMenu = useCallback(() => {
    setQuery("");
    setActiveIndex(-1);
    show();
  }, [show]);

  const requestClose = useCallback(
    (reason: CloseReason) => dismiss({ restoreFocus: reason !== "outside" }),
    [dismiss],
  );

  useLayoutEffect(() => {
    if (!open || closing) return;
    const field = menuRef.current?.querySelector<HTMLInputElement>("input");
    if (field) {
      field.focus();
      return;
    }
    // Not `matches`: the deferred query still holds the last session's value.
    const selectedIndex = choices.findIndex((c) => isSelected(c.value));
    focusOption(selectedIndex === -1 ? 0 : selectedIndex);
    // Only on open: re-running as the list changes would steal focus mid-typing.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, closing]);

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
    // In the search field, Home/End stay caret keys.
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
    // The wrapper positions the menu: the pill's overflow clip would crop it.
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
          <span className="filterchip__label">{chipLabel}</span>
          <ChevronDownIcon />
        </button>
        {active && (
          <button
            type="button"
            className="filterchip__clear"
            aria-label={`Clear ${label.toLowerCase()} filter`}
            onClick={() => {
              onClear();
              // Clearing unmounts this button; keep focus off <body>.
              triggerRef.current?.focus();
            }}
          >
            <XIcon />
          </button>
        )}
      </span>

      {/* The listbox role is on the list, not this surface, so the search and
          empty-state copy are not announced as options. */}
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
                  // A narrower list renumbers the options.
                  setActiveIndex(-1);
                }}
              />
            </label>
          )}
          <div
            ref={listRef}
            // Takes focus from a scrolled-away option (see the effect above).
            tabIndex={-1}
            className="filtermenu__list"
            id={menuId}
            role="listbox"
            aria-label={`Filter by ${label.toLowerCase()}`}
            aria-multiselectable={multiselectable || undefined}
          >
            {/* role="presentation": options still read as listbox children. */}
            <div
              role="presentation"
              // Else the column flex list squashes the sizer and caps the scroll.
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
                      // Fixed slot: a mark, not color alone, and labels do not shift.
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
  /** Chip label for an active value no longer among the choices. */
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
      onSelect={(v) => onChange(v === value ? null : v)}
      onClear={() => onChange(null)}
      closeOnSelect
    />
  );
}

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
  /** Labels for selected values no longer among the choices. */
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

const decadeOf = (year: number) => Math.floor(year / 10) * 10;

/**
 * Years grouped under selectable decade headers; year and decade are mutually
 * exclusive. The `d:`/`y:` tags keep decade 1990 apart from year 1990.
 */
function ReleaseYearSelect({
  label,
  years,
  year,
  decade,
  onChange,
}: {
  label: string;
  /** Newest first. */
  years: number[];
  year: number | null;
  decade: number | null;
  onChange: (next: { year: number | null; decade: number | null }) => void;
}) {
  // Memoized so the menu's filter memo survives parent renders.
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

/** The stats filter chips; `children` render as extra leading chips. */
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
  // Stable identities let each menu's filter memo survive parent renders.
  const genreChoices = useMemo(() => options.genres.map((g) => ({ value: g, label: g })), [options.genres]);
  const actorChoices = useMemo(() => personChoices(options.actors), [options.actors]);
  const crewChoices = useMemo(() => personChoices(options.crew), [options.crew]);
  const adderChoices = useMemo(() => personChoices(options.adders), [options.adders]);

  // Fall back to the selected entry, so names survive a refetch that drops a person.
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
