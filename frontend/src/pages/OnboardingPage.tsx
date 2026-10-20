// Org onboarding: shown after login when the user belongs to no org
// (orgs: [], active_org_id: 0), and reachable via "New organization" in the
// org switcher (?new=1 skips the has-org redirect). GET /me accepts pending
// invites, so an org-less user invited after signing in is offered the org
// here; the page re-checks when the tab regains focus.

import { useEffect, useState, type FormEvent } from "react";
import { Navigate, useNavigate, useSearchParams } from "react-router-dom";

import { useAuth } from "../auth/AuthContext";
import Logo from "../components/Logo";
import { ErrorBanner, NoticeBanner, Spinner } from "../components/ui";
import { ApiError, LIMIT_REACHED } from "../lib/api";
import { limitNoticeAction } from "../ext";
import { usePageTitle } from "../lib/usePageTitle";

export default function OnboardingPage() {
  usePageTitle("Create organization");

  const { user, activeOrgId, orgs, createOrg, switchOrg, refreshMe, logout } = useAuth();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();

  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // LIMIT_REACHED denial: the server's message, shown verbatim as a notice.
  const [limitNotice, setLimitNotice] = useState("");
  const [openingOrgId, setOpeningOrgId] = useState(0);

  const isExtraOrg = searchParams.get("new") === "1";
  const firstName = user?.name.split(" ")[0] ?? "";

  // An invite may arrive while this page is open (e.g. the user signed in,
  // then asked a teammate to invite them) — look again on return to the tab.
  useEffect(() => {
    if (isExtraOrg) return;

    const recheck = () => {
      if (document.visibilityState === "visible") refreshMe().catch(() => {});
    };

    window.addEventListener("focus", recheck);
    document.addEventListener("visibilitychange", recheck);

    return () => {
      window.removeEventListener("focus", recheck);
      document.removeEventListener("visibilitychange", recheck);
    };
  }, [isExtraOrg, refreshMe]);

  // Already in an org and not explicitly creating another one → dashboard.
  if (activeOrgId > 0 && !isExtraOrg) return <Navigate to="/" replace />;

  const openOrg = async (orgId: number) => {
    if (openingOrgId) return;

    setOpeningOrgId(orgId);
    setError("");

    try {
      await switchOrg(orgId);
      navigate("/", { replace: true });
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Could not open the organization.");
      setOpeningOrgId(0);
    }
  };

  // Org-less session with memberships: invited (or auto-joined) mid-session.
  const joinedOrgs = isExtraOrg ? [] : orgs;

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
    <main className="flex min-h-screen items-center justify-center bg-slate-50 dark:bg-slate-950 px-4">
      <div className="w-full max-w-md">
        <div className="text-center">
          <Logo withWordmark size="md" className="text-slate-900 dark:text-white" />
        </div>

        <div className="mt-6 rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-6 shadow-sm sm:p-8">
          <h1 className="text-lg font-semibold text-slate-900 dark:text-white">
            {isExtraOrg ? "Create a new organization" : `Welcome${firstName ? `, ${firstName}` : ""}!`}
          </h1>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
            {isExtraOrg
              ? "You will become its owner and switch to it right away."
              : joinedOrgs.length > 0
                ? "Open an organization you were invited to, or create your own."
                : "Create an organization to get started. You can invite your team later."}
          </p>

          {joinedOrgs.length > 0 && (
            <div className="mt-6">
              <p className="text-sm font-medium text-slate-700 dark:text-slate-200">
                You have been added to {joinedOrgs.length === 1 ? "an organization" : "these organizations"}
              </p>
              <ul className="mt-2 space-y-2">
                {joinedOrgs.map((org) => (
                  <li
                    key={org.org_id}
                    className="flex items-center justify-between gap-3 rounded-lg border border-slate-200 dark:border-slate-700 px-3 py-2"
                  >
                    <span className="min-w-0 truncate text-sm text-slate-900 dark:text-white">{org.name}</span>
                    <button
                      type="button"
                      onClick={() => openOrg(org.org_id)}
                      disabled={openingOrgId !== 0}
                      className="flex shrink-0 items-center gap-2 rounded-lg bg-indigo-600 px-3 py-1.5 text-sm font-semibold text-white shadow-sm transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {openingOrgId === org.org_id && <Spinner className="h-4 w-4 text-white" />}
                      Open
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          )}

          <form onSubmit={submit} className={`${joinedOrgs.length > 0 ? "mt-3" : "mt-6"} space-y-4`}>
            <div>
              <label htmlFor="org-name" className="block text-sm font-medium text-slate-700 dark:text-slate-200">
                Organization name
              </label>
              <input
                id="org-name"
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Acme Inc"
                autoFocus={joinedOrgs.length === 0}
                required
                maxLength={120}
                className="mt-1.5 block w-full rounded-lg border border-slate-300 dark:border-slate-700 px-3 py-2 text-sm text-slate-900 dark:text-white shadow-sm placeholder:text-slate-400 dark:placeholder:text-slate-500 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200 dark:focus:ring-indigo-500/30"
              />
            </div>

            {error && <ErrorBanner message={error} />}
            {limitNotice && <NoticeBanner message={limitNotice} action={limitNoticeAction} />}

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

        <div className="mt-6 text-center text-sm text-slate-500 dark:text-slate-400">
          {isExtraOrg && orgs.length > 0 ? (
            <button
              type="button"
              onClick={() => navigate("/")}
              className="font-medium text-indigo-600 dark:text-indigo-400 hover:text-indigo-500"
            >
              Back to dashboard
            </button>
          ) : (
            <button
              type="button"
              onClick={logout}
              className="font-medium text-indigo-600 dark:text-indigo-400 hover:text-indigo-500"
            >
              Sign out
            </button>
          )}
        </div>
      </div>
    </main>
  );
}
