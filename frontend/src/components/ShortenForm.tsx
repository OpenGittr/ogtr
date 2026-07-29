// Shorten form: URL input plus optional advanced fields (visibility type and
// UTM parameters). Used by the Links page and the home quick-shorten box.
// A successful shorten shows the creation success card: short URL + copy, the
// QR code with download, and quick actions (alias / targeting / analytics)
// scoped to the new link — creation time is when users actually want these.

import { useRef, useState, type FormEvent } from "react";
import { Link as RouterLink } from "react-router-dom";

import { ApiError, LIMIT_REACHED, endpoints } from "../lib/api";
import { limitNoticeAction } from "../ext";
import type { DeeplinkConfig, LinkType, RuleInput, ShortURL } from "../lib/types";
import { LinkActionOverlays, QRPanel, type LinkAction } from "./LinkActions";
import { CopyButton, ErrorBanner, NoticeBanner, Spinner, secondaryButtonClass } from "./ui";

// Small inline icon set (stroke = currentColor) used to make the create form
// scannable and to advertise the per-link features visually.
const icons = {
  link: "M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71",
  pencil: "M12 20h9M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4 12.5-12.5z",
  tag: "M20.59 13.41 12 22l-8.59-8.59A2 2 0 0 1 3 12V4a1 1 0 0 1 1-1h8a2 2 0 0 1 1.41.59L22 12zM7 7h.01",
  eye: "M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z",
  phone: "M8 2h8a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2zm4 16h.01",
  target: "M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zm0-5a4 4 0 1 0 0-8 4 4 0 0 0 0 8zm0-4h.01",
  qr: "M3 3h6v6H3zM15 3h6v6h-6zM3 15h6v6H3zM15 15h2v2h-2zM19 15h2v2h-2zM15 19h2v2h-2zM19 19h2v2h-2z",
  chart: "M3 3v18h18M8 17V9m5 8V5m5 12v-6",
  clock: "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM12 6v6l4 2",
} as const;

function Icon({ path, className = "h-4 w-4" }: { path: string; className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={path} />
    </svg>
  );
}

// A create-time draft rule: one OS + one city + a destination. The full rule
// editor (multi-value, is/is-not, city autocomplete) lives on the link detail
// page; this compact builder covers the common case at creation.
interface DraftRule {
  device: "any" | "iOS" | "Android" | "Windows" | "Other";
  city: string;
  url: string;
}

const DEVICE_OPTIONS = ["any", "iOS", "Android", "Windows", "Other"] as const;

function buildDeeplink(dl: {
  iosIntent: string;
  andIntent: string;
  andPackage: string;
  andScheme: string;
  andFallback: string;
}): DeeplinkConfig | undefined {
  const ios = dl.iosIntent.trim() ? { intent: dl.iosIntent.trim() } : undefined;
  const android =
    dl.andIntent.trim() && dl.andPackage.trim() && dl.andScheme.trim() && dl.andFallback.trim()
      ? {
          intent: dl.andIntent.trim(),
          package: dl.andPackage.trim(),
          scheme: dl.andScheme.trim(),
          fallback_url: dl.andFallback.trim(),
        }
      : undefined;

  return ios || android ? { ios, android } : undefined;
}

function buildRules(drafts: DraftRule[]): RuleInput[] | undefined {
  const valid = drafts
    .filter((r) => r.url.trim() && (r.device !== "any" || r.city.trim()))
    .map((r, i) => ({
      target_name: `Rule ${i + 1}`,
      url: r.url.trim(),
      device_type: r.device !== "any" ? { type: "is" as const, values: [r.device] } : undefined,
      location: r.city.trim() ? { type: "is" as const, values: [r.city.trim()] } : undefined,
    }));

  return valid.length ? valid : undefined;
}

