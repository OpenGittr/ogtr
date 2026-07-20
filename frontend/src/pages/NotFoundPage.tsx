// Friendly 404 for unknown SPA paths (works for both signed-in and anonymous
// visitors — it renders standalone, outside the dashboard shell).

import { Link } from "react-router-dom";

import { usePageTitle } from "../lib/usePageTitle";

export default function NotFoundPage() {
  usePageTitle("Page not found");

  return (
    <main className="flex min-h-screen flex-col items-center justify-center bg-slate-50 px-4 text-center">
      <p className="text-sm font-semibold uppercase tracking-wide text-indigo-600">404</p>
      <h1 className="mt-2 text-2xl font-semibold text-slate-900 sm:text-3xl">
        This page does not exist
      </h1>
      <p className="mt-2 max-w-md text-sm text-slate-500">
        The address may be mistyped, or the page may have moved. Short links themselves are
        served by the API host, not this dashboard.
      </p>
      <Link
        to="/"
        className="mt-6 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-semibold text-white shadow-sm transition hover:bg-indigo-500"
      >
        Go to dashboard
      </Link>
    </main>
  );
}
