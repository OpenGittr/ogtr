// Responsive list of short links: table-like rows on desktop, stacked cards
// on mobile. Rows navigate to the link detail page; a per-row kebab menu
// offers the management actions (copy / QR / alias / edit destination /
// targeting / analytics) without leaving the list.

import { useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import type { ShortURL } from "../lib/types";
import { LinkActionOverlays, linkMenuItems, type LinkAction } from "./LinkActions";
import { CopyButton, EmptyIcon, EmptyState, KebabMenu, emptyIconPaths } from "./ui";

export function formatDateTime(iso: string | null): string {
  if (!iso) return "Never";

  const date = new Date(iso);

  return Number.isNaN(date.getTime())
    ? ""
    : date.toLocaleString(undefined, {
        year: "numeric",
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      });
}

export default function LinkList({
  links,
  onChanged,
}: {
  links: ShortURL[];
  /** Called after an overlay that changed a link closes, to refresh the list. */
  onChanged?: () => void;
}) {
  const navigate = useNavigate();

  // One overlay for the whole list, scoped to the row it was opened from.
  // The list is only refreshed when the overlay closes — refreshing earlier
  // would unmount (and thereby close) the overlay mid-edit.
  const [overlay, setOverlay] = useState<{ link: ShortURL; action: LinkAction } | null>(null);
  const dirty = useRef(false);

  const openAction = (link: ShortURL, action: LinkAction) => {
    dirty.current = false;
    setOverlay({ link, action });
  };

  const handleUpdated = (updated: ShortURL) => {
    dirty.current = true;
    setOverlay((o) => (o ? { ...o, link: updated } : o));
  };

  const closeOverlay = () => {
    setOverlay(null);

    if (dirty.current) {
      dirty.current = false;
      onChanged?.();
    }
  };

  if (links.length === 0) {
    return (
      <EmptyState
        icon={<EmptyIcon path={emptyIconPaths.link} />}
        title="No links yet"
        hint="Shorten your first URL with the form above."
        testid="links-empty"
      />
    );
  }

  return (
    <div data-testid="link-list">
      {/* Column header (desktop only) */}
      <div className="hidden grid-cols-[minmax(0,1.2fr)_minmax(0,1.6fr)_5rem_8rem_2.5rem] gap-3 border-b border-slate-100 px-6 py-2 text-xs font-semibold uppercase tracking-wide text-slate-400 lg:grid">
        <span>Short link</span>
        <span>Destination</span>
        <span className="text-right">Visits</span>
        <span className="text-right">Last visit</span>
        <span className="sr-only">Actions</span>
      </div>

      <ul className="divide-y divide-slate-100">
        {links.map((link) => (
          <li key={link.id}>
            <div
              role="link"
              tabIndex={0}
              onClick={() => void navigate(`/links/${link.id}`)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void navigate(`/links/${link.id}`);
              }}
              className="relative grid cursor-pointer gap-1.5 px-4 py-3.5 pr-14 transition hover:bg-slate-50 sm:px-6 sm:pr-14 lg:grid-cols-[minmax(0,1.2fr)_minmax(0,1.6fr)_5rem_8rem_2.5rem] lg:items-center lg:gap-3 lg:pr-6"
              data-testid={`link-row-${link.code}`}
            >
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate text-sm font-medium text-indigo-600">
                  {link.short_url.replace(/^https?:\/\//, "")}
                </span>
                {link.type === "PRIVATE" && (
                  <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-slate-500 ring-1 ring-inset ring-slate-200">
                    private
                  </span>
                )}
                {link.api_key_id != null && (
                  <span
                    className="rounded-full bg-sky-50 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-sky-600 ring-1 ring-inset ring-sky-200"
                    title="Created programmatically with an API key"
                  >
                    via API
                  </span>
                )}
                <CopyButton text={link.short_url} />
              </div>

              <p className="truncate text-sm text-slate-500" title={link.destination_url}>
                {link.destination_url}
              </p>

              <p className="text-sm text-slate-600 lg:text-right">
                <span className="lg:hidden text-slate-400">Visits: </span>
                {link.visits}
              </p>

              <p className="text-xs text-slate-400 lg:text-right">
                <span className="lg:hidden">Last visit: </span>
                {formatDateTime(link.last_visit_at)}
              </p>

              {/* Kebab: pinned top-right on mobile cards, last column on lg */}
              <div className="absolute right-3 top-3 lg:static lg:flex lg:justify-end">
                <KebabMenu
                  testid={`link-menu-${link.code}`}
                  items={linkMenuItems(
                    link,
                    (action) => openAction(link, action),
                    () => void navigate(`/links/${link.id}`),
                  )}
                />
              </div>
            </div>
          </li>
        ))}
      </ul>

      {overlay && (
        <LinkActionOverlays
          link={overlay.link}
          action={overlay.action}
          onClose={closeOverlay}
          onUpdated={handleUpdated}
        />
      )}
    </div>
  );
}
