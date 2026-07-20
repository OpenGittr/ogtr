// Links: shorten form on top, paginated table of the org's links below
// (10/page, matching the backend). Rows navigate to the link detail page.

import { useCallback, useEffect, useState } from "react";

import LinkList from "../components/LinkList";
import ShortenForm from "../components/ShortenForm";
import { ErrorBanner, PageLoader } from "../components/ui";
import { ApiError, endpoints } from "../lib/api";
import type { ShortURL } from "../lib/types";
import { usePageTitle } from "../lib/usePageTitle";

export default function LinksPage() {
  usePageTitle("Links");

  const [links, setLinks] = useState<ShortURL[]>([]);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [perPage, setPerPage] = useState(10);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async (target: number) => {
    setLoading(true);
    setError("");

    try {
      const result = await endpoints.links(target);

      setLinks(result.links);
      setPage(result.page);
      setTotal(result.total);
      setPerPage(result.per_page);
    } catch (err: unknown) {
      setError(
        err instanceof ApiError && err.message
          ? err.message
          : "Could not load your links. Please try again.",
      );
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(1);
  }, [load]);

  const totalPages = Math.max(1, Math.ceil(total / perPage));

  return (
    <div className="space-y-6">
      <section className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
        <h2 className="text-base font-semibold text-slate-900">Shorten a URL</h2>
        <p className="mt-0.5 mb-4 text-sm text-slate-500">
          Paste a long URL to get a short link on this deployment.
        </p>
        <ShortenForm onCreated={() => void load(1)} onChanged={() => void load(page)} />
      </section>

      <section className="rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="flex items-center justify-between border-b border-slate-100 px-4 py-4 sm:px-6">
          <div>
            <h2 className="text-base font-semibold text-slate-900">Your links</h2>
            <p className="mt-0.5 text-sm text-slate-500">
              {total} {total === 1 ? "link" : "links"} in this organization
            </p>
          </div>
        </div>

        {loading ? (
          <PageLoader />
        ) : error ? (
          <div className="px-4 py-4 sm:px-6">
            <ErrorBanner message={error} onRetry={() => void load(page)} />
          </div>
        ) : (
          <>
            <LinkList links={links} onChanged={() => void load(page)} />

            {totalPages > 1 && (
              <div className="flex items-center justify-between border-t border-slate-100 px-4 py-3 sm:px-6">
                <button
                  type="button"
                  onClick={() => void load(page - 1)}
                  disabled={page <= 1}
                  className="rounded-lg border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-600 transition hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  Previous
                </button>
                <span className="text-sm text-slate-500" data-testid="pagination-label">
                  Page {page} of {totalPages}
                </span>
                <button
                  type="button"
                  onClick={() => void load(page + 1)}
                  disabled={page >= totalPages}
                  className="rounded-lg border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-600 transition hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  Next
                </button>
              </div>
            )}
          </>
        )}
      </section>
    </div>
  );
}
