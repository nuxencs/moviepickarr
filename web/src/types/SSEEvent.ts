import { Movie, Settings, User } from "@/types/Response";

export type SSEEventType =
  | "user:created"
  | "user:deleted"
  | "movie:added"
  | "movie:deleted"
  | "movie:moved"
  | "movie:drawn"
  | "movie:revealed"
  | "movie:watched"
  | "movie:updated"
  | "movies:enriched-batch"
  | "settings:pool-lock-changed"
  | "settings:next-up-changed";

export interface SSEEvent<T = unknown> {
  // Broker-global monotonic sequence number, assigned at broadcast time. The
  // client uses it for gap detection only (a jump means a dropped frame → one
  // resync); the server keeps no replay history.
  seq: number;
  type: SSEEventType;
  data?: T;
}

// The one-shot handshake (`event: connected`). Not a domain event. epoch detects
// a server restart; seq is the head at subscribe time (cursor alignment);
// serverNow seeds the choreography clock offset.
export interface SSEConnectedFrame {
  type: "connected";
  epoch: string;
  seq: number;
  serverNow: string;
}

// The idle keep-alive (`event: heartbeat`). Carries the head seq for passive gap
// detection and serverNow for clock-offset refresh.
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

export interface MovieAddedEvent extends SSEEvent<Movie> {
  type: "movie:added";
}

export interface MovieDeletedEvent extends SSEEvent<{ userID: number; movieID: number }> {
  type: "movie:deleted";
}

export interface MovieMovedEvent extends SSEEvent<User> {
  type: "movie:moved";
}

export interface MovieDrawnEvent extends SSEEvent<Movie> {
  type: "movie:drawn";
}

// The drawer confirmed (or the reel's countdown filled): every client closes its
// reel and reveals the draw in lockstep. Carries just enough to match the spin.
export interface MovieRevealedEvent extends SSEEvent<{ movieID: number; drawnAt: string }> {
  type: "movie:revealed";
}

export interface MovieWatchedEvent extends SSEEvent<Movie> {
  type: "movie:watched";
}

export interface MovieUpdatedEvent extends SSEEvent<Movie> {
  type: "movie:updated";
}

// Coalesced signal: the enrichment worker finished a burst of movies and emits
// one batch event instead of one per movie. Carries no payload — the frontend
// invalidates the affected lists (see useSSE), which then refetch enriched data.
export interface MoviesEnrichedBatchEvent extends SSEEvent<undefined> {
  type: "movies:enriched-batch";
}

export interface PoolLockChangedEvent extends SSEEvent<Settings> {
  type: "settings:pool-lock-changed";
}

export interface NextUpChangedEvent extends SSEEvent<Settings> {
  type: "settings:next-up-changed";
}
