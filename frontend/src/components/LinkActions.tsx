// On-demand link management (UI restructure plan): the QR / alias / edit-
// destination modals and the targeting drawer, plus the kebab-menu items that
// open them. Reused by the links list, the link detail header and the
// creation success card — management lives in overlays, not resident forms.

import { useEffect, useState, type FormEvent } from "react";

import { ApiError, endpoints, fetchQRObjectURL } from "../lib/api";
import type { ShortURL } from "../lib/types";
import DeeplinkSection from "./DeeplinkSection";
import TargetRulesSection from "./TargetRulesSection";
import {
  Drawer,
  ErrorBanner,
  Modal,
  Spinner,
  copyToClipboard,
  primaryButtonClass,
  type MenuItem,
} from "./ui";

/** One of the on-demand management surfaces. */
export type LinkAction = "qr" | "alias" | "edit" | "targeting";

const inputClass =
  "w-full rounded-lg border border-slate-300 px-3 py-2 text-sm text-slate-900 shadow-sm " +
  "placeholder:text-slate-400 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200";

// ---------------------------------------------------------------------------
// Kebab items — one definition so the list rows, the detail header and the
// mobile cards offer identical actions.
// ---------------------------------------------------------------------------

/**
 * Builds the shared action-menu items for a link. `onAction` opens the
 * corresponding overlay; `onViewAnalytics` (when given) appends the
 * navigation item — the detail page omits it.
 */
export function linkMenuItems(
  link: ShortURL,
  onAction: (action: LinkAction) => void,
  onViewAnalytics?: () => void,
): MenuItem[] {
  const items: MenuItem[] = [
    {
      label: "Copy short URL",
      onSelect: () => void copyToClipboard(link.short_url),
      testid: "menu-copy",
    },
    { label: "View QR code", onSelect: () => onAction("qr"), testid: "menu-qr" },
    { label: "Change alias", onSelect: () => onAction("alias"), testid: "menu-alias" },
    { label: "Edit destination", onSelect: () => onAction("edit"), testid: "menu-edit" },
    { label: "Manage targeting", onSelect: () => onAction("targeting"), testid: "menu-targeting" },
  ];

  if (onViewAnalytics) {
    items.push({ label: "View analytics", onSelect: onViewAnalytics, testid: "menu-analytics" });
  }

  return items;
}

// ---------------------------------------------------------------------------
// QR panel (modal body + creation success card)
// ---------------------------------------------------------------------------

/**
 * Fetches the link's QR PNG with auth and shows it with a Download button.
 * Fetches on every mount — the QR follows the current short code.
 */
