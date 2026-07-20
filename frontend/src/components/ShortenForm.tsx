// Shorten form: URL input plus optional advanced fields (visibility type and
// UTM parameters). Used by the Links page and the home quick-shorten box.
// A successful shorten shows the creation success card: short URL + copy, the
// QR code with download, and quick actions (alias / targeting / analytics)
// scoped to the new link — creation time is when users actually want these.

import { useRef, useState, type FormEvent } from "react";
import { Link as RouterLink } from "react-router-dom";

import { ApiError, LIMIT_REACHED, endpoints } from "../lib/api";
import type { LinkType, ShortURL } from "../lib/types";
import { LinkActionOverlays, QRPanel, type LinkAction } from "./LinkActions";
import { CopyButton, ErrorBanner, NoticeBanner, Spinner, secondaryButtonClass } from "./ui";

export default function ShortenForm({
  onCreated,
  onChanged,
}: {
  onCreated?: (link: ShortURL) => void;
  /** Called when a success-card overlay changed the new link (alias, edit…). */
  onChanged?: () => void;
}) {
  const [url, setUrl] = useState("");
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [type, setType] = useState<LinkType>("PUBLIC");
  const [utmSource, setUtmSource] = useState("");
  const [utmMedium, setUtmMedium] = useState("");
  const [utmCampaign, setUtmCampaign] = useState("");

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // LIMIT_REACHED denial: the server's message, shown verbatim as a notice.
  const [limitNotice, setLimitNotice] = useState("");
  const [created, setCreated] = useState<ShortURL | null>(null);

  // Success-card overlays (scoped to the just-created link).
  const [action, setAction] = useState<LinkAction | null>(null);
  const dirty = useRef(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();

    const trimmed = url.trim();
    if (!trimmed || busy) return;

    setBusy(true);
    setError("");
    setLimitNotice("");
    setCreated(null);
    setAction(null);

    try {
      const link = await endpoints.shorten({
        url: trimmed,
        type,
        utm_source: utmSource.trim() || undefined,
        utm_medium: utmMedium.trim() || undefined,
        utm_campaign: utmCampaign.trim() || undefined,
      });

      setCreated(link);
      setUrl("");
      setUtmSource("");
      setUtmMedium("");
      setUtmCampaign("");
      onCreated?.(link);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.code === LIMIT_REACHED) {
        setLimitNotice(err.message);
      } else {
        setError(
          err instanceof ApiError && err.message
            ? err.message
            : "Could not shorten this URL. Please try again.",
        );
      }
    } finally {
      setBusy(false);
    }
  };

  const handleUpdated = (updated: ShortURL) => {
    dirty.current = true;
    setCreated(updated);
  };

  const closeAction = () => {
    setAction(null);

    if (dirty.current) {
      dirty.current = false;
      onChanged?.();
    }
  };

  const inputClass =
    "w-full rounded-lg border border-slate-300 px-3 py-2 text-sm text-slate-900 shadow-sm " +
    "placeholder:text-slate-400 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200";

  return (
    <>
    <form onSubmit={submit} className="space-y-3" data-testid="shorten-form">
      <div className="flex flex-col gap-2 sm:flex-row">
        <label htmlFor="shorten-url" className="sr-only">
          URL to shorten
        </label>
        <input
          id="shorten-url"
          type="text"
          inputMode="url"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="Paste a long URL, e.g. example.com/some/long/path"
          required
          className={`flex-1 ${inputClass}`}
        />
        <button
          type="submit"
          disabled={busy || url.trim() === ""}
          className="flex items-center justify-center gap-2 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-semibold text-white shadow-sm transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {busy && <Spinner className="h-4 w-4 text-white" />}
          {busy ? "Shortening…" : "Shorten"}
        </button>
      </div>

      <button
        type="button"
        onClick={() => setShowAdvanced((v) => !v)}
        className="text-xs font-medium text-indigo-600 hover:text-indigo-500"
        aria-expanded={showAdvanced}
      >
        {showAdvanced ? "Hide options" : "More options (visibility, UTM)"}
      </button>

      {showAdvanced && (
        <div className="grid gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3 sm:grid-cols-2">
          <div>
            <label htmlFor="shorten-type" className="block text-xs font-medium text-slate-600">
              Visibility
            </label>
            <select
              id="shorten-type"
              value={type}
              onChange={(e) => setType(e.target.value as LinkType)}
              className={`mt-1 ${inputClass}`}
            >
              <option value="PUBLIC">Public — visible to your whole organization</option>
              <option value="PRIVATE">Private — listed only to you</option>
            </select>
          </div>
          <div>
            <label htmlFor="shorten-utm-source" className="block text-xs font-medium text-slate-600">
              UTM source
            </label>
            <input
              id="shorten-utm-source"
              type="text"
              value={utmSource}
              onChange={(e) => setUtmSource(e.target.value)}
              placeholder="newsletter"
              className={`mt-1 ${inputClass}`}
            />
          </div>
          <div>
            <label htmlFor="shorten-utm-medium" className="block text-xs font-medium text-slate-600">
              UTM medium
            </label>
            <input
              id="shorten-utm-medium"
              type="text"
              value={utmMedium}
              onChange={(e) => setUtmMedium(e.target.value)}
              placeholder="email"
              className={`mt-1 ${inputClass}`}
            />
          </div>
          <div>
            <label htmlFor="shorten-utm-campaign" className="block text-xs font-medium text-slate-600">
              UTM campaign
            </label>
            <input
              id="shorten-utm-campaign"
              type="text"
              value={utmCampaign}
              onChange={(e) => setUtmCampaign(e.target.value)}
              placeholder="spring-launch"
              className={`mt-1 ${inputClass}`}
            />
          </div>
        </div>
      )}

      {error && <ErrorBanner message={error} />}
      {limitNotice && <NoticeBanner message={limitNotice} />}

      {created && (
        <div
          className="rounded-xl border border-emerald-200 bg-emerald-50/60 p-4"
          role="status"
          data-testid="shorten-result"
        >
          <div className="flex items-start justify-between gap-2">
            <p className="text-sm font-semibold text-emerald-800">Your short link is ready</p>
            <button
              type="button"
              onClick={() => setCreated(null)}
              aria-label="Dismiss"
              data-testid="success-dismiss"
              className="shrink-0 rounded-md p-1 text-emerald-600 transition hover:bg-emerald-100"
            >
              <svg className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
                <path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22z" />
              </svg>
            </button>
          </div>

          <div className="mt-3 flex flex-col gap-4 sm:flex-row">
            <div className="mx-auto w-40 shrink-0 sm:mx-0" data-testid="success-qr">
              <QRPanel link={created} size="sm" />
            </div>

            <div className="min-w-0 flex-1 space-y-3">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <a
                  href={created.short_url}
                  target="_blank"
                  rel="noreferrer"
                  className="min-w-0 max-w-full truncate font-mono text-sm font-semibold text-emerald-900 underline decoration-emerald-300 underline-offset-2"
                >
                  {created.short_url}
                </a>
                <CopyButton text={created.short_url} />
              </div>

              <p
                className="truncate text-xs text-emerald-700/80"
                title={created.destination_url}
              >
                to {created.destination_url}
              </p>

              <div className="flex flex-wrap gap-2">
                <button
                  type="button"
                  onClick={() => setAction("alias")}
                  className={`${secondaryButtonClass} px-3 py-1.5`}
                  data-testid="success-alias"
                >
                  Add custom alias
                </button>
                <button
                  type="button"
                  onClick={() => setAction("targeting")}
                  className={`${secondaryButtonClass} px-3 py-1.5`}
                  data-testid="success-targeting"
                >
                  Set up targeting
                </button>
                <RouterLink
                  to={`/links/${created.id}`}
                  className={`${secondaryButtonClass} px-3 py-1.5`}
                  data-testid="success-analytics"
                >
                  View analytics
                </RouterLink>
              </div>
            </div>
          </div>

        </div>
      )}
    </form>

    {/* Overlays live outside the <form>: the alias/edit modals contain their
        own forms, and nesting them would bubble submits into the shorten form. */}
    {created && action && (
      <LinkActionOverlays
        link={created}
        action={action}
        onClose={closeAction}
        onUpdated={handleUpdated}
      />
    )}
    </>
  );
}
