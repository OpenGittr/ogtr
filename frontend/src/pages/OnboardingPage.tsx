// Org onboarding: shown after login when the user belongs to no org
// (orgs: [], active_org_id: 0), and reachable via "New organization" in the
// org switcher (?new=1 skips the has-org redirect).

import { useState, type FormEvent } from "react";
import { Navigate, useNavigate, useSearchParams } from "react-router-dom";

import { useAuth } from "../auth/AuthContext";
import { ErrorBanner, NoticeBanner, Spinner } from "../components/ui";
import { ApiError, LIMIT_REACHED } from "../lib/api";
import { usePageTitle } from "../lib/usePageTitle";

export default function OnboardingPage() {
  usePageTitle("Create organization");

  const { user, activeOrgId, orgs, createOrg, logout } = useAuth();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();

  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // LIMIT_REACHED denial: the server's message, shown verbatim as a notice.
  const [limitNotice, setLimitNotice] = useState("");

  const isExtraOrg = searchParams.get("new") === "1";
  const firstName = user?.name.split(" ")[0] ?? "";

  // Already in an org and not explicitly creating another one → dashboard.
  if (activeOrgId > 0 && !isExtraOrg) return <Navigate to="/" replace />;

  const submit = async (event: FormEvent) => {
    event.preventDefault();

    const trimmed = name.trim();
    if (!trimmed || busy) return;

    setBusy(true);
    setError("");
    setLimitNotice("");

    try {
      await createOrg(trimmed);
      navigate("/", { replace: true });
    } catch (err: unknown) {
      if (err instanceof ApiError && err.code === LIMIT_REACHED) {
        setLimitNotice(err.message);
      } else {
        setError(err instanceof Error ? err.message : "Could not create the organization.");
      }

      setBusy(false);
    }
  };

  return (
    <main className="flex min-h-screen items-center justify-center bg-slate-50 px-4">
      <div className="w-full max-w-md">
        <div className="text-center">
          <span className="text-xl font-semibold tracking-tight text-slate-900">ogtr</span>
        </div>

        <div className="mt-6 rounded-2xl border border-slate-200 bg-white p-6 shadow-sm sm:p-8">
          <h1 className="text-lg font-semibold text-slate-900">
            {isExtraOrg ? "Create a new organization" : `Welcome${firstName ? `, ${firstName}` : ""}!`}
          </h1>
          <p className="mt-1 text-sm text-slate-500">
            {isExtraOrg
              ? "You will become its owner and switch to it right away."
              : "Create an organization to get started. You can invite your team later."}
          </p>

          <form onSubmit={submit} className="mt-6 space-y-4">
            <div>
              <label htmlFor="org-name" className="block text-sm font-medium text-slate-700">
                Organization name
              </label>
              <input
                id="org-name"
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Acme Inc"
                autoFocus
                required
                maxLength={120}
                className="mt-1.5 block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm text-slate-900 shadow-sm placeholder:text-slate-400 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200"
              />
            </div>

            {error && <ErrorBanner message={error} />}
            {limitNotice && <NoticeBanner message={limitNotice} />}

            <button
              type="submit"
              disabled={busy || name.trim() === ""}
              className="flex w-full items-center justify-center gap-2 rounded-lg bg-indigo-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {busy && <Spinner className="h-4 w-4 text-white" />}
              {busy ? "Creating…" : "Create organization"}
            </button>
          </form>
        </div>

        <div className="mt-6 text-center text-sm text-slate-500">
          {isExtraOrg && orgs.length > 0 ? (
            <button
              type="button"
              onClick={() => navigate("/")}
              className="font-medium text-indigo-600 hover:text-indigo-500"
            >
              Back to dashboard
            </button>
          ) : (
            <button
              type="button"
              onClick={logout}
              className="font-medium text-indigo-600 hover:text-indigo-500"
            >
              Sign out
            </button>
          )}
        </div>
      </div>
    </main>
  );
}
