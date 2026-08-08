import { useMutation, useQueryClient } from "@tanstack/react-query";
import { DownloadIcon, Loader2Icon, RefreshCwIcon, SearchIcon, XIcon } from "lucide-react";
import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";

import { IntegrationProblem } from "@/api/integrations";
import {
  grabRadarrRelease,
  RadarrKeys,
  searchRadarrReleases,
  type RadarrAcquisition,
  type RadarrRelease,
} from "@/api/radarr";

import { formatBytes } from "@/components/moviepickarr/admin/radarr";
import { RadarrDisclosure } from "@/components/moviepickarr/admin/RadarrDisclosure";
import { Modal } from "@/components/moviepickarr/Modal";

type ReleaseSort = "score" | "quality" | "age" | "size" | "peers" | "indexer";

interface ScoreTooltipPlacement {
  left: number;
  top: number;
}

const SCORE_TOOLTIP_GAP = 7;
const SCORE_TOOLTIP_MARGIN = 8;

function ageLabel(hours?: number) {
  if (hours === undefined || !Number.isFinite(hours) || hours < 0) return "Age unknown";
  if (hours < 1) return "Less than 1 hr";
  if (hours < 24) {
    const rounded = Math.max(1, Math.floor(hours));
    return `${rounded} hr`;
  }
  const days = Math.floor(hours / 24);
  if (days < 365) return `${days} day${days === 1 ? "" : "s"}`;
  const years = Math.floor((days / 365) * 10) / 10;
  return `${years.toFixed(years % 1 === 0 ? 0 : 1)} year${years === 1 ? "" : "s"}`;
}

function unique(values: Array<string | undefined>) {
  return [...new Set(values.filter((value): value is string => Boolean(value)))].sort((a, b) => a.localeCompare(b));
}

function sortedReleases(releases: RadarrRelease[], sort: ReleaseSort) {
  return [...releases].sort((a, b) => {
    if (sort === "score") return (b.customFormatScore ?? 0) - (a.customFormatScore ?? 0);
    if (sort === "age") return (a.ageHours ?? Number.MAX_SAFE_INTEGER) - (b.ageHours ?? Number.MAX_SAFE_INTEGER);
    if (sort === "size") return (b.size ?? 0) - (a.size ?? 0);
    if (sort === "peers") return (b.peers ?? 0) - (a.peers ?? 0);
    return String(a[sort] ?? "").localeCompare(String(b[sort] ?? ""));
  });
}

