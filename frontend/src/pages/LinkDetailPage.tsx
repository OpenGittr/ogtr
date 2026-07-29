// Link detail: identity + analytics, full width. The header keeps the short
// URL / destination / meta chips and gains clickable status chips (alias,
// deep links, N rules) plus the same kebab menu as the links list. All
// management (QR, alias, edit destination, targeting) happens in on-demand
// overlays — no resident forms stealing width from analytics.

import { useCallback, useEffect, useState } from "react";
import { Link as RouterLink, useParams } from "react-router-dom";

import AnalyticsSection from "../components/AnalyticsSection";
import {
  LinkActionOverlays,
  QRPanel,
  looksLikeAlias,
  type LinkAction,
} from "../components/LinkActions";
import { formatDateTime } from "../components/LinkList";
import { CopyButton, ErrorBanner, PageLoader } from "../components/ui";
import { ApiError, endpoints } from "../lib/api";
import type { ShortURL } from "../lib/types";
import { usePageTitle } from "../lib/usePageTitle";

export default function LinkDetailPage() {
  const { id } = useParams();
  const linkId = Number(id);

  const [link, setLink] = useState<ShortURL | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [ruleCount, setRuleCount] = useState(0);

  usePageTitle(link ? `/${link.code}` : "Link");

  const load = useCallback(async () => {
    setLoadError("");

    try {
      setLink(await endpoints.link(linkId));
    } catch (err: unknown) {
      setLoadError(
        err instanceof ApiError && err.status === 404
          ? "This link does not exist or you do not have access to it."
          : "Could not load this link. Please try again.",
      );
    } finally {
      setLoading(false);
    }
  }, [linkId]);

  const loadRuleCount = useCallback(async () => {
    try {
      setRuleCount((await endpoints.rules(linkId)).length);
    } catch {
      // The chip is a nicety; the drawer surfaces rule-load errors itself.
    }
  }, [linkId]);

  useEffect(() => {
    if (Number.isInteger(linkId) && linkId > 0) {
      void load();
      void loadRuleCount();
    } else {
      setLoadError("This link does not exist.");
      setLoading(false);
    }
  }, [linkId, load, loadRuleCount]);

  // --- on-demand management overlays ---
  const [action, setAction] = useState<LinkAction | null>(null);

  const closeAction = () => {
    const wasTargeting = action === "targeting";

    setAction(null);

    // Rules may have changed inside the drawer; refresh the "N rules" chip.
    if (wasTargeting) void loadRuleCount();
  };

  if (loading) return <PageLoader />;

  if (loadError || !link) {
    return (
      <div className="space-y-4">
        <ErrorBanner message={loadError || "Could not load this link."} />
        <RouterLink
          to="/links"
          className="inline-flex rounded-lg bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500"
        >
          Back to links
        </RouterLink>
      </div>
    );
  }

  const utms = [
    { label: "Source", value: link.utm_source },
    { label: "Medium", value: link.utm_medium },
    { label: "Campaign", value: link.utm_campaign },
  ].filter((u) => u.value);

  const metaChip =
    "inline-flex max-w-full items-center gap-1.5 rounded-full bg-slate-50 dark:bg-slate-900 px-2.5 py-1 text-xs text-slate-500 dark:text-slate-400 ring-1 ring-inset ring-slate-200 dark:ring-slate-700";

  // Always-visible action buttons: a filled indigo style marks a surface that
  // is already configured, a plain outline one that is available but unused —
  // so every feature is discoverable without opening the kebab menu.
  const actionButton = (active: boolean) =>
    "inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium shadow-sm transition " +
    (active
      ? "border-indigo-300 dark:border-indigo-500/50 bg-indigo-50 dark:bg-indigo-500/10 text-indigo-700 dark:text-indigo-300 hover:bg-indigo-100 dark:hover:bg-indigo-500/20"
      : "border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-900 text-slate-700 dark:text-slate-200 hover:border-indigo-400 dark:hover:border-indigo-500 hover:bg-slate-50 dark:hover:bg-slate-800/60");

  const hasDeeplinks = Boolean(link.deeplink_config?.android || link.deeplink_config?.ios);
  const hasAlias = looksLikeAlias(link.code);

  return (
    <div className="space-y-6">
      <RouterLink to="/links" className="text-sm font-medium text-indigo-600 dark:text-indigo-400 hover:text-indigo-500">
        &larr; All links
      </RouterLink>

      {/* Top: link header + an always-visible QR card (stacks on mobile). */}
      <div className="grid gap-6 lg:grid-cols-3">
      {/* Page header: short URL + destination + status/meta chips + actions */}
      <header className="rounded-xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-4 shadow-sm sm:p-6 lg:col-span-2">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1.5">
              <a
                href={link.short_url}
                target="_blank"
                rel="noreferrer"
                title={link.short_url}
                className="min-w-0 max-w-full truncate font-mono text-lg font-semibold tracking-tight text-slate-900 dark:text-white transition hover:text-indigo-600 dark:hover:text-indigo-400 sm:text-xl"
                data-testid="detail-short-url"
              >
                {link.short_url}
              </a>
              <a
                href={link.short_url}
                target="_blank"
                rel="noreferrer"
                aria-label="Open short link in a new tab"
                className="shrink-0 rounded-md p-1.5 text-slate-400 dark:text-slate-500 ring-1 ring-inset ring-slate-200 dark:ring-slate-700 transition hover:bg-slate-50 dark:hover:bg-slate-800/60 hover:text-indigo-600 dark:hover:text-indigo-400"
              >
                <svg className="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
                  <path d="M11 3a1 1 0 1 0 0 2h2.586l-6.293 6.293a1 1 0 1 0 1.414 1.414L15 6.414V9a1 1 0 1 0 2 0V4a1 1 0 0 0-1-1h-5z" />
                  <path d="M5 5a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2v-3a1 1 0 1 0-2 0v3H5V7h3a1 1 0 0 0 0-2H5z" />
                </svg>
              </a>
              <CopyButton text={link.short_url} />
            </div>

            <div className="mt-1.5 flex min-w-0 items-center gap-2">
              <span className="shrink-0 text-xs font-medium uppercase tracking-wide text-slate-400 dark:text-slate-500">
                to
              </span>
              <p
                className="min-w-0 truncate text-sm text-slate-500 dark:text-slate-400"
                title={link.destination_url}
                data-testid="detail-destination"
              >
                {link.destination_url}
              </p>
              <CopyButton text={link.destination_url} />
            </div>
          </div>

        </div>

        {/* Always-visible action toolbar — every management surface is a labeled
            button (discoverable without opening a menu); a filled one is already
            configured, an outline one is available. QR lives in its own card. */}
        <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-slate-100 dark:border-slate-800 pt-4">
          <button type="button" onClick={() => setAction("edit")} className={actionButton(false)} data-testid="action-edit">
            Edit destination
          </button>
          <button type="button" onClick={() => setAction("alias")} className={actionButton(hasAlias)} data-testid="action-alias">
            {hasAlias ? "Alias set" : "Custom alias"}
          </button>
          <button type="button" onClick={() => setAction("schedule")} className={actionButton(Boolean(link.activates_at || link.expires_at))} data-testid="action-schedule">
            {link.activates_at || link.expires_at ? "Scheduled" : "Schedule"}
          </button>
          <button type="button" onClick={() => setAction("targeting")} className={actionButton(hasDeeplinks || ruleCount > 0)} data-testid="action-targeting">
            Targeting{ruleCount > 0 ? ` · ${ruleCount} ${ruleCount === 1 ? "rule" : "rules"}` : ""}
            {hasDeeplinks && ruleCount === 0 ? " · deep links" : ""}
          </button>
        </div>

        {/* Informational meta chips */}
        <div className="mt-3 flex flex-wrap items-center gap-1.5">
          <span className={metaChip}>
            <span className="font-semibold text-slate-900 dark:text-white" data-testid="detail-visits">
              {link.visits}
            </span>
            {link.visits === 1 ? "visit" : "visits"}
          </span>
          <span className={metaChip}>
            Last visit
            <span className="font-medium text-slate-700 dark:text-slate-200">{formatDateTime(link.last_visit_at)}</span>
          </span>
          <span className={metaChip}>
            Created
            <span className="font-medium text-slate-700 dark:text-slate-200">{formatDateTime(link.created_at)}</span>
          </span>
          {link.type === "PRIVATE" && (
            <span className="rounded-full bg-slate-100 dark:bg-slate-800 px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400 ring-1 ring-inset ring-slate-200 dark:ring-slate-700">
              private
            </span>
          )}
          {link.api_key_id != null && (
            <span
              className="rounded-full bg-sky-50 dark:bg-sky-500/10 px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wide text-sky-600 dark:text-sky-400 ring-1 ring-inset ring-sky-200 dark:ring-sky-500/30"
              title="Created programmatically with an API key"
              data-testid="detail-via-api-badge"
            >
              via API
            </span>
          )}
          {utms.map((u) => (
            <span
              key={u.label}
              className="max-w-full truncate rounded-full bg-indigo-50 dark:bg-indigo-500/10 px-2.5 py-1 text-xs font-medium text-indigo-700 dark:text-indigo-300 ring-1 ring-inset ring-indigo-200 dark:ring-indigo-500/30"
            >
              {u.label}: {u.value}
            </span>
          ))}
        </div>
      </header>

      <aside className="flex flex-col rounded-xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 p-4 shadow-sm sm:p-6">
        <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-slate-400 dark:text-slate-500">
          QR code
        </h2>
        <div className="flex flex-1 items-center justify-center">
          <QRPanel link={link} />
        </div>
      </aside>
      </div>

      {/* Analytics: the full content width (management moved to overlays) */}
      <AnalyticsSection link={link} />

      <LinkActionOverlays
        link={link}
        action={action}
        onClose={closeAction}
        onUpdated={setLink}
      />
    </div>
  );
}
