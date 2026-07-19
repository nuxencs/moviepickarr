import { Outlet } from "@tanstack/react-router";

import { NavBar } from "@/components/moviepickarr/NavBar";
import { Toaster } from "@/components/ui/toast";

import type { ReactNode } from "react";

import { useSSE } from "@/hooks/useSSE";

/**
 * Root shell for every route. Holds only the global Toaster and the outlet, so
 * the standalone auth screens (login / claim) render without the NavBar or SSE
 * stream, while the app pages get their chrome from AppLayout below.
 */
export function RootShell() {
  return (
    <>
      <Outlet />
      <Toaster />
    </>
  );
}

/**
 * Chrome for the authenticated app pages (movies / members / stats), mounted
 * once by a pathless layout route so it persists across tab navigations: the
 * SSE stream (useSSE) opens a single EventSource for the session rather than
 * tearing it down and reconnecting on every route change.
 */
export function AppLayout() {
  useSSE();
  return (
    <div className="app">
      <NavBar />
      <Outlet />
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
