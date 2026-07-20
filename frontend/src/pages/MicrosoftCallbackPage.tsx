// Return leg of the Microsoft sign-in (see lib/microsoftAuth.ts for the full
// flow). Microsoft redirects here with ?code=&state= (or ?error=); this page
// validates the CSRF state against the sessionStorage stash, exchanges the
// code for an ID token at Microsoft's token endpoint, and hands that token to
// the backend exactly like a Google credential. Errors (user denied, state
// mismatch, token-exchange failure) render in a card matching the login page,
// with a way back to /login.

import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";

import { useAuth } from "../auth/AuthContext";
import Logo from "../components/Logo";
import { ErrorBanner, Spinner } from "../components/ui";
import {
  redeemMicrosoftCode,
  takePendingMicrosoftLogin,
} from "../lib/microsoftAuth";
import { usePageTitle } from "../lib/usePageTitle";

export default function MicrosoftCallbackPage() {
  const { loginWithMicrosoft } = useAuth();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [error, setError] = useState("");
  // An auth code is single-use and the state stash is one-shot, so the
  // exchange must run exactly once (StrictMode re-runs effects in dev).
  const ran = useRef(false);

  usePageTitle("Signing in with Microsoft");

  useEffect(() => {
    if (ran.current) return;
    ran.current = true;

    const run = async () => {
      // User cancelled or Microsoft refused: no state to consume server-side.
      const providerError = params.get("error");
      if (providerError) {
        takePendingMicrosoftLogin(); // clear the stash either way
        setError(
          providerError === "access_denied"
            ? "Sign-in was cancelled. No account access was granted."
            : (params.get("error_description") ??
                `Microsoft sign-in failed (${providerError}).`),
        );
        return;
      }

      const pending = takePendingMicrosoftLogin();
      const state = params.get("state");
      const code = params.get("code");

      if (!pending || !state || state !== pending.state) {
        setError(
          "This sign-in response could not be verified (state mismatch or expired attempt). " +
            "Please start again.",
        );
        return;
      }

      if (!code) {
        setError("Microsoft did not return an authorization code. Please try again.");
        return;
      }

      try {
        const idToken = await redeemMicrosoftCode(code, pending);
        await loginWithMicrosoft(idToken);
        navigate(pending.from ?? "/", { replace: true });
      } catch (err: unknown) {
        setError(err instanceof Error ? err.message : "Sign-in failed. Please try again.");
      }
    };

    void run();
  }, [params, loginWithMicrosoft, navigate]);

  return (
    <main className="flex min-h-screen items-center justify-center bg-slate-950 px-4 py-8">
      <div className="w-full max-w-md">
        <div className="text-center">
          <Logo size="lg" className="mx-auto" />
          <h1 className="mt-4 text-3xl font-semibold tracking-tight text-white sm:text-4xl">
            ogtr
          </h1>
        </div>

        <div className="mt-8 rounded-2xl bg-white p-6 shadow-xl sm:p-8">
          <h2 className="text-center text-lg font-semibold text-slate-900">
            Microsoft sign-in
          </h2>

          <div className="mt-6 flex min-h-[44px] flex-col gap-4">
            {error ? (
              <>
                <ErrorBanner message={error} />
                <Link
                  to="/login"
                  replace
                  data-testid="ms-back-to-login"
                  className="text-center text-sm font-medium text-indigo-600 hover:text-indigo-700"
                >
                  Back to sign in
                </Link>
              </>
            ) : (
              <div
                className="flex items-center justify-center gap-2 text-sm text-slate-500"
                data-testid="ms-exchanging"
              >
                <Spinner /> Completing sign-in…
              </div>
            )}
          </div>
        </div>
      </div>
    </main>
  );
}
