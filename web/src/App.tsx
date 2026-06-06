import { QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { queryClient } from "@/api/QueryClient";

import { Hero } from "@/components/movie-gang/Hero";
import { MoviesTab } from "@/components/movie-gang/MoviesTab";
import { NavBar, type Tab } from "@/components/movie-gang/NavBar";
import { StatsTab } from "@/components/movie-gang/StatsTab";
import { UsersTab } from "@/components/movie-gang/UsersTab";
import { ThemeProvider } from "@/components/ThemeProvider";
import { Toaster } from "@/components/ui/toast";

import { useSSE } from "@/hooks/useSSE";

const TABS: Tab[] = ["movies", "users", "stats"];

/** Active tab lives in the URL (`?tab=`) so it's shareable, deep-linkable, and
 *  works with browser back/forward — not hidden in localStorage. */
function tabFromURL(): Tab {
  const t = new URLSearchParams(window.location.search).get("tab");
  return t && (TABS as string[]).includes(t) ? (t as Tab) : "movies";
}

function AppContent() {
  useSSE();
  const [tab, setTab] = useState<Tab>(tabFromURL);

  // Keep state in sync when the user navigates with back/forward.
  useEffect(() => {
    const onPop = () => setTab(tabFromURL());
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  const changeTab = (next: Tab) => {
    if (next === tab) return; // don't push a duplicate history entry for the active tab
    const url = new URL(window.location.href);
    url.searchParams.set("tab", next);
    window.history.pushState({}, "", url);
    setTab(next);
  };

  return (
    <div className="app">
      <NavBar active={tab} onChange={changeTab} />

      {tab === "movies" && <Hero />}

      <main className="shell" key={tab}>
        {tab === "movies" && <MoviesTab />}
        {tab === "users" && <UsersTab />}
        {tab === "stats" && <StatsTab />}
      </main>

      <Toaster />
    </div>
  );
}

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
        <AppContent />
      </ThemeProvider>
    </QueryClientProvider>
  );
}

export default App;
