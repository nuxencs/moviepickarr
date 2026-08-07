import { Outlet } from "@tanstack/react-router";

import { AudioProvider } from "@/components/AudioProvider";
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
 *
 * The AudioProvider hangs off this layout, not the app root: it builds the Web
 * Audio graph on mount, and the only surfaces that play or configure sound (the
 * draw reel, the profile panel's volume control) live under here. Mounting it at
 * the root made the login screen pay for a graph it could never use.
 */
export function AppLayout() {
  useSSE();
  return (
    <AudioProvider>
      <div className="app">
        <NavBar />
        <Outlet />
      </div>
    </AudioProvider>
  );
}

/**
 * Mirrors the former `<main className="shell">` wrapper. Each route renders its
 * own, so navigating between tabs unmounts/remounts the content — preserving the
 * old `key={tab}` behavior (fresh state and scroll on every tab entry).
 */
export function Shell({ children, className }: { children: ReactNode; className?: string }) {
  return <main className={className ? `shell ${className}` : "shell"}>{children}</main>;
}
