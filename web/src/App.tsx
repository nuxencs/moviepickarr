import { QueryClientProvider } from "@tanstack/react-query";
import { ChartNoAxesColumnIcon, FilmIcon, UsersIcon } from "lucide-react";

import { queryClient } from "@/api/QueryClient";

import { Header } from "@/components/Header";
import { MoviePicker } from "@/components/MoviePicker";
import { NextPicker } from "@/components/NextPicker";
import { StatsTab } from "@/components/StatsTab";
import { ThemeProvider } from "@/components/ThemeProvider";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Toaster } from "@/components/ui/toast";
import { UsersGrid } from "@/components/UserManagement";

import { useSSE } from "@/hooks/useSSE";

function AppContent() {
  useSSE();

  return (
    <div className="App">
      <Header/>
      <div className="p-4">
        <NextPicker/>
      </div>
      <Tabs defaultValue="movies" className="w-full">
        <div className="px-4">
          <TabsList className="grid w-full grid-cols-3">
            <TabsTrigger value="movies" className="flex items-center gap-2">
              <FilmIcon className="size-4"/>
              Movies
            </TabsTrigger>
            <TabsTrigger value="users" className="flex items-center gap-2">
              <UsersIcon className="size-4"/>
              Users
            </TabsTrigger>
            <TabsTrigger value="stats" className="flex items-center gap-2">
              <ChartNoAxesColumnIcon className="size-4"/>
              Stats
            </TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value="movies">
          <MoviePicker/>
        </TabsContent>
        <TabsContent value="stats">
          <StatsTab/>
        </TabsContent>
        <TabsContent value="users">
          <UsersGrid/>
        </TabsContent>
      </Tabs>
      <Toaster/>
    </div>
  );
}

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
        <AppContent/>
      </ThemeProvider>
    </QueryClientProvider>
  )
}

export default App
