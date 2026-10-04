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
                // Vendor chunks survive app deploys. Function form, not array: the
                // array matches bare specifiers and missed `react-dom/client`.
                manualChunks(id) {
                    if (/[\\/]node_modules[\\/](react|react-dom|scheduler)[\\/]/.test(id)) {
                        return "react-vendor"
                    }
                    // All of @tanstack, so internals like router-core stay out of the app chunk.
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
