import { Outlet, useRouterState } from "@tanstack/react-router";
import { useLayoutEffect } from "react";

import { AudioProvider } from "@/components/AudioProvider";
import { NavBar } from "@/components/moviepickarr/NavBar";
import { Toaster } from "@/components/ui/toast";

import type { ReactNode } from "react";

import { useSSE } from "@/hooks/useSSE";
import { resetDocumentScroll } from "@/lib/scrollPolicy";

/** Root shell: no NavBar or SSE, so the auth screens render bare. */
export function RootShell() {
  return (
    <>
      <Outlet />
      <Toaster />
    </>
  );
}

/**
 * Chrome for the app pages, mounted once by a pathless layout route so the SSE
 * EventSource survives tab navigation. AudioProvider sits here, not at the
 * root, so the login screen does not build an unused Web Audio graph.
 */
export function AppLayout() {
  useSSE();
  const renderedPathname = useRouterState({
    select: (state) => state.matches[state.matches.length - 1]?.pathname,
  });

  useLayoutEffect(() => {
    resetDocumentScroll();
  }, [renderedPathname]);

  return (
    <AudioProvider>
      <div className="app">
        <NavBar />
        <Outlet />
      </div>
    </AudioProvider>
  );
}

/** Each route renders its own Shell, so every tab entry starts fresh. */
export function Shell({ children, className }: { children: ReactNode; className?: string }) {
  return <main className={className ? `shell ${className}` : "shell"}>{children}</main>;
}
