// Org switcher dropdown: lists the user's memberships (GET /me), switches the
// active org via POST /api/v1/auth/switch-org, and offers "New organization".

import { useEffect, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { useAuth } from "../auth/AuthContext";
import { InitialAvatar, Spinner } from "./ui";

export default function OrgSwitcher() {
  const { orgs, activeOrgId, switchOrg } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();

  const [open, setOpen] = useState(false);
  const [busyOrgId, setBusyOrgId] = useState(0);
  const [error, setError] = useState("");
  const rootRef = useRef<HTMLDivElement>(null);

  const activeOrg = orgs.find((o) => o.org_id === activeOrgId);

  // Close on outside click / Escape.
  useEffect(() => {
    if (!open) return;

    const onPointerDown = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };

    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);

    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  const select = async (orgId: number) => {
    if (orgId === activeOrgId || busyOrgId !== 0) {
      setOpen(false);
      return;
    }

    setBusyOrgId(orgId);
    setError("");

    try {
      await switchOrg(orgId);
      setOpen(false);

      // A link-detail page belongs to the previous org (ids are org-scoped),
      // so it would just 404 after the switch — land on the links list instead.
      if (/^\/links\/\d+/.test(location.pathname)) navigate("/links");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Could not switch organization.");
    } finally {
      setBusyOrgId(0);
    }
  };

  return (
    <div ref={rootRef} className="relative" data-testid="org-switcher">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="listbox"
        aria-expanded={open}
        className="flex max-w-[220px] items-center gap-2 rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-sm font-medium text-slate-700 shadow-sm transition hover:bg-slate-50 sm:max-w-[280px]"
      >
        <InitialAvatar name={activeOrg?.name ?? "?"} className="h-6 w-6 text-xs" />
        <span className="truncate" data-testid="active-org-name">
          {activeOrg?.name ?? "Select organization"}
        </span>
        <svg className="h-4 w-4 shrink-0 text-slate-400" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path
            fillRule="evenodd"
            d="M5.23 7.21a.75.75 0 0 1 1.06.02L10 11.17l3.71-3.94a.75.75 0 1 1 1.08 1.04l-4.25 4.5a.75.75 0 0 1-1.08 0l-4.25-4.5a.75.75 0 0 1 .02-1.06z"
            clipRule="evenodd"
          />
        </svg>
      </button>

      {open && (
        <div
          role="listbox"
          className="absolute right-0 z-30 mt-2 w-64 overflow-hidden rounded-xl border border-slate-200 bg-white py-1 shadow-lg"
        >
          <p className="px-3 py-1.5 text-xs font-semibold uppercase tracking-wide text-slate-400">
            Organizations
          </p>

          {orgs.map((org) => (
            <button
              key={org.org_id}
              type="button"
              role="option"
              aria-selected={org.org_id === activeOrgId}
              onClick={() => void select(org.org_id)}
              className="flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm text-slate-700 transition hover:bg-slate-50"
            >
              <InitialAvatar name={org.name} className="h-6 w-6 text-xs" />
              <span className="min-w-0 flex-1">
                <span className="block truncate font-medium">{org.name}</span>
                <span className="block text-xs text-slate-400">{org.role.toLowerCase()}</span>
              </span>
              {busyOrgId === org.org_id ? (
                <Spinner className="h-4 w-4" />
              ) : (
                org.org_id === activeOrgId && (
                  <svg className="h-4 w-4 text-indigo-600" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
                    <path
                      fillRule="evenodd"
                      d="M16.7 5.3a1 1 0 0 1 0 1.4l-7.5 7.5a1 1 0 0 1-1.4 0l-3.5-3.5a1 1 0 1 1 1.4-1.4l2.8 2.79 6.8-6.8a1 1 0 0 1 1.4 0z"
                      clipRule="evenodd"
                    />
                  </svg>
                )
              )}
            </button>
          ))}

          {error && <p className="px-3 py-2 text-xs text-red-600">{error}</p>}

          <div className="my-1 border-t border-slate-100" />

          <button
            type="button"
            onClick={() => {
              setOpen(false);
              navigate("/onboarding?new=1");
            }}
            className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm font-medium text-indigo-600 transition hover:bg-indigo-50"
          >
            <span className="flex h-6 w-6 items-center justify-center rounded-full border border-dashed border-indigo-300 text-base leading-none">
              +
            </span>
            New organization
          </button>
        </div>
      )}
    </div>
  );
}
