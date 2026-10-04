import { MovieDetail, MovieDrawPayload, Settings, User, Wildcard } from "@/types/Response";

export type SSEEventType =
  | "user:created"
  | "user:deleted"
  | "user:role-changed"
  | "movie:added"
  | "movie:deleted"
  | "movie:moved"
  | "movie:drawn"
  | "movie:revealed"
  | "movie:watched"
  | "movie:updated"
  | "wildcard:selected"
  | "wildcard:canceled"
  | "wildcard:watched"
  | "movies:enriched-batch"
  | "settings:pool-lock-changed"
  | "settings:next-up-changed";

export interface SSEEvent<T = unknown> {
  // Broker-global and monotonic. Gap detection only (a jump triggers one resync);
  // the server keeps no replay history.
  seq: number;
  type: SSEEventType;
  data?: T;
}

// epoch detects a server restart; seq is the head at subscribe time; serverNow
// seeds the clock offset.
export interface SSEConnectedFrame {
  type: "connected";
  epoch: string;
  seq: number;
  serverNow: string;
}

// Head seq for passive gap detection, serverNow for clock-offset refresh.
export interface SSEHeartbeatFrame {
  seq: number;
  serverNow: string;
}

export interface UserCreatedEvent extends SSEEvent<User> {
  type: "user:created";
}

export interface UserDeletedEvent extends SSEEvent<{ userID: number }> {
  type: "user:deleted";
}

export interface MovieAddedEvent extends SSEEvent<MovieDetail> {
  type: "movie:added";
}

export interface MovieDeletedEvent extends SSEEvent<{ userID: number; movieID: number }> {
  type: "movie:deleted";
}

export interface MovieMovedEvent extends SSEEvent<{ userID: number; movieID: number }> {
  type: "movie:moved";
}

export interface MovieDrawnEvent extends SSEEvent<MovieDrawPayload> {
  type: "movie:drawn";
}

// Every client closes its reel and reveals in lockstep.
export interface MovieRevealedEvent extends SSEEvent<{ movieID: number; drawnAt: string }> {
  type: "movie:revealed";
}

export interface MovieWatchedEvent extends SSEEvent<MovieDetail> {
  type: "movie:watched";
}

export interface MovieUpdatedEvent extends SSEEvent<MovieDetail> {
  type: "movie:updated";
}

export interface WildcardSelectedEvent extends SSEEvent<Wildcard> {
  type: "wildcard:selected";
}

export interface WildcardCanceledEvent extends SSEEvent<{ id: number; movieId: number }> {
  type: "wildcard:canceled";
}

export interface WildcardWatchedEvent extends SSEEvent<Wildcard> {
  type: "wildcard:watched";
}

// One event per enrichment burst, not per movie; useSSE invalidates the lists.
export interface MoviesEnrichedBatchEvent extends SSEEvent<undefined> {
  type: "movies:enriched-batch";
}

export interface PoolLockChangedEvent extends SSEEvent<Settings> {
  type: "settings:pool-lock-changed";
}

export interface NextUpChangedEvent extends SSEEvent<Settings> {
  type: "settings:next-up-changed";
}
