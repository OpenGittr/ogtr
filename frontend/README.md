# frontend — ogtr dashboard

React 19 + Tailwind 4 + Vite SPA (CSR only, served as static files in production by
[gostatic](https://github.com/opengittr/gostatic) — no SSR, no nginx; see the Dockerfile).
Port **5800** (strict) from the project's 5800–5899 block.

## Run locally

```sh
# 1. MySQL (host port 5830)
docker compose up -d              # from the repo root

# 2. Backend (port 5810; needs backend/configs/.local.env — see configs/.sample.env)
cd backend && go run .

# 3. This app
cd frontend
npm install
npm run dev                       # http://localhost:5800
```

The Vite dev server proxies `/api/*` to `http://localhost:5810` (see `vite.config.ts`), so
local dev needs no backend CORS. For cross-origin production setups the backend pins CORS via
`ACCESS_CONTROL_ALLOW_ORIGIN` in its config (keep it in sync with `APP_URL`).

### Env vars

Copy `.env.sample` to `.env.local` (gitignored):

- `VITE_GOOGLE_CLIENT_ID` — your Google OAuth client ID for the Google Identity Services
  button (required GCP step: add `http://localhost:5800` to the OAuth client's
  **Authorized JavaScript origins** — see docs/LOCAL_DEVELOPMENT.md, "Google OAuth"
  section). When unset, the login page shows a clear
  "not configured" notice instead of a broken button.
- `VITE_API_URL` — API base URL. Default `""` = same origin (dev proxy handles it; production
  either serves app + API behind one host or sets this at build time).

## What's implemented

- **App shell** — `react-router` routing, auth context, typed `fetch` API client
  (`src/lib/api.ts`): attaches the access token, unwraps Gofr's `{"data": ...}` envelope,
  surfaces its `{"error": {"message"}}` shape as `ApiError`, and transparently refreshes on
  401 (single-flight; concurrent 401s await one refresh, retry once; refresh failure logs out).
  Tokens live in `localStorage` under `ogtr.tokens`.
- **Google sign-in** (`/login`) — GIS button flow → ID token → `POST /api/v1/auth/google`.
- **Org onboarding** (`/onboarding`) — org-less logins (orgs `[]`) create their first org;
  `POST /api/v1/orgs` returns an org-scoped token pair which is adopted directly. Also
  reachable as "New organization" from the org switcher (`?new=1`).
- **Org switcher** — header dropdown over `GET /me` memberships; `POST /api/v1/auth/switch-org`
  re-scopes the session.
- **Members & invites** (`/members`) — member list with role badges, remove member with inline
  confirm (OWNER only; hidden for MEMBER), invite by email, pending invites with revoke.
  API errors (403 non-owner, 409 duplicate invite, 422 last-owner) render as friendly inline
  messages.
- **Responsive shell** — dark sidebar on desktop, hamburger + slide-over on mobile. All nav
  items (Links / Analytics / API keys / Members) are live since phases 3–6.

## UI conventions (phase 7 polish)

- **Page titles** — every page calls `usePageTitle("Links")` (`src/lib/usePageTitle.ts`) so
  the tab reads `Links — ogtr`; the link detail page uses the short code (`/abc123`).
- **Loading** — page-level fetches render the shared `<PageLoader/>` (centered 8×8 spinner);
  in-card/in-button busy states use the small `<Spinner/>`. Both live in `components/ui.tsx`.
- **Empty states** — use `<EmptyState icon title hint action?/>` (icon + one line + optional
  action), not bare grey text. Stock icon paths in `emptyIconPaths`.
- **Errors** — `<ErrorBanner message onRetry?/>`; pass `onRetry` for load failures so every
  error surface has a retry affordance in the same style.
- **404** — unknown SPA paths render `pages/NotFoundPage.tsx` (standalone, link back home).
- **Session expiry** — a rejected refresh token flips `sessionExpired` in `AuthContext`; the
  login page shows a "session has expired" notice (cleared on the next sign-in; plain sign-out
  does not set it).
- **Org switching** — the shell's `<main>` is keyed by `activeOrgId`, so switching orgs
  remounts the current page and refetches under the new tenant; switching while on a link
  detail page navigates to `/links` (link ids are org-scoped and would 404).
- **Mobile** — every route must render at 390 px with no horizontal scroll (wide tables get
  `overflow-x-auto` wrappers; list rows collapse to stacked cards).
- **Overlays & row menus** — link management is on-demand, not resident: `Modal`, `Drawer`
  and `KebabMenu` in `components/ui.tsx` are the hand-rolled primitives (Esc + backdrop
  close, body-scroll lock, focus returned to the opener; the drawer becomes a full-screen
  sheet below 640 px; menus support arrow keys and close on outside click). The link-specific
  overlay bodies (QR panel, alias editor, edit-destination form, targeting drawer content)
  and the shared kebab item list live in `components/LinkActions.tsx` — reuse
  `LinkActionOverlays` rather than re-wrapping those forms. Refresh lists when an overlay
  *closes* (see `LinkList`'s dirty-flag pattern): refetching earlier unmounts the overlay
  mid-edit.

## Scripts

- `npm run dev` — dev server on 5800 (strictPort)
- `npm run build` — `tsc --noEmit` + production build to `dist/`
- `npm run preview` — serve the production build
