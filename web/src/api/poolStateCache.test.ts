import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";

import {
  applyImmediateLifecycleState,
  drawInProgressForEvent,
  setCachedDrawInProgress,
} from "@/api/poolStateCache";
import { SettingsKeys } from "@/api/query_keys";

import type { MovieDetail } from "@/types/Response";

describe("setCachedDrawInProgress", () => {
  it("patches the server draw gate without changing the round lock", () => {
    const client = new QueryClient();
    client.setQueryData(SettingsKeys.poolLock(), {
      poolLocked: true,
      drawInProgress: false,
    });

    setCachedDrawInProgress(client, true);

    expect(client.getQueryData(SettingsKeys.poolLock())).toEqual({
      poolLocked: true,
      drawInProgress: true,
    });
  });

  it("does not invent an unlocked round when no pool state is cached", () => {
    const client = new QueryClient();

    setCachedDrawInProgress(client, true);

    expect(client.getQueryData(SettingsKeys.poolLock())).toBeUndefined();
  });
});

describe("drawInProgressForEvent", () => {
  it("opens the gate on draw and closes it on either terminal event", () => {
    expect(drawInProgressForEvent("movie:drawn")).toBe(true);
    expect(drawInProgressForEvent("movie:revealed")).toBe(false);
    expect(drawInProgressForEvent("movie:watched")).toBe(false);
  });

  it("leaves unrelated events alone", () => {
    expect(drawInProgressForEvent("movie:updated")).toBeUndefined();
  });
});

describe("applyImmediateLifecycleState", () => {
  const movie: MovieDetail = {
    movieID: 42,
    title: "Alien",
    link: "",
    addedAt: "2026-07-01T20:00:00Z",
    addedByID: 1,
    addedByName: "ana",
    status: "pool",
    overview: "In space.",
  };

  it("keeps a drawn gate when a later lock event carries a stale pre-draw snapshot", () => {
    const client = new QueryClient();
    client.setQueryData(SettingsKeys.poolLock(), {
      poolLocked: false,
      drawInProgress: false,
    });

    applyImmediateLifecycleState(client, {
      seq: 1,
      type: "movie:drawn",
      data: movie,
    });
    applyImmediateLifecycleState(client, {
      seq: 2,
      type: "settings:pool-lock-changed",
      data: { poolLocked: true, drawInProgress: false },
    });

    expect(client.getQueryData(SettingsKeys.poolLock())).toEqual({
      poolLocked: true,
      drawInProgress: true,
    });
  });

  it("keeps a revealed gate when a later lock event carries a stale pre-reveal snapshot", () => {
    const client = new QueryClient();
    client.setQueryData(SettingsKeys.poolLock(), {
      poolLocked: false,
      drawInProgress: true,
    });

    applyImmediateLifecycleState(client, {
      seq: 1,
      type: "movie:revealed",
      data: { movieID: 42, drawnAt: "2026-07-29T20:00:00Z" },
    });
    applyImmediateLifecycleState(client, {
      seq: 2,
      type: "settings:pool-lock-changed",
      data: { poolLocked: true, drawInProgress: true },
    });

    expect(client.getQueryData(SettingsKeys.poolLock())).toEqual({
      poolLocked: true,
      drawInProgress: false,
    });
  });

  it("seeds both gates when a lock event is the first cached pool state", () => {
    const client = new QueryClient();

    applyImmediateLifecycleState(client, {
      seq: 1,
      type: "settings:pool-lock-changed",
      data: { poolLocked: true, drawInProgress: true },
    });

    expect(client.getQueryData(SettingsKeys.poolLock())).toEqual({
      poolLocked: true,
      drawInProgress: true,
    });
  });

  it("coordinates reveal state before queued refetches can race", () => {
    const client = new QueryClient();
    client.setQueryData(SettingsKeys.poolLock(), {
      poolLocked: false,
      drawInProgress: true,
    });
    client.setQueryData(["movies", "detail", 42], movie);

    applyImmediateLifecycleState(client, {
      seq: 1,
      type: "movie:revealed",
      data: { movieID: 42, drawnAt: "2026-07-29T20:00:00Z" },
    });

    expect(client.getQueryData(SettingsKeys.poolLock())).toEqual({
      poolLocked: false,
      drawInProgress: false,
    });
    expect(client.getQueryData<MovieDetail>(["movies", "detail", 42])?.status).toBe("current");
  });

  it("merges a watched payload into an enriched cached detail", () => {
    const client = new QueryClient();
    client.setQueryData(SettingsKeys.poolLock(), {
      poolLocked: false,
      drawInProgress: true,
    });
    client.setQueryData(["movies", "detail", 42], movie);

    applyImmediateLifecycleState(client, {
      seq: 2,
      type: "movie:watched",
      data: {
        ...movie,
        status: "watched",
        watchedAt: "2026-07-29T20:00:00Z",
      } satisfies MovieDetail,
    });

    const detail = client.getQueryData<MovieDetail>(["movies", "detail", 42]);
    expect(detail?.status).toBe("watched");
    expect(detail?.overview).toBe("In space.");
    expect(client.getQueryData<{ drawInProgress: boolean }>(SettingsKeys.poolLock())?.drawInProgress).toBe(false);
  });
});
