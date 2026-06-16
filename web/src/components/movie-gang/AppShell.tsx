import { Outlet } from "@tanstack/react-router";

import { NavBar } from "@/components/movie-gang/NavBar";
import { Toaster } from "@/components/ui/toast";

import type { ReactNode } from "react";

import { useSSE } from "@/hooks/useSSE";

/**
 * App shell. The root route mounts once and persists across tab navigations,
 * so the SSE stream (useSSE) opens a single EventSource for the session rather
 * than tearing it down and reconnecting on every route change.
 */
export function RootLayout() {
  useSSE();
  return (
    <div className="app">
      <NavBar />
      <Outlet />
      <Toaster />
    </div>
  );
}

/**
 * Mirrors the former `<main className="shell">` wrapper. Each route renders its
 * own, so navigating between tabs unmounts/remounts the content — preserving the
 * old `key={tab}` behavior (fresh state and scroll on every tab entry).
 */
export function Shell({ children }: { children: ReactNode }) {
  return <main className="shell">{children}</main>;
}
