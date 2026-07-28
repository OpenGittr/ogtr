import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";

import "./index.css";
import App from "./App";
import { AuthProvider } from "./auth/AuthContext";

// Router base path, derived from Vite's `base` (import.meta.env.BASE_URL).
// Defaults to "/" (unchanged for the standard build); the all-in-one image
// builds with --base=/app/ so the dashboard can be mounted under /app without
// colliding with the /{code} short-link namespace at the root.
const basename = import.meta.env.BASE_URL.replace(/\/+$/, "") || "/";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter basename={basename}>
      <AuthProvider>
        <App />
      </AuthProvider>
    </BrowserRouter>
  </StrictMode>,
);
