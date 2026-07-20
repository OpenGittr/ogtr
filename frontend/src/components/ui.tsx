// Small shared UI primitives: spinner, loaders, error banner (with optional
// retry), empty state, role badge, avatar, copy-to-clipboard button, and the
// hand-rolled overlay components (modal, drawer, kebab menu) used by the
// link-management flows.

import { useEffect, useRef, useState, type ReactNode } from "react";

import type { Role } from "../lib/types";

/**
 * Shared primary (indigo) button styles. The disabled state is deliberately
 * unmistakable — grey fill, muted text, no shadow — so a pale button is never
 * confused with an actionable one.
 */
export const primaryButtonClass =
  "inline-flex items-center justify-center gap-2 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-semibold " +
  "text-white shadow-sm transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:bg-slate-100 " +
  "disabled:text-slate-400 disabled:shadow-none disabled:ring-1 disabled:ring-inset disabled:ring-slate-200";

/** Shared secondary (outline) button styles with a matching disabled state. */
export const secondaryButtonClass =
  "inline-flex items-center justify-center gap-2 rounded-lg border border-slate-300 bg-white px-4 py-2 " +
  "text-sm font-medium text-slate-700 shadow-sm transition hover:bg-slate-50 " +
  "disabled:cursor-not-allowed disabled:border-slate-200 disabled:text-slate-300 disabled:shadow-none";

export function Spinner({ className = "h-5 w-5" }: { className?: string }) {
  return (
    <svg
      className={`animate-spin text-slate-400 ${className}`}
      viewBox="0 0 24 24"
      fill="none"
      aria-label="Loading"
      role="status"
    >
      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
      <path
        className="opacity-75"
        fill="currentColor"
        d="M4 12a8 8 0 0 1 8-8v4a4 4 0 0 0-4 4H4z"
      />
    </svg>
  );
}

export function FullPageSpinner() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-50">
      <Spinner className="h-8 w-8" />
    </div>
  );
}

/** Centered page/section-level loading spinner (one consistent size). */
export function PageLoader() {
  return (
    <div className="flex justify-center py-16">
      <Spinner className="h-8 w-8" />
    </div>
  );
}

export function ErrorBanner({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div
      className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700"
      role="alert"
    >
      <span>{message}</span>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="rounded-md bg-white px-3 py-1 text-xs font-semibold text-red-700 ring-1 ring-inset ring-red-200 transition hover:bg-red-100"
        >
          Retry
        </button>
      )}
    </div>
  );
}

/**
 * Informational notice, visually distinct from ErrorBanner — used when the
 * server declines an action with an explanatory message meant to be shown
 * verbatim (e.g. a LIMIT_REACHED response from the deployment's policy).
 */
export function NoticeBanner({ message }: { message: string }) {
  return (
    <div
      className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800"
      role="status"
    >
      {message}
    </div>
  );
}

/**
 * Consistent empty state: icon + one line + optional action. Used wherever a
 * list or report has nothing to show yet.
 */
