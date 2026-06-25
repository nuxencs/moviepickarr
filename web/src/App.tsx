import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";

import { queryClient } from "@/api/QueryClient";

import { AudioProvider } from "@/components/AudioProvider";
import { ThemeProvider } from "@/components/ThemeProvider";

import { router } from "@/router";

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
        <AudioProvider>
          <RouterProvider router={router} />
        </AudioProvider>
      </ThemeProvider>
    </QueryClientProvider>
  );
}

export default App;
