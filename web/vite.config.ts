import path from "path"

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import {defineConfig} from 'vite'

// https://vite.dev/config/
export default defineConfig({
    plugins: [
        react(),
        tailwindcss()
    ],
    resolve: {
        alias: {
            "@": path.resolve(__dirname, "./src"),
        },
    },
    build: {
        rollupOptions: {
            output: {
                // Peel the large, rarely-changing libraries into their own cached
                // chunks so an app-code deploy doesn't bust the whole bundle's
                // content hash: returning users keep the vendor chunks. App code
                // and on-demand deps (lucide, sonner, number-flow) stay with the
                // code that imports them, so route-level lazy chunks carry their own.
                //
                // This has to be the function form, matching resolved module paths.
                // The array form matches bare specifiers, so listing "react-dom"
                // never caught the `react-dom/client` the entry actually imports and
                // react-dom leaked into the app chunk (react-vendor was 4.4 kB gzip,
                // and every app deploy re-hashed react-dom along with it).
                manualChunks(id) {
                    if (/[\\/]node_modules[\\/](react|react-dom|scheduler)[\\/]/.test(id)) {
                        return "react-vendor"
                    }
                    // Everything under @tanstack, not just react-query/react-router:
                    // the function form matches module paths, so the internals those
                    // two pull in (router-core, history, store, query-core) have to
                    // be caught by name too or they fall back into the app chunk.
                    if (/[\\/]node_modules[\\/]@tanstack[\\/]/.test(id)) {
                        return "tanstack"
                    }
                },
            },
        },
    },
    server: {
        hmr: {
            overlay: true,
        },
        proxy: {
            '/api': {
                target: 'http://localhost:3030',
                changeOrigin: true,
                secure: false,
            },
        },
    }
})