export function EmptyState({
  icon,
  title,
  hint,
  action,
  testid,
}: {
  icon: ReactNode;
  title: string;
  hint?: string;
  action?: ReactNode;
  testid?: string;
}) {
  return (
    <div
      className="flex flex-col items-center px-6 py-12 text-center"
      data-testid={testid}
    >
      <span className="flex h-11 w-11 items-center justify-center rounded-full bg-slate-100 text-slate-400">
        {icon}
      </span>
      <p className="mt-3 text-sm font-medium text-slate-700">{title}</p>
      {hint && <p className="mt-1 max-w-md text-sm text-slate-500">{hint}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

/** Stock icons for EmptyState (24x24 stroke outlines). */
export function EmptyIcon({ path }: { path: string }) {
  return (
    <svg
      className="h-5 w-5"
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

export const emptyIconPaths = {
  link: "M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71",
  chart: "M3 3v18h18M8 17V9m5 8V5m5 12v-6",
  key: "M21 2l-2 2m-7.61 7.61A5.5 5.5 0 1 1 7.5 7.5c.5 0 1 .07 1.46.2L15 2h3v3l-2 2 2 2-3 3-2-2-1.61 1.61z",
} as const;

export function RoleBadge({ role }: { role: Role | string }) {
  const styles =
    role === "OWNER"
      ? "bg-indigo-50 text-indigo-700 ring-indigo-200"
      : "bg-slate-100 text-slate-600 ring-slate-200";

  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset ${styles}`}
    >
      {role.toLowerCase()}
    </span>
  );
}

/** Copies text to the clipboard, falling back when the API is unavailable. */
export async function copyToClipboard(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    // Clipboard API unavailable (http origin, permissions): legacy fallback.
    const el = document.createElement("textarea");
    el.value = text;
    document.body.appendChild(el);
    el.select();
    document.execCommand("copy");
    el.remove();
  }
}

/** Copies text to the clipboard with a transient "Copied" confirmation. */
export function CopyButton({ text, label = "Copy" }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const copy = async (event: React.MouseEvent) => {
    event.stopPropagation(); // rows using CopyButton also navigate on click

    await copyToClipboard(text);

    setCopied(true);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setCopied(false), 1500);
  };

  return (
    <button
      type="button"
      onClick={(e) => void copy(e)}
      className={`inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium ring-1 ring-inset transition ${
        copied
          ? "bg-emerald-50 text-emerald-700 ring-emerald-200"
          : "bg-white text-slate-600 ring-slate-200 hover:bg-slate-50"
      }`}
      aria-label={`Copy ${text}`}
    >
      {copied ? (
        <>
          <svg className="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
            <path
              fillRule="evenodd"
              d="M16.704 4.153a.75.75 0 0 1 .143 1.052l-8 10.5a.75.75 0 0 1-1.127.075l-4.5-4.5a.75.75 0 0 1 1.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 0 1 1.05-.143z"
              clipRule="evenodd"
            />
          </svg>
          Copied
        </>
      ) : (
        <>
          <svg className="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
            <path d="M7 3.5A1.5 1.5 0 0 1 8.5 2h3.879a1.5 1.5 0 0 1 1.06.44l3.122 3.12A1.5 1.5 0 0 1 17 6.622V12.5a1.5 1.5 0 0 1-1.5 1.5h-1v-3.379a3 3 0 0 0-.879-2.121L10.5 5.379A3 3 0 0 0 8.379 4.5H7v-1z" />
            <path d="M4.5 6A1.5 1.5 0 0 0 3 7.5v9A1.5 1.5 0 0 0 4.5 18h7a1.5 1.5 0 0 0 1.5-1.5v-5.879a1.5 1.5 0 0 0-.44-1.06L9.44 6.439A1.5 1.5 0 0 0 8.378 6H4.5z" />
          </svg>
          {label}
        </>
      )}
    </button>
  );
}

/** Colored circle with the entity's first letter. */
export function InitialAvatar({
  name,
  className = "h-9 w-9 text-sm",
}: {
  name: string;
  className?: string;
}) {
  const initial = (name.trim()[0] ?? "?").toUpperCase();

  return (
    <span
      className={`inline-flex shrink-0 items-center justify-center rounded-full bg-indigo-600 font-semibold text-white ${className}`}
      aria-hidden="true"
    >
      {initial}
    </span>
  );
}

// ---------------------------------------------------------------------------
// Overlays: modal + drawer (hand-rolled, no deps). Both close on Esc and
// backdrop click, lock body scroll, and return focus to the previously
// focused element on close. Render them conditionally — mounting = open.
// ---------------------------------------------------------------------------

/**
 * Shared overlay behavior: focus the panel on mount, close on Escape, lock
 * body scroll, restore focus on unmount.
 */
function useOverlay(onClose: () => void) {
  const panelRef = useRef<HTMLDivElement | null>(null);
  const closeRef = useRef(onClose);

  closeRef.current = onClose;

  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;

    panelRef.current?.focus();

    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") closeRef.current();
    };

    document.addEventListener("keydown", onKey);

    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";

    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
      previous?.focus?.();
    };
  }, []);

  return panelRef;
}

function OverlayCloseButton({ onClose }: { onClose: () => void }) {
  return (
    <button
      type="button"
      onClick={onClose}
      aria-label="Close"
      data-testid="overlay-close"
      className="shrink-0 rounded-md p-1.5 text-slate-400 transition hover:bg-slate-100 hover:text-slate-600"
    >
      <svg className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
        <path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22z" />
      </svg>
    </button>
  );
}

/** Centered dialog. Fits 390 px viewports; body scrolls when content is tall. */
export function Modal({
  title,
  onClose,
  children,
  testid,
  maxWidth = "max-w-md",
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  testid?: string;
  maxWidth?: string;
}) {
  const panelRef = useOverlay(onClose);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4" role="presentation">
      <div
        className="absolute inset-0 bg-slate-900/50"
        onClick={onClose}
        data-testid="overlay-backdrop"
        aria-hidden="true"
      />
      <div
        ref={panelRef}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        data-testid={testid}
        className={`relative z-10 flex max-h-[calc(100dvh-2rem)] w-full ${maxWidth} flex-col overflow-hidden rounded-2xl bg-white shadow-xl outline-none`}
      >
        <div className="flex items-center justify-between gap-2 border-b border-slate-100 px-4 py-3 sm:px-6">
          <h2 className="text-base font-semibold text-slate-900">{title}</h2>
          <OverlayCloseButton onClose={onClose} />
        </div>
        <div className="overflow-y-auto px-4 py-4 sm:px-6 sm:py-5">{children}</div>
      </div>
    </div>
  );
}

/**
 * Right slide-over panel for the complex editors; becomes a full-screen sheet
 * below 640 px.
 */
export function Drawer({
  title,
  onClose,
  children,
  testid,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  testid?: string;
}) {
  const panelRef = useOverlay(onClose);

  return (
    <div className="fixed inset-0 z-50 flex justify-end" role="presentation">
      <div
        className="absolute inset-0 bg-slate-900/50"
        onClick={onClose}
        data-testid="overlay-backdrop"
        aria-hidden="true"
      />
      <div
        ref={panelRef}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        data-testid={testid}
        className="relative z-10 flex h-full w-full flex-col bg-slate-50 shadow-xl outline-none sm:max-w-xl sm:border-l sm:border-slate-200"
      >
        <div className="flex items-center justify-between gap-2 border-b border-slate-200 bg-white px-4 py-3 sm:px-6">
          <h2 className="text-base font-semibold text-slate-900">{title}</h2>
          <OverlayCloseButton onClose={onClose} />
        </div>
        <div className="flex-1 overflow-y-auto px-4 py-4 sm:px-6 sm:py-5">{children}</div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Kebab (⋯) menu: accessible per-row actions. Opens on click/Enter, arrow
// keys move between items, Esc closes and refocuses the trigger, outside
// click closes.
// ---------------------------------------------------------------------------

export interface MenuItem {
  label: string;
  onSelect: () => void;
  testid?: string;
}

export function KebabMenu({
  items,
  label = "Link actions",
  testid,
}: {
  items: MenuItem[];
  label?: string;
  testid?: string;
}) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const buttonRef = useRef<HTMLButtonElement | null>(null);
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);

  useEffect(() => {
    if (!open) return undefined;

    const onDown = (event: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    };

    document.addEventListener("mousedown", onDown);
    itemRefs.current[0]?.focus();

    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  const onMenuKeyDown = (event: React.KeyboardEvent) => {
    const idx = itemRefs.current.findIndex((el) => el === document.activeElement);

    if (event.key === "Escape") {
      setOpen(false);
      buttonRef.current?.focus();
    } else if (event.key === "ArrowDown") {
      event.preventDefault();
      itemRefs.current[(idx + 1) % items.length]?.focus();
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      itemRefs.current[(idx - 1 + items.length) % items.length]?.focus();
    } else if (event.key === "Tab") {
      setOpen(false);
    }
  };

  return (
    // Rows hosting the menu navigate on click/Enter; keep both inside.
    <div
      ref={rootRef}
      className="relative"
      onClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => e.stopPropagation()}
    >
      <button
        ref={buttonRef}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={label}
        data-testid={testid}
        onClick={() => setOpen((v) => !v)}
        className="rounded-md p-1.5 text-slate-400 ring-1 ring-inset ring-slate-200 transition hover:bg-slate-50 hover:text-slate-600"
      >
        <svg className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path d="M10 3a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3zm0 5.5a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3zm0 5.5a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3z" />
        </svg>
      </button>

      {open && (
        <div
          role="menu"
          aria-label={label}
          onKeyDown={onMenuKeyDown}
          className="absolute right-0 z-20 mt-1 w-48 overflow-hidden rounded-lg border border-slate-200 bg-white py-1 shadow-lg"
        >
          {items.map((item, i) => (
            <button
              key={item.label}
              ref={(el) => {
                itemRefs.current[i] = el;
              }}
              role="menuitem"
              type="button"
              data-testid={item.testid}
              onClick={() => {
                setOpen(false);
                // Refocus the trigger before acting: an overlay opened by the
                // action then records it as "previous focus" and restores it
                // on close (the menu items themselves unmount).
                buttonRef.current?.focus();
                item.onSelect();
              }}
              className="block w-full px-3 py-1.5 text-left text-sm text-slate-700 transition hover:bg-indigo-50 focus:bg-indigo-50 focus:outline-none"
            >
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
