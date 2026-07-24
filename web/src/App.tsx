import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";

import { queryClient } from "@/api/QueryClient";

import { ThemeProvider } from "@/components/ThemeProvider";

import { router } from "@/router";

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
        {/* AudioProvider is deliberately not here: it mounts with the app
            chrome (see AppLayout) so the login and claim screens, which have
            nothing to play, never build a Web Audio graph. */}
        <RouterProvider router={router} />
      </ThemeProvider>
    </QueryClientProvider>
  );
}

export default App;