function ReleaseScore({ release }: { release: RadarrRelease }) {
  const formats = release.customFormats?.filter(Boolean) ?? [];
  const score = release.customFormatScore ?? 0;
  const [mode, setMode] = useState<"closed" | "transient" | "pinned">("closed");
  const [placement, setPlacement] = useState<ScoreTooltipPlacement | null>(null);
  const focused = useRef(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const tooltipRef = useRef<HTMLSpanElement>(null);
  const tooltipID = useId();
  const open = mode !== "closed";

  const place = useCallback(() => {
    const trigger = triggerRef.current;
    const tooltip = tooltipRef.current;
    if (!trigger || !tooltip) return;

    const triggerRect = trigger.getBoundingClientRect();
    const tooltipRect = tooltip.getBoundingClientRect();
    let top = triggerRect.bottom + SCORE_TOOLTIP_GAP;
    if (
      top + tooltipRect.height > window.innerHeight - SCORE_TOOLTIP_MARGIN &&
      triggerRect.top - SCORE_TOOLTIP_GAP - tooltipRect.height >= SCORE_TOOLTIP_MARGIN
    ) {
      top = triggerRect.top - SCORE_TOOLTIP_GAP - tooltipRect.height;
    }
    const maximumTop = Math.max(
      SCORE_TOOLTIP_MARGIN,
      window.innerHeight - tooltipRect.height - SCORE_TOOLTIP_MARGIN,
    );
    top = Math.min(maximumTop, Math.max(SCORE_TOOLTIP_MARGIN, top));
    const left = Math.max(
      SCORE_TOOLTIP_MARGIN,
      Math.min(
        triggerRect.left + (triggerRect.width - tooltipRect.width) / 2,
        window.innerWidth - tooltipRect.width - SCORE_TOOLTIP_MARGIN,
      ),
    );
    setPlacement((current) => current?.top === top && current.left === left ? current : { left, top });
  }, []);

  useLayoutEffect(() => {
    if (open) place();
  }, [open, place]);

  useEffect(() => {
    if (!open) return;
    let frame: number | null = null;
    const reposition = () => {
      if (frame !== null) return;
      frame = window.requestAnimationFrame(() => {
        frame = null;
        place();
      });
    };
    window.addEventListener("scroll", reposition, true);
    window.addEventListener("resize", reposition);
    return () => {
      if (frame !== null) window.cancelAnimationFrame(frame);
      window.removeEventListener("scroll", reposition, true);
      window.removeEventListener("resize", reposition);
    };
  }, [open, place]);

  useEffect(() => {
    if (mode !== "pinned") return;
    const dismissOutside = (event: PointerEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      if (triggerRef.current?.contains(target) || tooltipRef.current?.contains(target)) return;
      setMode("closed");
    };
    document.addEventListener("pointerdown", dismissOutside, true);
    return () => document.removeEventListener("pointerdown", dismissOutside, true);
  }, [mode]);

  if (formats.length === 0) return score;

  return (
    <span
      className="radarr-release__score-wrap"
      data-open={open ? true : undefined}
      onMouseEnter={() => setMode((current) => current === "closed" ? "transient" : current)}
      onMouseLeave={() => {
        if (!focused.current) setMode((current) => current === "transient" ? "closed" : current);
      }}
    >
      <button
        ref={triggerRef}
        type="button"
        className="radarr-release__score"
        aria-label={`Score ${score}. Show applied custom formats`}
        aria-expanded={open}
        aria-describedby={open ? tooltipID : undefined}
        onClick={() => setMode((current) => current === "pinned" ? "closed" : "pinned")}
        onFocus={() => {
          focused.current = true;
          setMode((current) => current === "closed" ? "transient" : current);
        }}
        onBlur={() => {
          focused.current = false;
          setMode((current) => current === "pinned" ? current : "closed");
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            event.stopPropagation();
            setMode("closed");
          } else if (event.key === "Tab") {
            setMode("closed");
          }
        }}
      >
        {score}
      </button>
      {createPortal(
        <span
          ref={tooltipRef}
          id={tooltipID}
          className="radarr-release__formats-tooltip"
          role="tooltip"
          hidden={!open}
          data-pinned={mode === "pinned" ? true : undefined}
          style={{
            left: placement?.left ?? 0,
            top: placement?.top ?? 0,
            visibility: open && placement ? "visible" : "hidden",
          }}
        >
          <strong>Custom formats</strong>
          <span>{formats.join(" · ")}</span>
        </span>,
        document.body,
      )}
    </span>
  );
}

function ReleaseRow({
  busy,
  grabbing,
  onGrab,
  release,
}: {
  busy: boolean;
  grabbing: boolean;
  onGrab: (release: RadarrRelease) => void;
  release: RadarrRelease;
}) {
  const lowPeers = typeof release.peers === "number" && Number.isFinite(release.peers) && release.peers >= 0 && release.peers <= 5
    ? release.peers
    : undefined;
  const actionLabel = release.rejected
    ? `Review ${release.title} before download`
    : `Download ${release.title}`;

  return (
    <li className="radarr-release">
      <div className="radarr-release__title">
        <strong>{release.title}</strong>
        <span className="radarr-release__meta">
          <span>{release.indexer ?? "Unknown indexer"}</span>
          {release.protocol ? <span>{release.protocol}</span> : null}
          {lowPeers !== undefined ? <span className="radarr-release__peer-warning">{lowPeers} peer{lowPeers === 1 ? "" : "s"}</span> : null}
        </span>
      </div>
      <dl className="radarr-release__facts">
        <div><dt>Quality</dt><dd>{release.quality ?? "Unknown"}</dd></div>
        <div><dt>Score</dt><dd><ReleaseScore release={release} /></dd></div>
        <div><dt>Size</dt><dd>{formatBytes(release.size)}</dd></div>
        <div><dt>Age</dt><dd>{ageLabel(release.ageHours)}</dd></div>
      </dl>
      <button
        type="button"
        className="iconbtn radarr-release__download"
        disabled={busy || release.grabAllowed === false}
        aria-label={actionLabel}
        title={actionLabel}
        onClick={() => onGrab(release)}
      >
        {grabbing ? <Loader2Icon className="animate-spin mg-spin" aria-hidden="true" /> : <DownloadIcon aria-hidden="true" />}
      </button>
    </li>
  );
}

