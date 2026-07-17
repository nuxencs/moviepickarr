import { describe, expect, it } from "vitest";

import {
  type ConnAction,
  type ConnState,
  initialConnState,
  RECONNECT_BASE_MS,
  RECONNECT_JITTER,
  RECONNECT_MAX_MS,
  reduceConnection,
} from "@/hooks/sseConnection";

const acts = (actions: ConnAction[]) => actions.map((a) => a.action);

function connected(state: ConnState, seq = 0, epoch = "e1") {
  return reduceConnection(state, { kind: "connected", seq, epoch }, 0);
}

describe("connected", () => {
  it("aligns the cursor and skips the resync on the very first connect", () => {
    const [state, actions] = connected(initialConnState, 7);
    expect(state).toMatchObject({ lastSeq: 7, epoch: "e1", attempts: 0, everConnected: true });
    expect(acts(actions)).toEqual(["log"]);
  });

  it("resyncs on every reconnect", () => {
    const [first] = connected(initialConnState, 7);
    const [, actions] = connected(first, 9);
    expect(acts(actions)).toEqual(["log", "resync"]);
  });

  it("flags a server restart when the epoch changes and clears a stale cursor", () => {
    const [first] = connected(initialConnState, 900, "e1");
    const [state, actions] = reduceConnection(first, { kind: "connected", seq: 0, epoch: "e2" }, 0);
    expect(state.lastSeq).toBe(0); // broker seq reset; a stale 900 would trip the detector
    expect(actions[0]).toMatchObject({ action: "log" });
    expect((actions[0] as { message: string }).message).toContain("server restarted");
  });

  it("resets the backoff ladder", () => {
    const [errored] = reduceConnection(initialConnState, { kind: "error" }, 0);
    const [state] = connected(errored);
    expect(state.attempts).toBe(0);
  });
});

describe("event seq gap-detection", () => {
  const aligned = () => connected(initialConnState, 10)[0];

  it("a contiguous seq advances the cursor silently", () => {
    const [state, actions] = reduceConnection(aligned(), { kind: "event", seq: 11 }, 0);
    expect(state.lastSeq).toBe(11);
    expect(actions).toEqual([]);
  });

  it("a seq jump resyncs once and realigns", () => {
    const [state, actions] = reduceConnection(aligned(), { kind: "event", seq: 14 }, 0);
    expect(state.lastSeq).toBe(14);
    expect(acts(actions)).toEqual(["log", "resync"]);
  });

  it("an unaligned cursor never reads the first event as a gap", () => {
    const [state, actions] = reduceConnection(initialConnState, { kind: "event", seq: 42 }, 0);
    expect(state.lastSeq).toBe(42);
    expect(actions).toEqual([]);
  });

  it("an event without a seq is ignored", () => {
    const before = aligned();
    const [state, actions] = reduceConnection(before, { kind: "event" }, 0);
    expect(state).toBe(before);
    expect(actions).toEqual([]);
  });
});

describe("heartbeat idle gap-detection", () => {
  const aligned = () => connected(initialConnState, 10)[0];

  it("a head past the cursor resyncs and realigns", () => {
    const [state, actions] = reduceConnection(aligned(), { kind: "heartbeat", seq: 13 }, 0);
    expect(state.lastSeq).toBe(13);
    expect(acts(actions)).toEqual(["log", "resync"]);
  });

  it("a head at the cursor proves liveness silently", () => {
    const before = aligned();
    const [state, actions] = reduceConnection(before, { kind: "heartbeat", seq: 10 }, 0);
    expect(state).toBe(before);
    expect(actions).toEqual([]);
  });

  it("does nothing before the cursor is aligned", () => {
    const [state, actions] = reduceConnection(initialConnState, { kind: "heartbeat", seq: 5 }, 0);
    expect(state).toBe(initialConnState);
    expect(actions).toEqual([]);
  });
});

describe("error backoff", () => {
  it("climbs the ladder exponentially and caps it", () => {
    let state = initialConnState;
    const delays: number[] = [];
    for (let i = 0; i < 8; i++) {
      const [next, actions] = reduceConnection(state, { kind: "error" }, 0);
      state = next;
      const reconnect = actions.find((a) => a.action === "reconnect") as { delayMs: number };
      delays.push(reconnect.delayMs);
    }
    expect(delays.slice(0, 4)).toEqual([1000, 2000, 4000, 8000]);
    expect(delays[7]).toBe(RECONNECT_MAX_MS);
    expect(state.attempts).toBe(8);
  });

  it("jitters within the configured band", () => {
    const [, actions] = reduceConnection(initialConnState, { kind: "error" }, 1);
    const reconnect = actions.find((a) => a.action === "reconnect") as { delayMs: number };
    expect(reconnect.delayMs).toBe(RECONNECT_BASE_MS * (1 + RECONNECT_JITTER));
  });

  it("reset drops the ladder without touching the cursor", () => {
    let state = connected(initialConnState, 10)[0];
    [state] = reduceConnection(state, { kind: "error" }, 0);
    [state] = reduceConnection(state, { kind: "error" }, 0);
    const [reset, actions] = reduceConnection(state, { kind: "reset" }, 0);
    expect(reset).toMatchObject({ attempts: 0, lastSeq: 10 });
    expect(actions).toEqual([]);
  });
});