export function QRPanel({ link, size = "lg" }: { link: ShortURL; size?: "sm" | "lg" }) {
  const [qrURL, setQrURL] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    let objectURL = "";
    let cancelled = false;

    fetchQRObjectURL(link.id)
      .then((url) => {
        objectURL = url;
        if (!cancelled) setQrURL(url);
      })
      .catch(() => {
        if (!cancelled) setError("Could not load the QR code.");
      });

    return () => {
      cancelled = true;
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
    // Re-fetch when the code (and therefore the short URL / QR) changes.
  }, [link.id, link.code]);

  const box = size === "sm" ? "h-32 w-32" : "h-52 w-52";

  if (error) return <ErrorBanner message={error} />;

  return (
    <div className="flex flex-col items-center gap-3">
      {qrURL ? (
        <img
          src={qrURL}
          alt={`QR code for ${link.short_url}`}
          className={`${box} rounded-lg border border-slate-200 bg-white`}
          data-testid="qr-image"
        />
      ) : (
        <div className={`flex ${box} items-center justify-center rounded-lg border border-slate-200 bg-white`}>
          <Spinner />
        </div>
      )}
      <a
        href={qrURL || undefined}
        download={`${link.code}-qr.png`}
        aria-disabled={!qrURL}
        data-testid="qr-download"
        className={`w-full rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-center text-sm font-medium text-slate-700 transition hover:bg-slate-50 ${
          qrURL ? "" : "pointer-events-none opacity-50"
        }`}
      >
        Download PNG
      </a>
      {size === "lg" && (
        <p className="max-w-full truncate text-center text-xs text-slate-400" title={link.short_url}>
          Scans open {link.short_url}
        </p>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Alias editor (modal body) — the existing rules: mirror backend validation,
// friendly 409/422 messages.
// ---------------------------------------------------------------------------

// Mirrors the backend alias rules (services/link.go): 3-50 chars of
// [a-zA-Z0-9_-], no reserved words, nothing starting with a dot.
const ALIAS_RE = /^[a-zA-Z0-9_-]{3,50}$/;
const RESERVED_ALIASES = ["api", "login", "metrics", "favicon.ico", "robots.txt", ".well-known"];

function aliasValidationError(alias: string): string {
  if (!ALIAS_RE.test(alias)) {
    return "Use 3-50 characters: letters, digits, '_' or '-'.";
  }

  if (alias.startsWith(".") || RESERVED_ALIASES.includes(alias.toLowerCase())) {
    return "This alias is reserved.";
  }

  return "";
}

/**
 * Heuristic "has a custom alias" check: generated codes are exactly 7 base62
 * characters, so anything else must be an alias. (A 7-character alias without
 * '_' or '-' is indistinguishable — accepted imprecision for a display chip.)
 */
export function looksLikeAlias(code: string): boolean {
  return !/^[a-zA-Z0-9]{7}$/.test(code);
}

export function AliasEditor({
  link,
  onSaved,
}: {
  link: ShortURL;
  onSaved: (link: ShortURL) => void;
}) {
  const [alias, setAlias] = useState(link.code);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const submit = async (event: FormEvent) => {
    event.preventDefault();

    if (busy) return;

    const next = alias.trim();

    setError("");
    setNotice("");

    if (next === link.code) return;

    const validation = aliasValidationError(next);
    if (validation) {
      setError(validation);
      return;
    }

    setBusy(true);

    try {
      const updated = await endpoints.setAlias(link.id, next);

      setNotice(`Alias set. The old code "${link.code}" no longer resolves.`);
      onSaved(updated);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 409) {
        setError("This alias is already taken by another link.");
      } else if (err instanceof ApiError && err.status === 422) {
        setError(err.message || "This alias is not allowed.");
      } else {
        setError("Could not set the alias. Please try again.");
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <div>
      <p className="text-sm text-slate-500">
        Replace the generated code with a branded alias. The old code stops working.
      </p>

      <form onSubmit={(e) => void submit(e)} className="mt-3 flex flex-col gap-2">
        <label htmlFor="alias-input" className="sr-only">
          Alias
        </label>
        <input
          id="alias-input"
          type="text"
          value={alias}
          onChange={(e) => {
            setAlias(e.target.value);
            setError("");
            setNotice("");
          }}
          className="w-full rounded-lg border border-slate-300 px-3 py-2 font-mono text-sm text-slate-900 shadow-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200"
        />
        <button
          type="submit"
          disabled={busy || alias.trim() === "" || alias.trim() === link.code}
          className={primaryButtonClass}
        >
          {busy && <Spinner className="h-4 w-4 text-white" />}
          {busy ? "Saving…" : "Save alias"}
        </button>
      </form>

      {error && (
        <p className="mt-2 text-sm text-red-600" role="alert" data-testid="alias-error">
          {error}
        </p>
      )}
      {notice && (
        <p className="mt-2 text-sm text-emerald-700" role="status" data-testid="alias-notice">
          {notice}
        </p>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Edit destination (modal body) — PATCH /api/v1/links/{id}
// ---------------------------------------------------------------------------

export function EditDestinationForm({
  link,
  onSaved,
}: {
  link: ShortURL;
  onSaved: (link: ShortURL) => void;
}) {
  const [url, setUrl] = useState(link.destination_url);
  const [utmSource, setUtmSource] = useState(link.utm_source ?? "");
  const [utmMedium, setUtmMedium] = useState(link.utm_medium ?? "");
  const [utmCampaign, setUtmCampaign] = useState(link.utm_campaign ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const clearFeedback = () => {
    setError("");
    setNotice("");
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();

    const trimmed = url.trim();
    if (!trimmed || busy) return;

    setBusy(true);
    clearFeedback();

    try {
      const updated = await endpoints.editLink(link.id, {
        url: trimmed,
        utm_source: utmSource.trim() || undefined,
        utm_medium: utmMedium.trim() || undefined,
        utm_campaign: utmCampaign.trim() || undefined,
      });

      setUrl(updated.destination_url);
      setNotice("Destination updated. The short link now redirects there.");
      onSaved(updated);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 403) {
        setError("Only the link's creator or an org owner can edit its destination.");
      } else if (err instanceof ApiError && err.status === 422) {
        setError(err.message || "This destination is not a valid URL.");
      } else {
        setError("Could not update the destination. Please try again.");
      }
    } finally {
      setBusy(false);
    }
  };

  const utmFields = [
    { id: "edit-utm-source", label: "UTM source", value: utmSource, set: setUtmSource },
    { id: "edit-utm-medium", label: "UTM medium", value: utmMedium, set: setUtmMedium },
    { id: "edit-utm-campaign", label: "UTM campaign", value: utmCampaign, set: setUtmCampaign },
  ];

  return (
    <div>
      <p className="text-sm text-slate-500">
        Repoint <span className="font-mono text-slate-700">/{link.code}</span> at a new
        destination. Printed QR codes and published short links keep working; clicks keep
        accruing to this link.
      </p>

      <form onSubmit={(e) => void submit(e)} className="mt-3 space-y-3" data-testid="edit-destination-form">
        <div>
          <label htmlFor="edit-destination-url" className="block text-xs font-medium text-slate-600">
            Destination URL
          </label>
          <input
            id="edit-destination-url"
            type="text"
            inputMode="url"
            value={url}
            onChange={(e) => {
              setUrl(e.target.value);
              clearFeedback();
            }}
            className={`mt-1 ${inputClass}`}
            data-testid="edit-destination-url"
          />
        </div>

        <div className="grid gap-3 sm:grid-cols-3">
          {utmFields.map((f) => (
            <div key={f.id}>
              <label htmlFor={f.id} className="block text-xs font-medium text-slate-600">
                {f.label}
              </label>
              <input
                id={f.id}
                type="text"
                value={f.value}
                onChange={(e) => {
                  f.set(e.target.value);
                  clearFeedback();
                }}
                className={`mt-1 ${inputClass}`}
              />
            </div>
          ))}
        </div>

        <button
          type="submit"
          disabled={busy || url.trim() === ""}
          className={primaryButtonClass}
          data-testid="edit-destination-save"
        >
          {busy && <Spinner className="h-4 w-4 text-white" />}
          {busy ? "Saving…" : "Save destination"}
        </button>
      </form>

      {error && (
        <p className="mt-2 text-sm text-red-600" role="alert" data-testid="edit-destination-error">
          {error}
        </p>
      )}
      {notice && (
        <p className="mt-2 text-sm text-emerald-700" role="status" data-testid="edit-destination-notice">
          {notice}
        </p>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Overlay host: one component that renders the right modal/drawer for an
// action. Mount with a non-null action; it calls onClose for Esc/backdrop/X.
// ---------------------------------------------------------------------------

export function LinkActionOverlays({
  link,
  action,
  onClose,
  onUpdated,
}: {
  link: ShortURL;
  action: LinkAction | null;
  onClose: () => void;
  /** Called with the fresh link after alias / destination / deeplink saves. */
  onUpdated: (link: ShortURL) => void;
}) {
  if (!action) return null;

  if (action === "qr") {
    return (
      <Modal title="QR code" onClose={onClose} testid="qr-modal" maxWidth="max-w-sm">
        <QRPanel link={link} />
      </Modal>
    );
  }

  if (action === "alias") {
    return (
      <Modal title="Custom alias" onClose={onClose} testid="alias-modal">
        <AliasEditor link={link} onSaved={onUpdated} />
      </Modal>
    );
  }

  if (action === "edit") {
    return (
      <Modal title="Edit destination" onClose={onClose} testid="edit-modal" maxWidth="max-w-lg">
        <EditDestinationForm link={link} onSaved={onUpdated} />
      </Modal>
    );
  }

  return (
    <Drawer title={`Targeting — /${link.code}`} onClose={onClose} testid="targeting-drawer">
      <div className="space-y-4">
        <DeeplinkSection link={link} onUpdated={onUpdated} />
        <TargetRulesSection link={link} />
      </div>
    </Drawer>
  );
}
