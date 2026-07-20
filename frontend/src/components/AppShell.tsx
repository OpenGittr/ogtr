// Dashboard shell: dark sidebar on desktop, slide-over menu on mobile, top
// bar with the org switcher. Child pages render into <Outlet/>.

import { useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router-dom";

import { useAuth } from "../auth/AuthContext";
import { extraNavItems } from "../ext";
import Logo from "./Logo";
import OrgSwitcher from "./OrgSwitcher";
import { InitialAvatar } from "./ui";

interface NavItem {
  to: string;
  label: string;
  soon?: boolean;
}

const NAV_ITEMS: NavItem[] = [
  { to: "/", label: "Overview" },
  { to: "/links", label: "Links" },
  { to: "/analytics", label: "Analytics" },
  { to: "/members", label: "Members" },
  { to: "/domains", label: "Domains" },
  { to: "/api-keys", label: "API keys" },
  // Deployment-registered items (src/ext) render after the built-ins.
  ...extraNavItems.map(({ to, label }) => ({ to, label })),
];

function NavList({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <nav className="flex-1 space-y-1 px-3" aria-label="Main">
      {NAV_ITEMS.map((item) => (
        <NavLink
          key={item.to}
          to={item.to}
          end={item.to === "/"}
          onClick={onNavigate}
          className={({ isActive }) =>
            `flex items-center justify-between rounded-lg px-3 py-2 text-sm font-medium transition ${
              isActive
                ? "bg-slate-800 text-white"
                : "text-slate-400 hover:bg-slate-800/60 hover:text-slate-200"
            }`
          }
        >
          <span>{item.label}</span>
          {item.soon && (
            <span className="rounded-full bg-slate-800 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-slate-500 ring-1 ring-inset ring-slate-700">
              soon
            </span>
          )}
        </NavLink>
      ))}
    </nav>
  );
}

function SidebarFooter() {
  const { user, logout } = useAuth();

  return (
    <div className="border-t border-slate-800 p-3">
      <div className="flex items-center gap-2.5 px-1">
        <InitialAvatar name={user?.name ?? "?"} className="h-8 w-8 text-xs" />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-slate-200">{user?.name}</p>
          <p className="truncate text-xs text-slate-500">{user?.email}</p>
        </div>
      </div>
      <button
        type="button"
        onClick={logout}
        className="mt-3 w-full rounded-lg border border-slate-700 px-3 py-1.5 text-sm font-medium text-slate-300 transition hover:bg-slate-800"
      >
        Sign out
      </button>
    </div>
  );
}

export default function AppShell() {
  const [menuOpen, setMenuOpen] = useState(false);
  const location = useLocation();
  const { activeOrgId } = useAuth();

  const currentLabel =
    NAV_ITEMS.find((item) =>
      item.to === "/" ? location.pathname === "/" : location.pathname.startsWith(item.to),
    )?.label ?? "";

  return (
    <div className="min-h-screen bg-slate-50">
      {/* Desktop sidebar */}
      <aside className="fixed inset-y-0 left-0 z-20 hidden w-60 flex-col bg-slate-900 lg:flex">
        <div className="flex h-16 items-center px-5">
          <Logo withWordmark className="text-white" />
        </div>
        <NavList />
        <SidebarFooter />
      </aside>

      {/* Mobile slide-over */}
      {menuOpen && (
        <div className="fixed inset-0 z-40 lg:hidden" role="dialog" aria-modal="true">
          <div
            className="absolute inset-0 bg-slate-950/60"
            onClick={() => setMenuOpen(false)}
            aria-hidden="true"
          />
          <div className="absolute inset-y-0 left-0 flex w-64 flex-col bg-slate-900 shadow-xl">
            <div className="flex h-16 items-center justify-between px-5">
              <Logo withWordmark className="text-white" />
              <button
                type="button"
                onClick={() => setMenuOpen(false)}
                aria-label="Close menu"
                className="rounded-md p-1.5 text-slate-400 hover:bg-slate-800 hover:text-white"
              >
                <svg className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
                  <path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22z" />
                </svg>
              </button>
            </div>
            <NavList onNavigate={() => setMenuOpen(false)} />
            <SidebarFooter />
          </div>
        </div>
      )}

      {/* Main column */}
      <div className="lg:pl-60">
        <header className="sticky top-0 z-10 flex h-16 items-center gap-3 border-b border-slate-200 bg-white/90 px-4 backdrop-blur sm:px-6">
          <button
            type="button"
            onClick={() => setMenuOpen(true)}
            aria-label="Open menu"
            className="rounded-md p-2 text-slate-500 hover:bg-slate-100 lg:hidden"
          >
            <svg className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path
                fillRule="evenodd"
                d="M2 4.75A.75.75 0 0 1 2.75 4h14.5a.75.75 0 0 1 0 1.5H2.75A.75.75 0 0 1 2 4.75zm0 5.25A.75.75 0 0 1 2.75 9.25h14.5a.75.75 0 0 1 0 1.5H2.75A.75.75 0 0 1 2 10zm0 5.25a.75.75 0 0 1 .75-.75h14.5a.75.75 0 0 1 0 1.5H2.75a.75.75 0 0 1-.75-.75z"
                clipRule="evenodd"
              />
            </svg>
          </button>

          <h1 className="flex-1 truncate text-base font-semibold text-slate-900">
            {currentLabel}
          </h1>

          <OrgSwitcher />
        </header>

        {/* Keyed by org: switching orgs remounts the page so every list and
            report refetches under the new tenant instead of showing stale data. */}
        <main key={activeOrgId} className="mx-auto max-w-5xl px-4 py-6 sm:px-6 sm:py-8">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
