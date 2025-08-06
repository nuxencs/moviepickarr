import { QueryClientProvider } from "@tanstack/react-query";
import { FilmIcon, UsersIcon } from "lucide-react";

import { queryClient } from "@/api/QueryClient";

import { Header } from "@/components/Header";
import { MoviePicker } from "@/components/MoviePicker";
import { NextPicker } from "@/components/NextPicker";
import { ThemeProvider } from "@/components/ThemeProvider";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Toaster } from "@/components/ui/toast";
import { UsersGrid } from "@/components/UserManagement";

function App() {
    return (
        <QueryClientProvider client={queryClient}>
            <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
                <div className="App">
                    <Header />
                    <div className="p-4">
                        <NextPicker />
                    </div>
                    <Tabs defaultValue="movies" className="w-full">
                        <div className="px-4">
                            <TabsList className="grid w-full grid-cols-2">
                                <TabsTrigger value="movies" className="flex items-center gap-2">
                                    <FilmIcon className="size-4" />
                                    Movies
                                </TabsTrigger>
                                <TabsTrigger value="users" className="flex items-center gap-2">
                                    <UsersIcon className="size-4" />
                                    User Management
                                </TabsTrigger>
                            </TabsList>
                        </div>
                        <TabsContent value="movies">
                            <MoviePicker />
                        </TabsContent>
                        <TabsContent value="users">
                            <UsersGrid />
                        </TabsContent>
                    </Tabs>
                    <Toaster />
                </div>
            </ThemeProvider>
        </QueryClientProvider>
    )
}

export default App
