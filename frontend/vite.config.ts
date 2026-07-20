import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Port 5800 = frontend in the project's 5800-5899 block (see docs/LOCAL_DEVELOPMENT.md).
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5800,
    strictPort: true,
    // Dev-time CORS avoidance: the SPA calls same-origin /api/* (VITE_API_URL
    // defaults to "") and Vite forwards to the Gofr backend on 5810. The
    // pattern is anchored with a trailing slash ("^/api/") so the SPA route
    // /api-keys is NOT proxied — a bare "/api" prefix would swallow it and
    // full-page loads of the API-keys page would 404 against the backend.
    proxy: {
      "^/api/": {
        target: "http://localhost:5810",
        changeOrigin: true,
      },
    },
  },
});
