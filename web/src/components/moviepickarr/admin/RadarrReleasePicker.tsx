import { useMutation, useQueryClient } from "@tanstack/react-query";
import { SearchIcon, XIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { IntegrationProblem } from "@/api/integrations";
import {
  grabRadarrRelease,
  RadarrKeys,
  searchRadarrReleases,
  type RadarrAcquisition,
  type RadarrRelease,
} from "@/api/radarr";

import { formatBytes } from "@/components/moviepickarr/admin/radarr";
import { Modal } from "@/components/moviepickarr/Modal";

type ReleaseSort = "score" | "quality" | "age" | "size" | "peers" | "indexer";

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

function ReleaseRow({
  busy,
  onGrab,
  release,
}: {
  busy: boolean;
  onGrab: (release: RadarrRelease) => void;
  release: RadarrRelease;
}) {
  return (
    <li className="radarr-release">
      <div className="radarr-release__title">
        <strong>{release.title}</strong>
        <span>{release.indexer ?? "Unknown indexer"}</span>
      </div>
      <dl className="radarr-release__facts">
        <div><dt>Quality</dt><dd>{release.quality ?? "Unknown"}</dd></div>
        <div><dt>Score</dt><dd>{release.customFormatScore ?? 0}</dd></div>
        <div><dt>Size</dt><dd>{formatBytes(release.size)}</dd></div>
        <div><dt>Age</dt><dd>{release.ageHours === undefined ? "Unknown" : `${Math.round(release.ageHours)} hr`}</dd></div>
        <div><dt>Peers</dt><dd>{release.peers ?? "Unknown"}</dd></div>
        <div><dt>Protocol</dt><dd>{release.protocol ?? "Unknown"}</dd></div>
      </dl>
      <button
        type="button"
        className="btn btn--ghost btn--sm"
        disabled={busy || release.grabAllowed === false}
        onClick={() => onGrab(release)}
      >
        {release.rejected ? "Review release" : "Grab release"}
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
    onSuccess: () => setFeedback(""),
    onError: (error) => setFeedback(
      error instanceof IntegrationProblem ? error.message : "Radarr release search failed.",
    ),
    onSettled: refreshAcquisition,
  });
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
        dismissible={!busy}
        onClose={onClose}
      >
        {(close) => (
          <>
            <header className="radarr-modal__head radarr-modal__head--with-close">
              <div>
                <h3>Choose a release</h3>
                <p>{acquisition.title || acquisition.identity?.title || "Current acquisition"}</p>
              </div>
              <button type="button" className="iconbtn" aria-label="Close" disabled={busy} onClick={close}>
                <XIcon aria-hidden="true" />
              </button>
            </header>
            <div className="modal__scroll radarr-modal__scroll">
              <div className="radarr-release-search">
                <button type="button" className="btn btn--accent" disabled={busy} onClick={() => search.mutate()}>
                  <SearchIcon aria-hidden="true" />
                  {search.isPending ? "Searching…" : releases.length > 0 ? "Search again" : "Search Radarr"}
                </button>
                {releases.length > 0 ? (
                  <span>{visible.length} matched release{visible.length === 1 ? "" : "s"}</span>
                ) : null}
              </div>
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
                  {approved.map((release) => <ReleaseRow key={release.id} release={release} busy={busy} onGrab={startGrab} />)}
                </ul>
              ) : null}
              {rejected.length > 0 ? (
                <details className="radarr-rejected">
                  <summary>{rejected.length} rejected release{rejected.length === 1 ? "" : "s"}</summary>
                  <ul className="radarr-release-list" aria-label="Rejected releases">
                    {rejected.map((release) => <ReleaseRow key={release.id} release={release} busy={busy} onGrab={startGrab} />)}
                  </ul>
                </details>
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
