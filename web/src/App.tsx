import {Users} from "@/components/UserManagement";
import {MoviePicker} from "@/components/MoviePicker";
import {ThemeProvider} from "@/components/ThemeProvider"
import {Toaster} from "@/components/ui/toast";
import {Header} from "@/components/Header";
import {queryClient} from "@/api/QueryClient";
import {QueryClientProvider} from "@tanstack/react-query";

function App() {
    return (
        <QueryClientProvider client={queryClient}>
            <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
                <div className="App">
                    <Header/>
                    <Users/>
                    <MoviePicker/>
                    <Toaster/>
                </div>
            </ThemeProvider>
        </QueryClientProvider>
    )
}

export default App