export function RadarrReleasePicker({
  acquisition,
  onClose,
}: {
  acquisition: RadarrAcquisition;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const initialSearchStarted = useRef(false);
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<ReleaseSort>("score");
  const [quality, setQuality] = useState("");
  const [protocol, setProtocol] = useState("");
  const [indexer, setIndexer] = useState("");
  const [override, setOverride] = useState<RadarrRelease | null>(null);
  const [feedback, setFeedback] = useState("");
  const available = !acquisition.activeQueue && (
    acquisition.status === "needs_release" ||
    (acquisition.status === "action_needed" && [
      "release_required",
      "no_releases",
      "release_failed",
      "import_failed",
    ].includes(acquisition.actionReason ?? ""))
  );
  useEffect(() => {
    if (!available) onClose();
  }, [available, onClose]);
  const refreshAcquisition = () => {
    void queryClient.invalidateQueries({ queryKey: RadarrKeys.acquisition(acquisition.id) });
    void queryClient.invalidateQueries({ queryKey: RadarrKeys.acquisitions() });
    void queryClient.invalidateQueries({ queryKey: RadarrKeys.attention() });
  };
  const search = useMutation({
    mutationFn: () => searchRadarrReleases(acquisition.id),
    onMutate: () => setFeedback(""),
    onSuccess: () => setFeedback(""),
    onError: (error) => setFeedback(
      error instanceof IntegrationProblem ? error.message : "Radarr release search failed.",
    ),
    onSettled: refreshAcquisition,
  });
  const { mutate: runSearch } = search;
  useEffect(() => {
    if (!available || initialSearchStarted.current) return;
    const timer = window.setTimeout(() => {
      if (initialSearchStarted.current) return;
      initialSearchStarted.current = true;
      runSearch();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [available, runSearch]);
  const grab = useMutation({
    mutationFn: ({ release, allowRejected }: { release: RadarrRelease; allowRejected: boolean }) =>
      grabRadarrRelease(acquisition.id, release.id, allowRejected),
    onSuccess: (next) => {
      queryClient.setQueryData(RadarrKeys.acquisition(acquisition.id), next);
      setOverride(null);
      onClose();
    },
    onError: (error) => {
      setOverride(null);
      setFeedback(
        error instanceof IntegrationProblem && error.status === 404
          ? "This search result expired. Search again and choose a fresh result."
          : error instanceof IntegrationProblem
            ? error.message
        : "The release could not be grabbed.",
      );
    },
    onSettled: refreshAcquisition,
  });
  const releases = useMemo(() => search.data ?? [], [search.data]);
  const qualities = unique(releases.map((release) => release.quality));
  const protocols = unique(releases.map((release) => release.protocol));
  const indexers = unique(releases.map((release) => release.indexer));
  const visible = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase();
    return sortedReleases(
      releases.filter((release) =>
        release.mapped !== false &&
        (!needle || release.title.toLocaleLowerCase().includes(needle)) &&
        (!quality || release.quality === quality) &&
        (!protocol || release.protocol === protocol) &&
        (!indexer || release.indexer === indexer),
      ),
      sort,
    );
  }, [indexer, protocol, quality, query, releases, sort]);
  const approved = visible.filter((release) => !release.rejected);
  const rejected = visible.filter((release) => release.rejected);
  const busy = search.isPending || grab.isPending;
  const startGrab = (release: RadarrRelease) => {
    if (release.rejected) setOverride(release);
    else grab.mutate({ release, allowRejected: false });
  };

  return (
    <>
      <Modal
        label="Choose a Radarr release"
        className="modal--radarr-releases"
        capped
        dismissible={!grab.isPending}
        onClose={onClose}
      >
        {(close) => (
          <>
            <header className="radarr-modal__head radarr-modal__head--with-close">
              <div>
                <h3>Choose a release</h3>
                <p>{acquisition.title || acquisition.identity?.title || "Current acquisition"}</p>
              </div>
              <div className="radarr-modal__head-actions">
                <button
                  type="button"
                  className="iconbtn"
                  aria-label={search.isPending ? "Searching releases" : "Search releases again"}
                  title={search.isPending ? "Searching releases" : "Search releases again"}
                  disabled={busy}
                  onClick={() => runSearch()}
                >
                  {search.isPending ? <Loader2Icon className="animate-spin mg-spin" aria-hidden="true" /> : <RefreshCwIcon aria-hidden="true" />}
                </button>
                <button type="button" className="iconbtn" aria-label="Close" disabled={grab.isPending} onClick={close}>
                  <XIcon aria-hidden="true" />
                </button>
              </div>
            </header>
            <div className="modal__scroll radarr-modal__scroll">
              {search.isPending ? <p className="radarr-release-summary" role="status">Searching Radarr…</p> : null}
              {!search.isPending && releases.length > 0 ? <p className="radarr-release-summary">{visible.length} matched release{visible.length === 1 ? "" : "s"}</p> : null}
              {releases.length > 0 ? (
                <div className="radarr-release-filters" aria-label="Release filters">
                  <label className="field radarr-release-filters__search">
                    <SearchIcon aria-hidden="true" />
                    <span className="vis-hidden">Filter releases</span>
                    <input value={query} placeholder="Filter titles" onChange={(event) => setQuery(event.target.value)} />
                  </label>
                  <label className="field"><span className="vis-hidden">Quality</span><select value={quality} onChange={(event) => setQuality(event.target.value)}><option value="">All qualities</option>{qualities.map((value) => <option key={value}>{value}</option>)}</select></label>
                  <label className="field"><span className="vis-hidden">Protocol</span><select value={protocol} onChange={(event) => setProtocol(event.target.value)}><option value="">All protocols</option>{protocols.map((value) => <option key={value}>{value}</option>)}</select></label>
                  <label className="field"><span className="vis-hidden">Indexer</span><select value={indexer} onChange={(event) => setIndexer(event.target.value)}><option value="">All indexers</option>{indexers.map((value) => <option key={value}>{value}</option>)}</select></label>
                  <label className="field"><span className="vis-hidden">Sort releases</span><select value={sort} onChange={(event) => setSort(event.target.value as ReleaseSort)}><option value="score">Score</option><option value="quality">Quality</option><option value="age">Age</option><option value="size">Size</option><option value="peers">Peers</option><option value="indexer">Indexer</option></select></label>
                </div>
              ) : null}
              {feedback ? <p className="radarr-feedback" role="alert">{feedback}</p> : null}
              {search.data && releases.length === 0 ? (
                <p className="radarr-empty">No mapped releases were found.</p>
              ) : null}
              {releases.length > 0 && approved.length === 0 && rejected.length === 0 ? (
                <p className="radarr-empty">No mapped releases match these filters.</p>
              ) : null}
              {approved.length > 0 ? (
                <ul className="radarr-release-list" aria-label="Approved releases">
                  {approved.map((release) => <ReleaseRow key={release.id} release={release} busy={busy} grabbing={grab.isPending && grab.variables?.release.id === release.id} onGrab={startGrab} />)}
                </ul>
              ) : null}
              {rejected.length > 0 ? (
                <RadarrDisclosure
                  className="radarr-disclosure--rejected"
                  title="Rejected releases"
                  meta={rejected.length}
                >
                  <ul className="radarr-release-list" aria-label="Rejected releases">
                    {rejected.map((release) => <ReleaseRow key={release.id} release={release} busy={busy} grabbing={grab.isPending && grab.variables?.release.id === release.id} onGrab={startGrab} />)}
                  </ul>
                </RadarrDisclosure>
              ) : null}
            </div>
          </>
        )}
      </Modal>

      {override ? (
        <Modal
          label="Grab rejected release?"
          className="modal--form"
          dismissible={!grab.isPending}
          onClose={() => setOverride(null)}
        >
          {(close) => (
            <div className="adm-sheet">
              <h3 className="adm-modal__title">Grab rejected release?</h3>
              <p className="adm-modal__sub">Radarr rejected this matched release for the following reasons:</p>
              <ul className="radarr-rejection-reasons">
                {(override.rejections?.length
                  ? override.rejections
                  : ["Radarr policy rejected this release."]
                ).map((reason) => <li key={reason}>{reason}</li>)}
              </ul>
              <div className="adm-modal__actions">
                <button type="button" className="btn btn--ghost" disabled={grab.isPending} onClick={close}>Cancel</button>
                <button type="button" className="btn btn--accent" disabled={grab.isPending} onClick={() => grab.mutate({ release: override, allowRejected: true })}>
                  {grab.isPending ? "Grabbing…" : "Grab anyway"}
                </button>
              </div>
            </div>
          )}
        </Modal>
      ) : null}
    </>
  );
}
