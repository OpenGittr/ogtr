// Per-link analytics (phase 5): date-ranged report (FEATURES.md §5.1) — total clicks,
// clicks-per-day chart, dimension breakdowns (totals + per-day), deep-link /
// target-rule stat tiles and the recent clicks list.

import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";

import { ApiError, LIMIT_REACHED, endpoints } from "../lib/api";
import { limitNoticeAction } from "../ext";
import type { DayCount, DayDimCount, DimCount, LinkStatsReport, ShortURL } from "../lib/types";
import { formatDateTime } from "./LinkList";
import { ColumnChart, HBarList, StatTile, shortDate } from "./charts";
import { ErrorBanner, NoticeBanner, Spinner } from "./ui";

const RECENT_CLICKS_SHOWN = 50;

/** Max range length that still zero-fills missing days in the chart. */
const MAX_FILLED_DAYS = 92;

type Dimension = "browser" | "device_type" | "referrer" | "mobile_os" | "location";

const DIMENSIONS: { key: Dimension; label: string; emptyLabel: string }[] = [
  { key: "browser", label: "Browser", emptyLabel: "(unknown)" },
  { key: "device_type", label: "Device", emptyLabel: "(unknown)" },
  { key: "referrer", label: "Referrer", emptyLabel: "Direct" },
  { key: "mobile_os", label: "Mobile OS", emptyLabel: "(unknown)" },
  { key: "location", label: "Location", emptyLabel: "Unknown" },
];

type LocationLevel = "countries" | "regions" | "cities";

const LOCATION_LEVELS: { key: LocationLevel; label: string }[] = [
  { key: "countries", label: "Country" },
  { key: "regions", label: "Region" },
  { key: "cities", label: "City" },
];

function isoDate(d: Date): string {
  return d.toISOString().slice(0, 10);
}

function defaultRange(): { from: string; to: string } {
  const to = new Date();
  const from = new Date();
  from.setMonth(from.getMonth() - 1);

  return { from: isoDate(from), to: isoDate(to) };
}

/** Fills days without clicks with 0 so the time axis is honest. */
function fillDays(series: DayCount[], from: string, to: string): DayCount[] {
  const start = new Date(`${from}T00:00:00Z`);
  const end = new Date(`${to}T00:00:00Z`);
  const span = Math.round((end.getTime() - start.getTime()) / 86_400_000) + 1;

  if (Number.isNaN(span) || span < 1 || span > MAX_FILLED_DAYS) return series;

  const byDate = new Map(series.map((d) => [d.date, d.clicks]));
  const filled: DayCount[] = [];

  for (let i = 0; i < span; i += 1) {
    const date = isoDate(new Date(start.getTime() + i * 86_400_000));
    filled.push({ date, clicks: byDate.get(date) ?? 0 });
  }

  return filled;
}

