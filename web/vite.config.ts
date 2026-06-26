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
                // content hash — returning users keep the vendor chunks. App code
                // and on-demand deps (lucide, sonner, number-flow) stay with the
                // code that imports them, so route-level lazy chunks carry their own.
                manualChunks: {
                    "react-vendor": ["react", "react-dom", "react/jsx-runtime"],
                    tanstack: ["@tanstack/react-query", "@tanstack/react-router"],
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
