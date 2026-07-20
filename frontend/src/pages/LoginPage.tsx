// Sign-in page. The server decides which methods exist: GET /auth/providers
// returns the enabled providers ("google", "dev") plus the Google client ID.
//
// - Google: Google Identity Services (GIS) — the one allowed external script,
//   loaded from Google's CDN. The GIS button hands us a Google ID token that
//   POST /api/v1/auth/google exchanges for ogtr session tokens.
//   Client-ID precedence: the server-provided value wins; the build-time
//   VITE_GOOGLE_CLIENT_ID is only a fallback (kept for older deployments and
//   for the offline fallback below).
// - Microsoft: no external script — the SPA runs the PKCE authorization-code
//   flow itself (lib/microsoftAuth.ts): redirect to login.microsoftonline.com,
//   return to /auth/microsoft, exchange the code for an ID token there and
//   POST it to /api/v1/auth/microsoft. Client ID comes from the server only.
// - Dev: a plain name+email form for local evaluation (no Google setup
//   needed); POST /api/v1/auth/dev. Rendered only when the server enables it,
//   under an unmissable amber warning.
//
// If the providers request itself fails we fall back to google + the env
// client ID (the pre-providers-endpoint behavior) so the page is never blank.

import { useEffect, useRef, useState, type FormEvent } from "react";
import { Navigate, useLocation } from "react-router-dom";

import { useAuth } from "../auth/AuthContext";
import Logo from "../components/Logo";
import { ErrorBanner, FullPageSpinner, Spinner } from "../components/ui";
import { endpoints } from "../lib/api";
import { beginMicrosoftLogin } from "../lib/microsoftAuth";
import type { AuthProvidersInfo } from "../lib/types";
import { usePageTitle } from "../lib/usePageTitle";

const FALLBACK_GOOGLE_CLIENT_ID: string = import.meta.env.VITE_GOOGLE_CLIENT_ID ?? "";
const GIS_SRC = "https://accounts.google.com/gsi/client";

interface GisCredentialResponse {
  credential: string;
}

interface GisIdApi {
  initialize(config: {
    client_id: string;
    callback: (response: GisCredentialResponse) => void;
  }): void;
  renderButton(
    parent: HTMLElement,
    options: { theme?: string; size?: string; width?: number; text?: string },
  ): void;
}

declare global {
  interface Window {
    google?: { accounts: { id: GisIdApi } };
  }
}

/** Loads the GIS script once and resolves with the GIS id API. */
function loadGis(): Promise<GisIdApi> {
  return new Promise((resolve, reject) => {
    if (window.google?.accounts.id) {
      resolve(window.google.accounts.id);
      return;
    }

    const existing = document.querySelector<HTMLScriptElement>(`script[src="${GIS_SRC}"]`);
    const script = existing ?? document.createElement("script");

    const onLoad = () => {
      if (window.google?.accounts.id) resolve(window.google.accounts.id);
      else reject(new Error("Google Identity Services failed to initialize"));
    };

    script.addEventListener("load", onLoad, { once: true });
    script.addEventListener(
      "error",
      () => reject(new Error("Could not load Google sign-in script")),
      { once: true },
    );

    if (!existing) {
      script.src = GIS_SRC;
      script.async = true;
      script.defer = true;
      document.head.appendChild(script);
    }
  });
}