export default function ShortenForm({
  onCreated,
  onChanged,
}: {
  onCreated?: (link: ShortURL) => void;
  /** Called when a success-card overlay changed the new link (alias, edit…). */
  onChanged?: () => void;
}) {
  const [url, setUrl] = useState("");
  const [showAlias, setShowAlias] = useState(false);
  const [showUtm, setShowUtm] = useState(false);
  const [showVisibility, setShowVisibility] = useState(false);
  const [showDeeplinks, setShowDeeplinks] = useState(false);
  const [showTargeting, setShowTargeting] = useState(false);
  const [showSchedule, setShowSchedule] = useState(false);
  const [alias, setAlias] = useState("");
  const [type, setType] = useState<LinkType>("PUBLIC");
  const [utmSource, setUtmSource] = useState("");
  const [utmMedium, setUtmMedium] = useState("");
  const [utmCampaign, setUtmCampaign] = useState("");
  // Deep link (flat fields, assembled into a DeeplinkConfig on submit).
  const [dl, setDl] = useState({ iosIntent: "", andIntent: "", andPackage: "", andScheme: "", andFallback: "" });
  // Draft target rules and the activation/expiry window.
  const [rules, setRules] = useState<DraftRule[]>([]);
  const [activatesAt, setActivatesAt] = useState("");
  const [expiresAt, setExpiresAt] = useState("");

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
        alias: alias.trim() || undefined,
        utm_source: utmSource.trim() || undefined,
        utm_medium: utmMedium.trim() || undefined,
        utm_campaign: utmCampaign.trim() || undefined,
        deeplink_config: buildDeeplink(dl),
        rules: buildRules(rules),
        activates_at: activatesAt ? new Date(activatesAt).toISOString() : undefined,
        expires_at: expiresAt ? new Date(expiresAt).toISOString() : undefined,
      });

      setCreated(link);
      setUrl("");
      setAlias("");
      setUtmSource("");
      setUtmMedium("");
      setUtmCampaign("");
      setDl({ iosIntent: "", andIntent: "", andPackage: "", andScheme: "", andFallback: "" });
      setRules([]);
      setActivatesAt("");
      setExpiresAt("");
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
    "w-full rounded-lg border border-slate-300 dark:border-slate-700 px-3 py-2 text-sm text-slate-900 dark:text-white shadow-sm " +
    "placeholder:text-slate-400 dark:placeholder:text-slate-500 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200 dark:focus:ring-indigo-500/30";

  // Feature pills below the input: each names an optional capability (so it's
  // discoverable at a glance) and toggles its own section. A filled pill = open.
  const pill = (active: boolean) =>
    "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition " +
    (active
      ? "border-indigo-300 dark:border-indigo-500/50 bg-indigo-50 dark:bg-indigo-500/10 text-indigo-700 dark:text-indigo-300"
      : "border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-900 text-slate-600 dark:text-slate-300 hover:border-indigo-400 dark:hover:border-indigo-500 hover:text-indigo-600 dark:hover:text-indigo-400");

  const updateRule = (i: number, patch: Partial<DraftRule>) =>
    setRules((rs) => rs.map((r, j) => (j === i ? { ...r, ...patch } : r)));

  const fieldClass =
    "rounded-md border border-slate-300 bg-white px-2 py-1 text-xs text-slate-900 dark:border-slate-700 dark:bg-slate-900 dark:text-white";

  return (
    <>
    <form onSubmit={submit} className="space-y-3" data-testid="shorten-form">
      <div className="flex flex-col gap-2 sm:flex-row">
        <label htmlFor="shorten-url" className="sr-only">
          URL to shorten
        </label>
        <div className="relative flex-1">
          <span className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-slate-400 dark:text-slate-500">
            <Icon path={icons.link} className="h-5 w-5" />
          </span>
          <input
            id="shorten-url"
            type="text"
            inputMode="url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="Paste a long URL, e.g. example.com/some/long/path"
            required
            className="w-full rounded-xl border border-slate-300 bg-white py-3 pl-11 pr-3 text-base text-slate-900 shadow-sm transition placeholder:text-slate-400 focus:border-indigo-500 focus:outline-none focus:ring-4 focus:ring-indigo-500/10 dark:border-slate-700 dark:bg-slate-900 dark:text-white dark:placeholder:text-slate-500 dark:focus:ring-indigo-500/20"
          />
        </div>
        <button
          type="submit"
          disabled={busy || url.trim() === ""}
          className="flex items-center justify-center gap-2 rounded-xl bg-indigo-600 px-6 py-3 text-base font-semibold text-white shadow-sm transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {busy && <Spinner className="h-4 w-4 text-white" />}
          {busy ? "Shortening…" : "Shorten"}
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <span className="mr-0.5 text-xs font-medium text-slate-400 dark:text-slate-500">Add</span>
        <button
          type="button"
          onClick={() => setShowAlias((v) => !v)}
          aria-expanded={showAlias}
          data-testid="toggle-alias"
          className={pill(showAlias || alias.trim() !== "")}
        >
          <Icon path={icons.pencil} className="h-3.5 w-3.5" />
          Custom link{alias.trim() ? ` · /${alias.trim()}` : ""}
        </button>
        <button
          type="button"
          onClick={() => setShowUtm((v) => !v)}
          aria-expanded={showUtm}
          data-testid="toggle-utm"
          className={pill(showUtm)}
        >
          <Icon path={icons.tag} className="h-3.5 w-3.5" />
          Campaign tracking
        </button>
        <button
          type="button"
          onClick={() => setShowVisibility((v) => !v)}
          aria-expanded={showVisibility}
          data-testid="toggle-visibility"
          className={pill(showVisibility || type === "PRIVATE")}
        >
          <Icon path={icons.eye} className="h-3.5 w-3.5" />
          Visibility{type === "PRIVATE" ? " · private" : ""}
        </button>
        <button
          type="button"
          onClick={() => setShowDeeplinks((v) => !v)}
          aria-expanded={showDeeplinks}
          data-testid="toggle-deeplinks"
          className={pill(showDeeplinks || dl.iosIntent.trim() !== "" || dl.andIntent.trim() !== "")}
        >
          <Icon path={icons.phone} className="h-3.5 w-3.5" />
          Deep links
        </button>
        <button
          type="button"
          onClick={() => setShowTargeting((v) => !v)}
          aria-expanded={showTargeting}
          data-testid="toggle-targeting"
          className={pill(showTargeting || rules.length > 0)}
        >
          <Icon path={icons.target} className="h-3.5 w-3.5" />
          Targeting{rules.length > 0 ? ` · ${rules.length}` : ""}
        </button>
        <button
          type="button"
          onClick={() => setShowSchedule((v) => !v)}
          aria-expanded={showSchedule}
          data-testid="toggle-schedule"
          className={pill(showSchedule || activatesAt !== "" || expiresAt !== "")}
        >
          <Icon path={icons.clock} className="h-3.5 w-3.5" />
          Schedule
        </button>
      </div>

      {showAlias && (
        <div className="rounded-lg border border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-900 p-3">
          <label htmlFor="shorten-alias" className="block text-xs font-medium text-slate-600 dark:text-slate-300">
            Custom back-half
          </label>
          <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
            Pick your own short link instead of a random code. 3–50 letters, digits, “-” or “_”.
          </p>
          <div className="mt-2 flex items-center gap-2">
            <span className="shrink-0 font-mono text-xs text-slate-400 dark:text-slate-500">/</span>
            <input
              id="shorten-alias"
              type="text"
              value={alias}
              onChange={(e) => setAlias(e.target.value)}
              placeholder="spring-sale"
              className={inputClass}
              data-testid="shorten-alias"
            />
          </div>
        </div>
      )}

      {showUtm && (
        <div className="rounded-lg border border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-900 p-3">
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Tag this link so its clicks land in your campaign reports (GA4 and ogtr’s own
            analytics). All optional.
          </p>
          <div className="mt-2 grid gap-3 sm:grid-cols-3">
            <div>
              <label htmlFor="shorten-utm-source" className="block text-xs font-medium text-slate-600 dark:text-slate-300">
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
              <label htmlFor="shorten-utm-medium" className="block text-xs font-medium text-slate-600 dark:text-slate-300">
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
              <label htmlFor="shorten-utm-campaign" className="block text-xs font-medium text-slate-600 dark:text-slate-300">
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
        </div>
      )}

      {showVisibility && (
        <div className="rounded-lg border border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-900 p-3">
          <label htmlFor="shorten-type" className="block text-xs font-medium text-slate-600 dark:text-slate-300">
            Who can see this link in the dashboard?
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
      )}

      {showDeeplinks && (
        <div className="rounded-lg border border-slate-200 bg-slate-50 p-3 dark:border-slate-800 dark:bg-slate-900">
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Open the link in your mobile app when it’s installed. Fill only the platforms you
            support (your app needs its own Universal Links / App Links set up).
          </p>
          <div className="mt-2 space-y-2">
            <input
              type="text"
              value={dl.iosIntent}
              onChange={(e) => setDl({ ...dl, iosIntent: e.target.value })}
              placeholder="iOS app link — e.g. myapp://path"
              className={`w-full ${inputClass}`}
              data-testid="dl-ios"
            />
            <div className="grid gap-2 sm:grid-cols-2">
              <input type="text" value={dl.andIntent} onChange={(e) => setDl({ ...dl, andIntent: e.target.value })} placeholder="Android intent — path/open" className={`w-full ${inputClass}`} />
              <input type="text" value={dl.andPackage} onChange={(e) => setDl({ ...dl, andPackage: e.target.value })} placeholder="Android package — com.example.app" className={`w-full ${inputClass}`} />
              <input type="text" value={dl.andScheme} onChange={(e) => setDl({ ...dl, andScheme: e.target.value })} placeholder="Android scheme — exampleapp" className={`w-full ${inputClass}`} />
              <input type="text" value={dl.andFallback} onChange={(e) => setDl({ ...dl, andFallback: e.target.value })} placeholder="Android fallback URL" className={`w-full ${inputClass}`} />
            </div>
            <p className="text-[11px] text-slate-400 dark:text-slate-500">Android needs all four fields; iOS needs just the app link.</p>
          </div>
        </div>
      )}

      {showTargeting && (
        <div className="rounded-lg border border-slate-200 bg-slate-50 p-3 dark:border-slate-800 dark:bg-slate-900">
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Send some visitors elsewhere by device or city. Rules are checked top to bottom — first
            match wins; everyone else gets the main destination.
          </p>
          <div className="mt-2 space-y-2">
            {rules.map((r, i) => (
              <div key={i} className="flex flex-wrap items-center gap-1.5 rounded-md border border-slate-200 bg-white p-2 dark:border-slate-700 dark:bg-slate-800/60">
                <span className="text-xs text-slate-500 dark:text-slate-400">If</span>
                <select value={r.device} onChange={(e) => updateRule(i, { device: e.target.value as DraftRule["device"] })} className={fieldClass}>
                  {DEVICE_OPTIONS.map((o) => (
                    <option key={o} value={o}>{o === "any" ? "any device" : o}</option>
                  ))}
                </select>
                <span className="text-xs text-slate-500 dark:text-slate-400">in</span>
                <input type="text" value={r.city} onChange={(e) => updateRule(i, { city: e.target.value })} placeholder="any city" className={`w-24 ${fieldClass}`} />
                <span className="text-xs text-slate-400 dark:text-slate-500">→</span>
                <input type="text" inputMode="url" value={r.url} onChange={(e) => updateRule(i, { url: e.target.value })} placeholder="https://destination" className={`min-w-0 flex-1 ${fieldClass}`} />
                <button type="button" onClick={() => setRules(rules.filter((_, j) => j !== i))} aria-label="Remove rule" className="rounded p-1 text-slate-400 transition hover:text-red-500 dark:text-slate-500">
                  <Icon path="M18 6 6 18M6 6l12 12" className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
            <button type="button" onClick={() => setRules([...rules, { device: "any", city: "", url: "" }])} className="text-xs font-medium text-indigo-600 hover:text-indigo-500 dark:text-indigo-400" data-testid="add-rule">
              + Add rule
            </button>
          </div>
        </div>
      )}

      {showSchedule && (
        <div className="rounded-lg border border-slate-200 bg-slate-50 p-3 dark:border-slate-800 dark:bg-slate-900">
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Make the link go live and/or expire on a schedule. Your local timezone; leave a field
            blank for no bound.
          </p>
          <div className="mt-2 grid gap-3 sm:grid-cols-2">
            <div>
              <label htmlFor="shorten-activates" className="block text-xs font-medium text-slate-600 dark:text-slate-300">Activates at</label>
              <input id="shorten-activates" type="datetime-local" value={activatesAt} onChange={(e) => setActivatesAt(e.target.value)} className={`mt-1 ${inputClass}`} />
            </div>
            <div>
              <label htmlFor="shorten-expires" className="block text-xs font-medium text-slate-600 dark:text-slate-300">Expires at</label>
              <input id="shorten-expires" type="datetime-local" value={expiresAt} onChange={(e) => setExpiresAt(e.target.value)} className={`mt-1 ${inputClass}`} />
            </div>
          </div>
        </div>
      )}

      {!created && (
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5 rounded-xl bg-slate-50 px-3 py-2.5 text-xs text-slate-500 dark:bg-slate-900/60 dark:text-slate-400">
          <span className="mr-1 font-medium text-slate-600 dark:text-slate-300">Every link also gets</span>
          {[
            { icon: icons.qr, label: "QR code" },
            { icon: icons.phone, label: "Deep links" },
            { icon: icons.target, label: "Targeting" },
            { icon: icons.chart, label: "Analytics" },
          ].map((f) => (
            <span
              key={f.label}
              className="inline-flex items-center gap-1 rounded-md bg-white px-2 py-1 font-medium text-slate-600 ring-1 ring-inset ring-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:ring-slate-700"
            >
              <Icon path={f.icon} className="h-3.5 w-3.5 text-indigo-500 dark:text-indigo-400" />
              {f.label}
            </span>
          ))}
          <span className="text-slate-400 dark:text-slate-500">— set them up above, or on the link later</span>
        </div>
      )}

      {error && <ErrorBanner message={error} />}
      {limitNotice && <NoticeBanner message={limitNotice} action={limitNoticeAction} />}

      {created && (
        <div
          className="rounded-xl border border-emerald-200 dark:border-emerald-500/30 bg-emerald-50/60 dark:bg-emerald-500/10 p-4"
          role="status"
          data-testid="shorten-result"
        >
          <div className="flex items-start justify-between gap-2">
            <p className="text-sm font-semibold text-emerald-800 dark:text-emerald-300">Your short link is ready</p>
            <button
              type="button"
              onClick={() => setCreated(null)}
              aria-label="Dismiss"
              data-testid="success-dismiss"
              className="shrink-0 rounded-md p-1 text-emerald-600 dark:text-emerald-400 transition hover:bg-emerald-100 dark:hover:bg-emerald-500/20"
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
                  className="min-w-0 max-w-full truncate font-mono text-sm font-semibold text-emerald-900 dark:text-emerald-200 underline decoration-emerald-300 underline-offset-2"
                >
                  {created.short_url}
                </a>
                <CopyButton text={created.short_url} />
              </div>

              <p
                className="truncate text-xs text-emerald-700/80 dark:text-emerald-400/80"
                title={created.destination_url}
              >
                to {created.destination_url}
              </p>

              {/* The teaching moment: a first-timer sees the whole per-link
                  toolkit the instant they create their first link. */}
              <div className="flex flex-wrap gap-2">
                <button
                  type="button"
                  onClick={() => setAction("alias")}
                  className={`${secondaryButtonClass} px-3 py-1.5`}
                  data-testid="success-alias"
                >
                  <Icon path={icons.pencil} className="h-3.5 w-3.5" />
                  Custom alias
                </button>
                <button
                  type="button"
                  onClick={() => setAction("deeplinks")}
                  className={`${secondaryButtonClass} px-3 py-1.5`}
                  data-testid="success-deeplinks"
                >
                  <Icon path={icons.phone} className="h-3.5 w-3.5" />
                  Mobile deep links
                </button>
                <button
                  type="button"
                  onClick={() => setAction("targeting")}
                  className={`${secondaryButtonClass} px-3 py-1.5`}
                  data-testid="success-targeting"
                >
                  <Icon path={icons.target} className="h-3.5 w-3.5" />
                  Targeting
                </button>
                <button
                  type="button"
                  onClick={() => setAction("schedule")}
                  className={`${secondaryButtonClass} px-3 py-1.5`}
                  data-testid="success-schedule"
                >
                  <Icon path={icons.clock} className="h-3.5 w-3.5" />
                  Schedule
                </button>
                <RouterLink
                  to={`/links/${created.id}`}
                  className={`${secondaryButtonClass} px-3 py-1.5`}
                  data-testid="success-analytics"
                >
                  <Icon path={icons.chart} className="h-3.5 w-3.5" />
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
