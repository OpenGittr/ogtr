// Hand-rolled chart primitives (no chart library): a column chart for daily
// time series and a horizontal bar list for grouped totals. Single-series
// magnitude displays — one hue (indigo), labels in text tokens, recessive
// grid, hover tooltips on every mark.

import { useState } from "react";

import type { DayCount, DimCount } from "../lib/types";

/** "2026-07-14" -> "Jul 14" (UTC-safe: no Date parsing of bare dates). */
export function shortDate(iso: string): string {
  const [, m, d] = iso.split("-");
  const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
  const idx = Number(m) - 1;

  return `${months[idx] ?? m} ${Number(d)}`;
}

/**
 * Daily clicks as a column chart. Columns are div-based (responsive by
 * construction), rounded at the data end, 2px-gapped, with a per-column
 * hover tooltip. A single series needs no legend — the card title names it.
 */
export function ColumnChart({ data }: { data: DayCount[] }) {
  const [active, setActive] = useState<number | null>(null);

  if (data.length === 0) {
    return (
      <div className="flex h-32 items-center justify-center rounded-lg border border-dashed border-slate-200 text-sm text-slate-400">
        No clicks in this period
      </div>
    );
  }

  const max = Math.max(...data.map((d) => d.clicks), 1);
  const gridLines = [0.25, 0.5, 0.75];

  return (
    <div>
      <div className="relative h-32" role="img" aria-label="Clicks per day">
        {/* Recessive grid */}
        <div className="pointer-events-none absolute inset-0">
          {gridLines.map((g) => (
            <div
              key={g}
              className="absolute inset-x-0 border-t border-slate-100"
              style={{ bottom: `${g * 100}%` }}
            />
          ))}
          <div className="absolute inset-x-0 bottom-0 border-t border-slate-200" />
        </div>

        <div className="absolute inset-0 flex items-end gap-[2px]">
          {data.map((d, i) => (
            <div
              key={d.date}
              className="group relative flex h-full flex-1 items-end"
              onMouseEnter={() => setActive(i)}
              onMouseLeave={() => setActive(null)}
            >
              {/* Full-height hit target (bigger than the mark) */}
              <div className="absolute inset-0" aria-hidden="true" />
              <div
                data-testid="chart-column"
                className={`w-full rounded-t transition-colors ${
                  active === i ? "bg-indigo-700" : "bg-indigo-500"
                }`}
                style={{ height: `${Math.max((d.clicks / max) * 100, d.clicks > 0 ? 2 : 0)}%` }}
              />
              {active === i && (
                <div
                  className={`pointer-events-none absolute bottom-full z-10 mb-1 whitespace-nowrap rounded-md bg-slate-900 px-2 py-1 text-xs text-white shadow ${
                    i < data.length / 2 ? "left-0" : "right-0"
                  }`}
                  role="status"
                >
                  <span className="font-semibold">{d.clicks}</span>{" "}
                  {d.clicks === 1 ? "click" : "clicks"}
                  <span className="text-slate-300"> · {shortDate(d.date)}</span>
                </div>
              )}
            </div>
          ))}
        </div>

        {/* Y max label */}
        <span className="absolute right-0 top-0 -translate-y-1/2 rounded bg-white px-1 text-[10px] tabular-nums text-slate-400">
          {max}
        </span>
      </div>

      <div className="mt-1 flex justify-between text-[10px] text-slate-400">
        <span>{shortDate(data[0].date)}</span>
        {data.length > 2 && <span>{shortDate(data[Math.floor(data.length / 2)].date)}</span>}
        {data.length > 1 && <span>{shortDate(data[data.length - 1].date)}</span>}
      </div>
    </div>
  );
}

/**
 * Grouped totals as a horizontal bar list: fixed label column, a bar sized by
 * share of total, and a right-aligned count. Values are direct-labeled, so
 * color never carries meaning alone.
 */
export function HBarList({
  data,
  emptyLabel = "(none)",
}: {
  data: DimCount[];
  /** Display label for an empty dimension value (e.g. direct traffic). */
  emptyLabel?: string;
}) {
  if (data.length === 0) {
    return <p className="py-6 text-center text-sm text-slate-400">No data in this period</p>;
  }

  // Bars show share of total (not share of max) with a minimum visual weight,
  // so a 1-and-1 split reads as 50/50 instead of two full-width bars.
  const total = data.reduce((sum, d) => sum + d.clicks, 0) || 1;

  return (
    <ul className="divide-y divide-slate-100">
      {data.map((d) => (
        <li
          key={d.value || "empty"}
          className="flex items-center gap-3 py-2"
          title={`${d.value || emptyLabel}: ${d.clicks}`}
        >
          <span className="w-24 shrink-0 truncate text-sm text-slate-700 sm:w-36">
            {d.value || emptyLabel}
          </span>
          <div className="h-2.5 min-w-0 flex-1 overflow-hidden rounded-full bg-slate-100">
            <div
              className="h-full rounded-full bg-indigo-500"
              style={{ width: `${Math.max((d.clicks / total) * 100, 4)}%` }}
            />
          </div>
          <span className="w-10 shrink-0 text-right text-sm font-medium tabular-nums text-slate-900">
            {d.clicks}
          </span>
        </li>
      ))}
    </ul>
  );
}

/** A compact stat tile: label on top, hero number below. */
export function StatTile({
  label,
  value,
  hint,
  testid,
}: {
  label: string;
  value: string | number;
  hint?: string;
  testid?: string;
}) {
  return (
    <div className="h-full rounded-xl border border-slate-200 bg-white px-3.5 py-3 shadow-sm">
      <p className="truncate text-[11px] font-medium uppercase tracking-wide text-slate-400">
        {label}
      </p>
      <p className="mt-0.5 text-xl font-semibold tabular-nums text-slate-900" data-testid={testid}>
        {value}
      </p>
      {hint && <p className="mt-0.5 text-xs leading-4 text-slate-500">{hint}</p>}
    </div>
  );
}