export default function LoginPage() {
  const { status, activeOrgId, loginWithGoogle, loginWithDev, sessionExpired } = useAuth();
  const location = useLocation();
  const buttonRef = useRef<HTMLDivElement>(null);
  const [providersInfo, setProvidersInfo] = useState<AuthProvidersInfo | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [devEmail, setDevEmail] = useState("");
  const [devName, setDevName] = useState("");

  usePageTitle("Sign in");

  // Where to land after signing in (set by RequireAuth on redirect here).
  const from = (location.state as { from?: string } | null)?.from;

  // Ask the server which sign-in methods it offers.
  useEffect(() => {
    let cancelled = false;

    endpoints
      .authProviders()
      .then((info) => {
        if (!cancelled) setProvidersInfo(info);
      })
      .catch(() => {
        // Server unreachable or pre-providers-endpoint: assume google with
        // the build-time client ID so the page still renders something.
        if (!cancelled) {
          setProvidersInfo({ providers: ["google"], google_client_id: "", microsoft_client_id: "" });
        }
      });

    return () => {
      cancelled = true;
    };
  }, []);

  const providers = providersInfo?.providers ?? [];
  const googleEnabled = providers.includes("google");
  const microsoftEnabled = providers.includes("microsoft");
  const devEnabled = providers.includes("dev");
  // Server-provided client ID wins; VITE_GOOGLE_CLIENT_ID is the fallback.
  const googleClientId = providersInfo?.google_client_id || FALLBACK_GOOGLE_CLIENT_ID;
  // Microsoft has no build-time fallback: the server is the single source.
  const microsoftClientId = providersInfo?.microsoft_client_id ?? "";

  // busy is a dependency: the button container unmounts while a sign-in is in
  // flight, so the GIS button must be re-rendered after a failed attempt.
  useEffect(() => {
    if (!googleEnabled || !googleClientId || status !== "anonymous" || busy) return;

    let cancelled = false;

    loadGis()
      .then((gis) => {
        if (cancelled || !buttonRef.current) return;

        gis.initialize({
          client_id: googleClientId,
          callback: (response) => {
            setBusy(true);
            setError("");
            loginWithGoogle(response.credential)
              .catch((err: unknown) => {
                setError(err instanceof Error ? err.message : "Sign-in failed. Please try again.");
              })
              .finally(() => setBusy(false));
          },
        });

        buttonRef.current.replaceChildren(); // StrictMode re-runs: avoid two buttons
        gis.renderButton(buttonRef.current, { theme: "outline", size: "large", width: 280 });
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Could not load Google sign-in.");
        }
      });

    return () => {
      cancelled = true;
    };
  }, [status, googleEnabled, googleClientId, busy, loginWithGoogle]);

  // Kicks off the PKCE redirect; only failures to *start* (e.g. storage or
  // WebCrypto unavailable) surface here — the page otherwise navigates away.
  const startMicrosoft = () => {
    setBusy(true);
    setError("");
    beginMicrosoftLogin(microsoftClientId, from).catch((err: unknown) => {
      setBusy(false);
      setError(err instanceof Error ? err.message : "Could not start Microsoft sign-in.");
    });
  };

  const submitDev = (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError("");
    loginWithDev(devEmail.trim(), devName.trim())
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : "Sign-in failed. Please try again.");
      })
      .finally(() => setBusy(false));
  };

  if (status === "loading") return <FullPageSpinner />;

  if (status === "authed") {
    return <Navigate to={activeOrgId > 0 ? (from ?? "/") : "/onboarding"} replace />;
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-slate-950 px-4 py-8">
      <div className="w-full max-w-md">
        <div className="text-center">
          <Logo size="lg" className="mx-auto" />
          <h1 className="mt-4 text-3xl font-semibold tracking-tight text-white sm:text-4xl">
            ogtr
          </h1>
          <p className="mt-2 text-sm text-slate-400">
            Short links, targeting, and analytics for your team.
          </p>
        </div>

        {sessionExpired && (
          <div
            className="mt-6 rounded-lg border border-amber-300/40 bg-amber-400/10 px-4 py-3 text-center text-sm text-amber-200"
            role="status"
            data-testid="session-expired-notice"
          >
            Your session has expired. Please sign in again.
          </div>
        )}

        <div className="mt-8 rounded-2xl bg-white p-6 shadow-xl sm:p-8">
          <h2 className="text-center text-lg font-semibold text-slate-900">Sign in</h2>

          <div className="mt-6 flex min-h-[44px] flex-col gap-4">
            {providersInfo === null ? (
              <div className="flex justify-center">
                <Spinner />
              </div>
            ) : busy ? (
              <div className="flex items-center justify-center gap-2 text-sm text-slate-500">
                <Spinner /> Signing you in…
              </div>
            ) : (
              <>
                {googleEnabled &&
                  (googleClientId ? (
                    <div className="flex justify-center">
                      <div ref={buttonRef} data-testid="google-signin" />
                    </div>
                  ) : (
                    <div
                      className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800"
                      data-testid="gis-not-configured"
                    >
                      <p className="font-medium">Google sign-in is not configured.</p>
                      <p className="mt-1">
                        Set <code className="font-mono">GOOGLE_CLIENT_ID</code> in the backend
                        config (served to this page automatically) and restart the server.
                      </p>
                    </div>
                  ))}

                {microsoftEnabled &&
                  (microsoftClientId ? (
                    <div className="flex justify-center">
                      <button
                        type="button"
                        onClick={startMicrosoft}
                        data-testid="microsoft-signin"
                        className="flex w-[280px] items-center justify-center gap-3 rounded border border-slate-300 bg-white px-4 py-2.5 text-sm font-medium text-slate-700 transition hover:bg-slate-50 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2"
                      >
                        {/* Microsoft four-square mark */}
                        <svg width="18" height="18" viewBox="0 0 21 21" aria-hidden="true">
                          <rect x="1" y="1" width="9" height="9" fill="#f25022" />
                          <rect x="11" y="1" width="9" height="9" fill="#7fba00" />
                          <rect x="1" y="11" width="9" height="9" fill="#00a4ef" />
                          <rect x="11" y="11" width="9" height="9" fill="#ffb900" />
                        </svg>
                        Sign in with Microsoft
                      </button>
                    </div>
                  ) : (
                    <div
                      className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800"
                      data-testid="ms-not-configured"
                    >
                      <p className="font-medium">Microsoft sign-in is not configured.</p>
                      <p className="mt-1">
                        Set <code className="font-mono">MICROSOFT_CLIENT_ID</code> in the
                        backend config (served to this page automatically) and restart the
                        server.
                      </p>
                    </div>
                  ))}

                {(googleEnabled || microsoftEnabled) && devEnabled && (
                  <div className="flex items-center gap-3" aria-hidden="true">
                    <span className="h-px flex-1 bg-slate-200" />
                    <span className="text-xs font-medium uppercase tracking-wide text-slate-400">
                      or
                    </span>
                    <span className="h-px flex-1 bg-slate-200" />
                  </div>
                )}

                {devEnabled && (
                  <div>
                    <div
                      className="rounded-lg border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-900"
                      role="note"
                      data-testid="dev-mode-banner"
                    >
                      <p className="font-semibold">Development mode sign-in</p>
                      <p className="mt-0.5 text-amber-800">
                        For local evaluation only — anyone can sign in as any identity. Never
                        enable this in production.
                      </p>
                    </div>

                    <form className="mt-4 space-y-3" onSubmit={submitDev} data-testid="dev-form">
                      <label className="block">
                        <span className="text-sm font-medium text-slate-700">Name</span>
                        <input
                          type="text"
                          required
                          value={devName}
                          onChange={(e) => setDevName(e.target.value)}
                          placeholder="Eval User"
                          data-testid="dev-name"
                          className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm text-slate-900 placeholder:text-slate-400 focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
                        />
                      </label>
                      <label className="block">
                        <span className="text-sm font-medium text-slate-700">Email</span>
                        <input
                          type="email"
                          required
                          value={devEmail}
                          onChange={(e) => setDevEmail(e.target.value)}
                          placeholder="you@example.com"
                          data-testid="dev-email"
                          className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm text-slate-900 placeholder:text-slate-400 focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
                        />
                      </label>
                      <button
                        type="submit"
                        data-testid="dev-submit"
                        className="w-full rounded-lg bg-amber-500 px-4 py-2 text-sm font-semibold text-white transition hover:bg-amber-600 focus:outline-none focus:ring-2 focus:ring-amber-500 focus:ring-offset-2"
                      >
                        Sign in (development mode)
                      </button>
                    </form>
                  </div>
                )}

                {!googleEnabled && !microsoftEnabled && !devEnabled && (
                  <div className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
                    No sign-in methods are enabled on this server. Set{" "}
                    <code className="font-mono">AUTH_PROVIDERS</code> in the backend config.
                  </div>
                )}
              </>
            )}

            {error && <ErrorBanner message={error} />}
          </div>
        </div>

        <p className="mt-6 text-center text-xs text-slate-500">
          Signing in creates your account if it does not exist yet.
        </p>
      </div>
    </main>
  );
}
