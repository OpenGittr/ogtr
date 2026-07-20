// Org-level UTM analysis (phase 5, FEATURES.md §6.3): click counts grouped by
// utm_source / utm_medium / utm_campaign, each broken down per link.

import { useCallback, useEffect, useState } from "react";
import { Link as RouterLink } from "react-router-dom";

import { ApiError, LIMIT_REACHED, endpoints } from "../lib/api";
import type { UTMAnalysis, UTMCount } from "../lib/types";
import { usePageTitle } from "../lib/usePageTitle";
import {
  EmptyIcon,
  EmptyState,
  ErrorBanner,
  NoticeBanner,
  PageLoader,
  emptyIconPaths,
} from "../components/ui";

const SECTIONS: { key: keyof UTMAnalysis; title: string; utmHeader: string; blurb: string }[] = [
  {
    key: "source_analysis",
    title: "By source",
    utmHeader: "utm_source",
    blurb: "Where clicks come from — explicit tags or referrer-derived auto-tags.",
  },
  {
    key: "medium_analysis",
    title: "By medium",
    utmHeader: "utm_medium",
    blurb: "How clicks arrive — explicit tags or device-derived auto-tags.",
  },
  {
    key: "campaign_analysis",
    title: "By campaign",
    utmHeader: "utm_campaign",
    blurb: "Clicks on links tagged with an explicit campaign.",
  },
];

function UTMTable({ rows, utmHeader }: { rows: UTMCount[]; utmHeader: string }) {
  if (rows.length === 0) {
    return (
      <p className="py-6 text-center text-sm text-slate-400" data-testid="utm-empty">
        No {utmHeader} data yet.
      </p>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-slate-200 text-left text-xs font-medium uppercase tracking-wide text-slate-400">
            <th className="py-2 pr-4">{utmHeader}</th>
            <th className="py-2 pr-4">Link</th>
            <th className="py-2 text-right">Clicks</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr
              key={`${r.utm_value}-${r.link_id}`}
              className="border-b border-slate-100 last:border-0"
            >
              <td className="py-2 pr-4">
                <span className="rounded-full bg-indigo-50 px-2 py-0.5 text-xs font-medium text-indigo-700 ring-1 ring-inset ring-indigo-200">
                  {r.utm_value}
                </span>
              </td>
              <td className="max-w-0 py-2 pr-4">
                <RouterLink
                  to={`/links/${r.link_id}`}
                  className="font-mono text-xs font-medium text-indigo-600 hover:text-indigo-500"
                >
                  /{r.code}
                </RouterLink>
                <p className="truncate text-xs text-slate-400" title={r.url}>
                  {r.url}
                </p>
              </td>
              <td className="py-2 text-right tabular-nums font-medium text-slate-900">{r.clicks}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default function AnalyticsPage() {
  usePageTitle("Analytics");

  const [analysis, setAnalysis] = useState<UTMAnalysis | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  // LIMIT_REACHED (analytics viewing gated by the deployment's policy): the
  // server's message, shown verbatim as a notice in place of the tables.
  const [limitNotice, setLimitNotice] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    setLimitNotice("");

    try {
      setAnalysis(await endpoints.utmAnalysis());
    } catch (err: unknown) {
      setAnalysis(null);

      if (err instanceof ApiError && err.code === LIMIT_REACHED) {
        setLimitNotice(err.message);
      } else {
        setError("Could not load the UTM analysis. Please try again.");
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) return <PageLoader />;

  const allEmpty =
    analysis !== null &&
    analysis.source_analysis.length === 0 &&
    analysis.medium_analysis.length === 0 &&
    analysis.campaign_analysis.length === 0;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-slate-900">Analytics</h1>
        <p className="mt-0.5 text-sm text-slate-500">
          UTM campaign performance across your links. Per-link click charts live on each link's
          detail page.
        </p>
      </div>

      {error && <ErrorBanner message={error} onRetry={() => void load()} />}
      {limitNotice && <NoticeBanner message={limitNotice} />}

      {allEmpty && (
        <div className="rounded-xl border border-dashed border-slate-300 bg-white">
          <EmptyState
            icon={<EmptyIcon path={emptyIconPaths.chart} />}
            title="No UTM data yet"
            hint="Clicks are tagged automatically as they happen, and links created with explicit UTM parameters report under those. Share a short link to see data here."
            action={
              <RouterLink
                to="/links"
                className="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500"
              >
                Go to links
              </RouterLink>
            }
            testid="utm-all-empty"
          />
        </div>
      )}

      {analysis && !allEmpty && (
        <div className="grid gap-6 xl:grid-cols-3">
          {SECTIONS.map((s) => (
            <section
              key={s.key}
              className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6"
              data-testid={`utm-section-${s.key}`}
            >
              <h2 className="text-base font-semibold text-slate-900">{s.title}</h2>
              <p className="mt-0.5 mb-3 text-xs text-slate-500">{s.blurb}</p>
              <UTMTable rows={analysis[s.key]} utmHeader={s.utmHeader} />
            </section>
          ))}
        </div>
      )}
    </div>
  );
}
