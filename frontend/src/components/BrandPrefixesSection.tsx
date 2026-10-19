// Brand prefixes: short links under <short-domain>/<prefix>/<code>, for
// senders (Indian DLT SMS) who must whitelist a URL prefix they exclusively
// control. The OWNER requests a prefix equal to their DLT header with the
// entity details; the operator reviews it, and once APPROVED the org's links
// resolve under it and can display under it (Set primary). Members see a
// read-only list.

import { useCallback, useEffect, useState, type FormEvent } from "react";

import { ApiError, endpoints, shortHost } from "../lib/api";
import type { BrandPrefix } from "../lib/types";
import { EmptyIcon, EmptyState, ErrorBanner, Spinner } from "./ui";

function friendlyError(err: unknown, fallback: string): string {
  if (err instanceof ApiError) {
    if (err.status === 403) return "Only an organization owner can manage brand prefixes.";
    if (err.message) return err.message[0].toUpperCase() + err.message.slice(1);
  }

  return fallback;
}

function StatusBadge({ status }: { status: BrandPrefix["status"] }) {
  const styles =
    status === "APPROVED"
      ? "bg-emerald-50 dark:bg-emerald-500/10 text-emerald-700 dark:text-emerald-400 ring-emerald-200 dark:ring-emerald-500/30"
      : status === "PENDING"
        ? "bg-amber-50 dark:bg-amber-500/10 text-amber-700 dark:text-amber-300 ring-amber-200 dark:ring-amber-500/30"
        : "bg-red-50 dark:bg-red-500/10 text-red-700 dark:text-red-400 ring-red-200 dark:ring-red-500/30";

  const label = status === "PENDING" ? "in review" : status.toLowerCase();

  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset ${styles}`}
    >
      {label}
    </span>
  );
}

const inputClass =
  "w-full rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-900 px-3 py-2 text-sm text-slate-900 dark:text-white shadow-sm placeholder:text-slate-400 dark:placeholder:text-slate-500 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200 dark:focus:ring-indigo-500/30";

export default function BrandPrefixesSection({ isOwner }: { isOwner: boolean }) {
  const host = shortHost();

  const [prefixes, setPrefixes] = useState<BrandPrefix[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");

  const load = useCallback(async () => {
    setLoadError("");

    try {
      setPrefixes(await endpoints.prefixes());
    } catch (err: unknown) {
      setLoadError(friendlyError(err, "Could not load brand prefixes. Please try again."));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // --- request ---
  const [header, setHeader] = useState("");
  const [entityId, setEntityId] = useState("");
  const [entityName, setEntityName] = useState("");
  const [addBusy, setAddBusy] = useState(false);
  const [addError, setAddError] = useState("");
  const [addNotice, setAddNotice] = useState("");

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (addBusy) return;

    const trimmed = header.trim();

    setAddBusy(true);
    setAddError("");
    setAddNotice("");

    try {
      // The prefix IS the DLT header — one field, sent as both.
      const created = await endpoints.createPrefix({
        prefix: trimmed,
        header: trimmed,
        entity_id: entityId.trim(),
        entity_name: entityName.trim(),
      });

      setPrefixes((prev) => [...prev, created]);
      setHeader("");
      setEntityId("");
      setEntityName("");
      setAddNotice(
        created.status === "APPROVED"
          ? `${created.prefix} is approved — your links now also work at ${host}/${created.prefix}/…`
          : `${created.prefix} submitted for review. We may email you for a screenshot of the header on your DLT portal.`,
      );
    } catch (err: unknown) {
      setAddError(friendlyError(err, "Could not submit the prefix."));
    } finally {
      setAddBusy(false);
    }
  };

  // --- set primary ---
  const [primaryBusyId, setPrimaryBusyId] = useState(0);
  const [actionError, setActionError] = useState("");

  const setPrimary = async (id: number) => {
    setPrimaryBusyId(id);
    setActionError("");

    try {
      const primary = await endpoints.setPrimaryPrefix(id);

      setPrefixes((prev) => prev.map((p) => (p.id === id ? primary : { ...p, is_primary: false })));
    } catch (err: unknown) {
      setActionError(friendlyError(err, "Could not set the primary prefix."));
    } finally {
      setPrimaryBusyId(0);
    }
  };

  // --- remove (two-click inline confirm) ---
  const [confirmDeleteId, setConfirmDeleteId] = useState(0);
  const [deleteBusyId, setDeleteBusyId] = useState(0);

  const remove = async (id: number) => {
    setDeleteBusyId(id);
    setActionError("");

    try {
      await endpoints.deletePrefix(id);

      setPrefixes((prev) => prev.filter((p) => p.id !== id));
    } catch (err: unknown) {
      setActionError(friendlyError(err, "Could not remove the prefix."));
    } finally {
      setDeleteBusyId(0);
      setConfirmDeleteId(0);
    }
  };

  const example = `${host}/${header.trim() || "HEADER"}/abc1234`;

  return (
    <section
      className="rounded-xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-sm"
      data-testid="brand-prefixes"
    >
      <div className="border-b border-slate-100 dark:border-slate-800 px-4 py-4 sm:px-6">
        <h2 className="text-base font-semibold text-slate-900 dark:text-white">Brand prefixes</h2>
        <p className="mt-0.5 text-sm text-slate-500 dark:text-slate-400">
          No domain of your own? Get short links like{" "}
          <code className="font-mono text-xs">{host}/ACMEIN/abc1234</code> — a path only your
          organization can use, so you can whitelist it for SMS on India’s DLT platform. The
          prefix must be your registered DLT sender header; we review each request.
        </p>
      </div>

      <div className="space-y-4 px-4 py-4 sm:px-6">
        {isOwner ? (
          <form onSubmit={(e) => void submit(e)} className="space-y-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <div>
                <label htmlFor="prefix-header" className="block text-xs font-medium text-slate-600 dark:text-slate-300">
                  DLT sender header
                </label>
                <input
                  id="prefix-header"
                  type="text"
                  value={header}
                  onChange={(e) => setHeader(e.target.value.replace(/[^A-Za-z0-9]/g, ""))}
                  placeholder="ACMEIN"
                  minLength={3}
                  maxLength={20}
                  required
                  autoCapitalize="characters"
                  className={`mt-1 font-mono ${inputClass}`}
                />
              </div>
              <div>
                <label htmlFor="prefix-entity-id" className="block text-xs font-medium text-slate-600 dark:text-slate-300">
                  Principal Entity ID
                </label>
                <input
                  id="prefix-entity-id"
                  type="text"
                  inputMode="numeric"
                  value={entityId}
                  onChange={(e) => setEntityId(e.target.value.replace(/\D/g, ""))}
                  placeholder="1101234567890123456"
                  maxLength={32}
                  required
                  className={`mt-1 font-mono ${inputClass}`}
                />
              </div>
              <div className="sm:col-span-2">
                <label htmlFor="prefix-entity-name" className="block text-xs font-medium text-slate-600 dark:text-slate-300">
                  Registered entity name
                </label>
                <input
                  id="prefix-entity-name"
                  type="text"
                  value={entityName}
                  onChange={(e) => setEntityName(e.target.value)}
                  placeholder="Acme India Private Limited"
                  minLength={2}
                  maxLength={255}
                  required
                  className={`mt-1 ${inputClass}`}
                />
              </div>
            </div>

            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <p className="min-w-0 truncate text-xs text-slate-500 dark:text-slate-400">
                Your links will look like <code className="font-mono">{example}</code>
              </p>
              <button
                type="submit"
                disabled={addBusy || header.trim().length < 3 || entityId === "" || entityName.trim().length < 2}
                className="flex shrink-0 items-center justify-center gap-2 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-semibold text-white shadow-sm transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {addBusy && <Spinner className="h-4 w-4 text-white" />}
                {addBusy ? "Submitting…" : "Request prefix"}
              </button>
            </div>
          </form>
        ) : (
          <p className="rounded-lg bg-slate-50 dark:bg-slate-900 px-4 py-3 text-sm text-slate-500 dark:text-slate-400">
            Only organization owners can request or remove brand prefixes.
          </p>
        )}

        {addError && <ErrorBanner message={addError} />}
        {addNotice && (
          <p
            className="rounded-lg border border-emerald-200 dark:border-emerald-500/30 bg-emerald-50 dark:bg-emerald-500/10 px-4 py-3 text-sm text-emerald-700 dark:text-emerald-400"
            role="status"
          >
            {addNotice}
          </p>
        )}
        {actionError && <ErrorBanner message={actionError} />}
      </div>

      {loading ? (
        <div className="flex justify-center px-4 py-6">
          <Spinner />
        </div>
      ) : loadError ? (
        <div className="px-4 pb-4 sm:px-6">
          <ErrorBanner
            message={loadError}
            onRetry={() => {
              setLoading(true);
              void load();
            }}
          />
        </div>
      ) : prefixes.length === 0 ? (
        <EmptyState
          icon={<EmptyIcon path="M4 7h16M4 12h10M4 17h6" />}
          title="No brand prefixes yet"
          hint={isOwner ? "Request one above with your DLT sender header." : "An organization owner can request one."}
          testid="prefixes-empty"
        />
      ) : (
        <ul className="divide-y divide-slate-100 border-t border-slate-100 dark:divide-slate-800 dark:border-slate-800" data-testid="prefixes-list">
          {prefixes.map((p) => (
            <li key={p.id} className="px-4 py-4 sm:px-6" data-testid={`prefix-row-${p.id}`}>
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
                <div className="min-w-0 flex-1 basis-48">
                  <p className="truncate font-mono text-sm font-medium text-slate-900 dark:text-white">
                    {host}/{p.prefix}/…
                  </p>
                  <p className="truncate text-xs text-slate-400 dark:text-slate-500">
                    {p.entity_name} · PE {p.entity_id}
                  </p>
                </div>

                {p.is_primary && (
                  <span className="inline-flex items-center rounded-full bg-indigo-50 dark:bg-indigo-500/10 px-2.5 py-0.5 text-xs font-medium text-indigo-700 dark:text-indigo-300 ring-1 ring-inset ring-indigo-200 dark:ring-indigo-500/30">
                    primary
                  </span>
                )}
                <StatusBadge status={p.status} />

                {isOwner && p.status === "APPROVED" && !p.is_primary && (
                  <button
                    type="button"
                    onClick={() => void setPrimary(p.id)}
                    disabled={primaryBusyId !== 0}
                    className="rounded-md px-2.5 py-1 text-xs font-medium text-indigo-600 dark:text-indigo-400 ring-1 ring-inset ring-indigo-200 dark:ring-indigo-500/30 transition hover:bg-indigo-50 dark:hover:bg-indigo-500/10 disabled:opacity-50"
                    data-testid={`set-primary-prefix-${p.id}`}
                  >
                    {primaryBusyId === p.id ? "Setting…" : "Set primary"}
                  </button>
                )}

                {isOwner &&
                  p.status !== "REVOKED" &&
                  (confirmDeleteId === p.id ? (
                    <span className="flex items-center gap-2">
                      <button
                        type="button"
                        onClick={() => void remove(p.id)}
                        disabled={deleteBusyId !== 0}
                        className="rounded-md bg-red-600 px-2.5 py-1 text-xs font-semibold text-white hover:bg-red-500 disabled:opacity-50"
                      >
                        {deleteBusyId === p.id ? "Removing…" : "Confirm"}
                      </button>
                      <button
                        type="button"
                        onClick={() => setConfirmDeleteId(0)}
                        disabled={deleteBusyId !== 0}
                        className="rounded-md px-2 py-1 text-xs font-medium text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200"
                      >
                        Cancel
                      </button>
                    </span>
                  ) : (
                    <button
                      type="button"
                      onClick={() => {
                        setConfirmDeleteId(p.id);
                        setActionError("");
                      }}
                      className="rounded-md px-2.5 py-1 text-xs font-medium text-red-600 dark:text-red-400 transition hover:bg-red-50 dark:hover:bg-red-500/10"
                      data-testid={`delete-prefix-${p.id}`}
                    >
                      {p.status === "PENDING" ? "Withdraw" : "Remove"}
                    </button>
                  ))}
              </div>

              {p.status === "PENDING" && (
                <p className="mt-2 text-xs text-slate-500 dark:text-slate-400">
                  In review. Links under this prefix start working once it’s approved; we’ll email the owners.
                </p>
              )}
              {(p.status === "REJECTED" || p.status === "REVOKED") && p.decision_reason && (
                <p className="mt-2 text-xs text-red-600 dark:text-red-400">
                  {p.status === "REJECTED" ? "Not approved" : "Revoked"}: {p.decision_reason}
                </p>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
