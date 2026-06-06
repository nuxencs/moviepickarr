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
  | "movie:enriched"
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

export interface MovieEnrichedEvent extends SSEEvent<{ id: number }> {
  type: "movie:enriched";
}

export interface PoolLockChangedEvent extends SSEEvent<Settings> {
  type: "settings:pool-lock-changed";
}

export interface NextPickerChangedEvent extends SSEEvent<Settings> {
  type: "settings:next-picker-changed";
}