/** Per-day breakdown rows for one dimension, newest day first. */
function PerDayTable({ rows, emptyLabel }: { rows: DayDimCount[]; emptyLabel: string }) {
  if (rows.length === 0) {
    return <p className="py-6 text-center text-sm text-slate-400">No data in this period</p>;
  }

  const sorted = [...rows].sort((a, b) => (a.date === b.date ? b.clicks - a.clicks : b.date.localeCompare(a.date)));

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-slate-200 text-left text-xs font-medium uppercase tracking-wide text-slate-400">
            <th className="py-2 pr-4">Day</th>
            <th className="py-2 pr-4">Value</th>
            <th className="py-2 text-right">Clicks</th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((r) => (
            <tr key={`${r.date}-${r.value}`} className="border-b border-slate-100 last:border-0">
              <td className="py-1.5 pr-4 whitespace-nowrap text-slate-500">{shortDate(r.date)}</td>
              <td className="py-1.5 pr-4 break-all text-slate-700">{r.value || emptyLabel}</td>
              <td className="py-1.5 text-right tabular-nums font-medium text-slate-900">{r.clicks}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default function AnalyticsSection({ link }: { link: ShortURL }) {
  const [{ from, to }, setRange] = useState(defaultRange);
  const [draftFrom, setDraftFrom] = useState(from);
  const [draftTo, setDraftTo] = useState(to);

  const [report, setReport] = useState<LinkStatsReport | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  // LIMIT_REACHED (analytics viewing gated by the deployment's policy): the
  // server's message, shown verbatim as a notice in place of the charts.
  const [limitNotice, setLimitNotice] = useState("");

  const [dimension, setDimension] = useState<Dimension>("browser");
  const [locationLevel, setLocationLevel] = useState<LocationLevel>("countries");
  const [perDay, setPerDay] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    setLimitNotice("");

    try {
      setReport(await endpoints.linkStats(link.id, from, to));
    } catch (err: unknown) {
      setReport(null);

      if (err instanceof ApiError && err.code === LIMIT_REACHED) {
        setLimitNotice(err.message);
      } else {
        setError(
          err instanceof ApiError && err.status === 400
            ? "That date range is not valid — pick a start date on or before the end date."
            : "Could not load analytics. Please try again.",
        );
      }
    } finally {
      setLoading(false);
    }
  }, [link.id, from, to]);

  useEffect(() => {
    void load();
  }, [load]);

  const applyRange = (event: FormEvent) => {
    event.preventDefault();

    if (draftFrom && draftTo && draftFrom > draftTo) {
      setError("That date range is not valid — pick a start date on or before the end date.");
      return;
    }

    setError("");
    setRange({ from: draftFrom, to: draftTo });
  };

  const chartData = useMemo(
    () => (report ? fillDays(report.clicks_per_day, report.from, report.to) : []),
    [report],
  );

  const dim = DIMENSIONS.find((d) => d.key === dimension) ?? DIMENSIONS[0];
  const totalsRows: DimCount[] =
    (dimension === "location"
      ? report?.total_breakdowns.location[locationLevel]
      : report?.total_breakdowns[dimension]) ?? [];
  const perDayRows: DayDimCount[] =
    (dimension === "location"
      ? report?.per_day_breakdowns.location[locationLevel]
      : report?.per_day_breakdowns[dimension]) ?? [];
  const recentClicks = report?.clicks.slice(0, RECENT_CLICKS_SHOWN) ?? [];

  // GeoIP-off deployments record no geo at all: every location bucket at every
  // level is "Unknown". Only then does the breakdown swap to the setup hint.
  const locationAllUnknown = useMemo(() => {
    const loc = report?.total_breakdowns.location;
    if (!loc) return false;

    const buckets = [...loc.countries, ...loc.regions, ...loc.cities];

    return buckets.length > 0 && buckets.every((d) => d.value === "Unknown");
  }, [report]);

  return (
    <div className="space-y-4" data-testid="analytics-section">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-semibold text-slate-900">Analytics</h2>

        {/* Date-range filter: a single joined control attached to the header */}
        <form
          onSubmit={applyRange}
          className="flex items-stretch overflow-hidden rounded-lg border border-slate-300 bg-white shadow-sm"
        >
          <label className="sr-only" htmlFor="stats-from">
            From
          </label>
          <input
            id="stats-from"
            type="date"
            value={draftFrom}
            max={draftTo || undefined}
            onChange={(e) => setDraftFrom(e.target.value)}
            className="w-[8.25rem] border-0 bg-transparent px-2 py-1.5 text-sm text-slate-700 focus:outline-none"
            data-testid="stats-from"
          />
          <span className="flex items-center text-slate-300" aria-hidden="true">
            &ndash;
          </span>
          <label className="sr-only" htmlFor="stats-to">
            To
          </label>
          <input
            id="stats-to"
            type="date"
            value={draftTo}
            min={draftFrom || undefined}
            onChange={(e) => setDraftTo(e.target.value)}
            className="w-[8.25rem] border-0 bg-transparent px-2 py-1.5 text-sm text-slate-700 focus:outline-none"
            data-testid="stats-to"
          />
          <button
            type="submit"
            className="border-l border-slate-300 bg-slate-50 px-3 text-sm font-semibold text-indigo-600 transition hover:bg-indigo-50"
            data-testid="stats-apply"
          >
            Apply
          </button>
        </form>
      </div>

      {error && <ErrorBanner message={error} />}
      {limitNotice && <NoticeBanner message={limitNotice} action={limitNoticeAction} />}

      {loading ? (
        <div className="flex justify-center rounded-xl border border-slate-200 bg-white py-16 shadow-sm">
          <Spinner className="h-8 w-8" />
        </div>
      ) : (
        report && (
          <>
            {/* Stat tiles */}
            <div
              className={`grid grid-cols-2 gap-3 ${report.target_rule ? "sm:grid-cols-4" : "sm:grid-cols-3"}`}
            >
              <StatTile label="Total clicks" value={report.total_clicks} testid="stat-total-clicks" />
              <StatTile
                label="App opens"
                value={report.deeplink.mobile_app_opens}
                hint="Clicks served a mobile deep link"
                testid="stat-app-opens"
              />
              {report.target_rule && (
                <StatTile
                  label="Target rule matched"
                  value={`${report.target_rule.target_matched} / ${report.target_rule.total_clicks}`}
                  hint="Matched clicks vs. all clicks"
                  testid="stat-target-rule"
                />
              )}
              <StatTile
                label="Days with clicks"
                value={report.clicks_per_day.length}
                testid="stat-active-days"
              />
            </div>

            {/* Clicks per day */}
            <section className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
              <div className="mb-4 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                <h3 className="text-base font-semibold text-slate-900">Clicks per day</h3>
                {report.clicks_per_day.length > 0 &&
                  report.clicks_per_day.length <= 3 &&
                  chartData.length > 7 && (
                    <p className="text-xs text-slate-400">
                      Clicks on only {report.clicks_per_day.length} of {chartData.length} days in
                      this range
                    </p>
                  )}
              </div>
              <ColumnChart data={chartData} />
            </section>

            {/* Breakdowns */}
            <section className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
              <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-base font-semibold text-slate-900">Breakdown</h3>
                <button
                  type="button"
                  onClick={() => setPerDay((v) => !v)}
                  className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-600 transition hover:bg-slate-50"
                  data-testid="breakdown-toggle"
                >
                  {perDay ? "Show totals" : "Show per day"}
                </button>
              </div>

              <div className="mb-4 flex flex-wrap gap-1.5" role="tablist" aria-label="Breakdown dimension">
                {DIMENSIONS.map((d) => (
                  <button
                    key={d.key}
                    type="button"
                    role="tab"
                    aria-selected={dimension === d.key}
                    onClick={() => setDimension(d.key)}
                    className={`rounded-full px-3 py-1 text-xs font-medium ring-1 ring-inset transition ${
                      dimension === d.key
                        ? "bg-indigo-600 text-white ring-indigo-600"
                        : "bg-white text-slate-600 ring-slate-200 hover:bg-slate-50"
                    }`}
                    data-testid={`breakdown-tab-${d.key}`}
                  >
                    {d.label}
                  </button>
                ))}
              </div>

              {/* Location level toggle: same pill pattern as the dimension
                  tabs, one size down, so it reads as a secondary control */}
              {dimension === "location" && (
                <div
                  className="mb-4 flex flex-wrap gap-1"
                  role="tablist"
                  aria-label="Location level"
                >
                  {LOCATION_LEVELS.map((l) => (
                    <button
                      key={l.key}
                      type="button"
                      role="tab"
                      aria-selected={locationLevel === l.key}
                      onClick={() => setLocationLevel(l.key)}
                      className={`rounded-full px-2.5 py-0.5 text-[11px] font-medium ring-1 ring-inset transition ${
                        locationLevel === l.key
                          ? "bg-indigo-50 text-indigo-700 ring-indigo-200"
                          : "bg-white text-slate-500 ring-slate-200 hover:bg-slate-50"
                      }`}
                      data-testid={`location-level-${l.key}`}
                    >
                      {l.label}
                    </button>
                  ))}
                </div>
              )}

              <div data-testid="breakdown-body">
                {dimension === "location" && locationAllUnknown ? (
                  <p className="py-6 text-center text-sm text-slate-400">
                    No location data &mdash; GeoIP not configured?
                  </p>
                ) : perDay ? (
                  <PerDayTable rows={perDayRows} emptyLabel={dim.emptyLabel} />
                ) : (
                  <HBarList data={totalsRows} emptyLabel={dim.emptyLabel} />
                )}
              </div>

              {dimension === "mobile_os" && (
                <p className="mt-3 text-xs text-slate-400">Desktop clicks are not included.</p>
              )}
            </section>

            {/* Recent clicks */}
            <section className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
              <h3 className="mb-4 text-base font-semibold text-slate-900">
                Recent clicks
                {report.clicks.length > RECENT_CLICKS_SHOWN && (
                  <span className="ml-2 text-xs font-normal text-slate-400">
                    latest {RECENT_CLICKS_SHOWN} of {report.clicks.length}
                  </span>
                )}
              </h3>

              {recentClicks.length === 0 ? (
                <p className="py-4 text-center text-sm text-slate-400">No clicks in this period</p>
              ) : (
                <div className="overflow-x-auto">
                  <table className="w-full text-sm" data-testid="recent-clicks-table">
                    <thead>
                      <tr className="border-b border-slate-200 text-left text-xs font-medium uppercase tracking-wide text-slate-400">
                        <th className="py-2 pr-4">Time</th>
                        <th className="py-2 text-right">Campaign tag</th>
                      </tr>
                    </thead>
                    <tbody>
                      {recentClicks.map((c) => (
                        <tr key={c.id} className="border-b border-slate-100 last:border-0">
                          <td className="py-1.5 pr-4 whitespace-nowrap text-slate-700">
                            {formatDateTime(c.ts)}
                          </td>
                          <td className="py-1.5 text-right">
                            {c.custom_tag_id ? (
                              <span className="rounded-full bg-indigo-50 px-2 py-0.5 text-xs font-medium text-indigo-700 ring-1 ring-inset ring-indigo-200">
                                {c.custom_tag_id}
                              </span>
                            ) : (
                              <span className="text-slate-400">—</span>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
          </>
        )
      )}
    </div>
  );
}
