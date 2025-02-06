import { QueryClientProvider } from "@tanstack/react-query";

import { queryClient } from "@/api/QueryClient";

import { Header } from "@/components/Header";
import { MoviePicker } from "@/components/MoviePicker";
import { ThemeProvider } from "@/components/ThemeProvider";
import { Toaster } from "@/components/ui/toast";
import { UsersGrid } from "@/components/UserManagement";

function App() {
    return (
        <QueryClientProvider client={queryClient}>
            <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
                <div className="App">
                    <Header />
                    <UsersGrid />
                    <MoviePicker />
                    <Toaster />
                </div>
            </ThemeProvider>
        </QueryClientProvider>
    )
}

export default App
