// Overview: quick-shorten box and the org's most recent links, reusing the
// Links page components.

import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";

import { useAuth } from "../auth/AuthContext";
import LinkList from "../components/LinkList";
import ShortenForm from "../components/ShortenForm";
import { ErrorBanner, Spinner } from "../components/ui";
import { endpoints } from "../lib/api";
import type { ShortURL } from "../lib/types";
import { usePageTitle } from "../lib/usePageTitle";

const RECENT_COUNT = 5;

export default function HomePage() {
  usePageTitle("Overview");

  const { user, orgs, activeOrgId } = useAuth();
  const activeOrg = orgs.find((o) => o.org_id === activeOrgId);
  const firstName = user?.name.split(" ")[0] ?? "";

  const [recent, setRecent] = useState<ShortURL[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setError("");

    try {
      const page = await endpoints.links(1);

      setRecent(page.links.slice(0, RECENT_COUNT));
      setTotal(page.total);
    } catch {
      setError("Could not load your recent links.");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold text-slate-900" data-testid="welcome-heading">
          Welcome{firstName ? `, ${firstName}` : ""}
        </h2>
        <p className="mt-1 text-sm text-slate-500">
          {activeOrg ? (
            <>
              You are working in <span className="font-medium text-slate-700">{activeOrg.name}</span>{" "}
              as {activeOrg.role === "OWNER" ? "an owner" : "a member"}.
            </>
          ) : (
            "Select an organization to get started."
          )}
        </p>
      </div>

      <section className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
        <h3 className="text-base font-semibold text-slate-900">Shorten a URL</h3>
        <p className="mt-0.5 mb-4 text-sm text-slate-500">
          Paste a long URL to get a short link right away.
        </p>
        <ShortenForm onCreated={() => void load()} onChanged={() => void load()} />
      </section>

      <section className="rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="flex items-center justify-between border-b border-slate-100 px-4 py-4 sm:px-6">
          <h3 className="text-base font-semibold text-slate-900">Recent links</h3>
          {total > 0 && (
            <Link to="/links" className="text-sm font-medium text-indigo-600 hover:text-indigo-500">
              View all {total}
            </Link>
          )}
        </div>

        {loading ? (
          <div className="flex justify-center py-10">
            <Spinner className="h-7 w-7" />
          </div>
        ) : error ? (
          <div className="px-4 py-4 sm:px-6">
            <ErrorBanner message={error} onRetry={() => void load()} />
          </div>
        ) : (
          <LinkList links={recent} onChanged={() => void load()} />
        )}
      </section>
    </div>
  );
}
