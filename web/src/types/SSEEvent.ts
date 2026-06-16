import { Movie, Settings, User } from "@/types/Response";

export type SSEEventType =
  | "user:created"
  | "user:deleted"
  | "movie:added"
  | "movie:deleted"
  | "movie:moved"
  | "movie:picked"
  | "movie:watched"
  | "movie:updated"
  | "movies:enriched-batch"
  | "settings:pool-lock-changed"
  | "settings:next-picker-changed";

export interface SSEEvent<T = unknown> {
  type: SSEEventType;
  data?: T;
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

export interface MoviePickedEvent extends SSEEvent<Movie> {
  type: "movie:picked";
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

export interface NextPickerChangedEvent extends SSEEvent<Settings> {
  type: "settings:next-picker-changed";
}
