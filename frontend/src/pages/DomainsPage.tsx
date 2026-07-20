// Custom domains: register a hostname (OWNER), publish the shown DNS TXT
// record, verify, then set a primary domain — short URLs across the app
// switch to it. Members see a read-only list. Verification failures (409:
// record missing / not propagated / wrong value) surface verbatim per row.

import { useCallback, useEffect, useState, type FormEvent } from "react";

import { useAuth } from "../auth/AuthContext";
import { ApiError, endpoints } from "../lib/api";
import type { OrgDomain } from "../lib/types";
import { usePageTitle } from "../lib/usePageTitle";
import {
  CopyButton,
  EmptyIcon,
  EmptyState,
  ErrorBanner,
  PageLoader,
  Spinner,
} from "../components/ui";

function friendlyError(err: unknown, fallback: string): string {
  if (err instanceof ApiError) {
    if (err.status === 403) return "Only an organization owner can manage domains.";
    if (err.message) return err.message[0].toUpperCase() + err.message.slice(1);
  }

  return fallback;
}

function formatDate(iso: string | null): string {
  if (!iso) return "";

  const date = new Date(iso);

  return Number.isNaN(date.getTime())
    ? ""
    : date.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

function StatusBadge({ status }: { status: OrgDomain["status"] }) {
  const styles =
    status === "VERIFIED"
      ? "bg-emerald-50 text-emerald-700 ring-emerald-200"
      : status === "PENDING"
        ? "bg-amber-50 text-amber-700 ring-amber-200"
        : "bg-slate-100 text-slate-500 ring-slate-200";

  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset ${styles}`}
    >
      {status.toLowerCase()}
    </span>
  );
}

function PrimaryBadge() {
  return (
    <span className="inline-flex items-center rounded-full bg-indigo-50 px-2.5 py-0.5 text-xs font-medium text-indigo-700 ring-1 ring-inset ring-indigo-200">
      primary
    </span>
  );
}

/** The DNS record the owner must publish, with copy buttons. */
function TxtInstructions({ domain }: { domain: OrgDomain }) {
  return (
    <div className="mt-3 space-y-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3">
      <p className="text-sm text-amber-800">
        Add this DNS <strong>TXT record</strong> at your DNS provider, then click Verify.
        DNS changes can take a few minutes to propagate.
      </p>

      <div className="space-y-2">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <span className="w-14 shrink-0 text-xs font-semibold uppercase tracking-wide text-amber-700">
            Name
          </span>
          <code className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap rounded-md bg-white px-3 py-1.5 font-mono text-xs text-slate-800 ring-1 ring-inset ring-amber-200">
            {domain.txt_record_name}
          </code>
          <CopyButton text={domain.txt_record_name} label="Copy name" />
        </div>

        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <span className="w-14 shrink-0 text-xs font-semibold uppercase tracking-wide text-amber-700">
            Value
          </span>
          <code className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap rounded-md bg-white px-3 py-1.5 font-mono text-xs text-slate-800 ring-1 ring-inset ring-amber-200">
            {domain.txt_record_value}
          </code>
          <CopyButton text={domain.txt_record_value} label="Copy value" />
        </div>
      </div>
    </div>
  );
}

export default function DomainsPage() {
  usePageTitle("Domains");

  const { role } = useAuth();
  const isOwner = role === "OWNER";

  const [domains, setDomains] = useState<OrgDomain[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");

  const load = useCallback(async () => {
    setLoadError("");

    try {
      setDomains(await endpoints.domains());
    } catch (err: unknown) {
      setLoadError(friendlyError(err, "Could not load domains. Please try again."));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // --- add ---
  const [hostname, setHostname] = useState("");
  const [addBusy, setAddBusy] = useState(false);
  const [addError, setAddError] = useState("");
  const [addNotice, setAddNotice] = useState("");

  const submitAdd = async (event: FormEvent) => {
    event.preventDefault();

    const trimmed = hostname.trim();
    if (!trimmed || addBusy) return;

    setAddBusy(true);
    setAddError("");
    setAddNotice("");

    try {
      const created = await endpoints.createDomain(trimmed);

      setDomains((prev) => [...prev, created]);
      setHostname("");
      setAddNotice(
        `${created.hostname} added. Publish the TXT record shown below, then click Verify.`,
      );
    } catch (err: unknown) {
      setAddError(friendlyError(err, "Could not add the domain."));
    } finally {
      setAddBusy(false);
    }
  };

  // --- verify (per-row busy + per-row failure message) ---
  const [verifyBusyId, setVerifyBusyId] = useState(0);
  const [verifyErrors, setVerifyErrors] = useState<Record<number, string>>({});

  const verify = async (id: number) => {
    setVerifyBusyId(id);
    setVerifyErrors((prev) => ({ ...prev, [id]: "" }));

    try {
      const verified = await endpoints.verifyDomain(id);

      setDomains((prev) => prev.map((d) => (d.id === id ? verified : d)));
    } catch (err: unknown) {
      setVerifyErrors((prev) => ({
        ...prev,
        [id]: friendlyError(err, "Verification failed. Please try again."),
      }));
    } finally {
      setVerifyBusyId(0);
    }
  };

  // --- set primary ---
  const [primaryBusyId, setPrimaryBusyId] = useState(0);
  const [actionError, setActionError] = useState("");

  const setPrimary = async (id: number) => {
    setPrimaryBusyId(id);
    setActionError("");

    try {
      const primary = await endpoints.setPrimaryDomain(id);

      // Single primary per org: reflect the server-side swap locally.
      setDomains((prev) =>
        prev.map((d) => (d.id === id ? primary : { ...d, is_primary: false })),
      );
    } catch (err: unknown) {
      setActionError(friendlyError(err, "Could not set the primary domain."));
    } finally {
      setPrimaryBusyId(0);
    }
  };

  // --- delete (two-click inline confirm) ---
  const [confirmDeleteId, setConfirmDeleteId] = useState(0);
  const [deleteBusyId, setDeleteBusyId] = useState(0);

  const deleteDomain = async (id: number) => {
    setDeleteBusyId(id);
    setActionError("");

    try {
      await endpoints.deleteDomain(id); // 204, no body

      setDomains((prev) => prev.filter((d) => d.id !== id));
    } catch (err: unknown) {
      setActionError(friendlyError(err, "Could not remove the domain."));
    } finally {
      setDeleteBusyId(0);
      setConfirmDeleteId(0);
    }
  };

  if (loading) return <PageLoader />;

  if (loadError) {
    return (
      <ErrorBanner
        message={loadError}
        onRetry={() => {
          setLoading(true);
          void load();
        }}
      />
    );
  }

  return (
    <div className="space-y-8">
      {/* Add */}
      <section className="rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="border-b border-slate-100 px-4 py-4 sm:px-6">
          <h2 className="text-base font-semibold text-slate-900">Add a custom domain</h2>
          <p className="mt-0.5 text-sm text-slate-500">
            Serve your short links from your own domain, like{" "}
            <code className="font-mono text-xs">links.your-brand.com</code>. You’ll prove
            ownership with a DNS TXT record.
          </p>
        </div>

        <div className="space-y-4 px-4 py-4 sm:px-6">
          {isOwner ? (
            <form onSubmit={(e) => void submitAdd(e)} className="flex flex-col gap-2 sm:flex-row">
              <label htmlFor="domain-hostname" className="sr-only">
                Hostname
              </label>
              <input
                id="domain-hostname"
                type="text"
                value={hostname}
                onChange={(e) => setHostname(e.target.value)}
                placeholder="links.your-brand.com"
                maxLength={253}
                required
                className="w-full flex-1 rounded-lg border border-slate-300 px-3 py-2 text-sm text-slate-900 shadow-sm placeholder:text-slate-400 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200"
              />
              <button
                type="submit"
                disabled={addBusy || hostname.trim() === ""}
                className="flex items-center justify-center gap-2 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-semibold text-white shadow-sm transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {addBusy && <Spinner className="h-4 w-4 text-white" />}
                {addBusy ? "Adding…" : "Add domain"}
              </button>
            </form>
          ) : (
            <p className="rounded-lg bg-slate-50 px-4 py-3 text-sm text-slate-500">
              Only organization owners can add, verify, or remove domains.
            </p>
          )}

          {addError && <ErrorBanner message={addError} />}
          {addNotice && (
            <p
              className="rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700"
              role="status"
            >
              {addNotice}
            </p>
          )}
        </div>
      </section>

      {/* List */}
      <section className="rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="border-b border-slate-100 px-4 py-4 sm:px-6">
          <h2 className="text-base font-semibold text-slate-900">Domains</h2>
          <p className="mt-0.5 text-sm text-slate-500">
            Short links display under the primary verified domain. They always keep working on
            the deployment’s domain too.
          </p>
        </div>

        {actionError && (
          <div className="px-4 pt-4 sm:px-6">
            <ErrorBanner message={actionError} />
          </div>
        )}

        {domains.length === 0 ? (
          <EmptyState
            icon={
              <EmptyIcon path="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zM2 12h20M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" />
            }
            title="No custom domains yet"
            hint={
              isOwner
                ? "Add your first domain above to brand your short links."
                : "An organization owner can add one."
            }
            testid="domains-empty"
          />
        ) : (
          <ul className="divide-y divide-slate-100" data-testid="domains-list">
            {domains.map((domain) => {
              const pending = domain.status === "PENDING";
              const verified = domain.status === "VERIFIED";
              const verifyError = verifyErrors[domain.id] ?? "";

              return (
                <li key={domain.id} className="px-4 py-4 sm:px-6" data-testid={`domain-row-${domain.id}`}>
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
                    <div className="min-w-0 flex-1 basis-48">
                      <p className="truncate font-mono text-sm font-medium text-slate-900">
                        {domain.hostname}
                      </p>
                      <p className="text-xs text-slate-400">
                        Added {formatDate(domain.created_at)}
                        {verified && domain.verified_at
                          ? ` · verified ${formatDate(domain.verified_at)}`
                          : ""}
                      </p>
                    </div>

                    {domain.is_primary && <PrimaryBadge />}
                    <StatusBadge status={domain.status} />

                    {isOwner && pending && (
                      <button
                        type="button"
                        onClick={() => void verify(domain.id)}
                        disabled={verifyBusyId !== 0}
                        className="flex items-center gap-1.5 rounded-md bg-indigo-600 px-2.5 py-1 text-xs font-semibold text-white transition hover:bg-indigo-500 disabled:opacity-50"
                        data-testid={`verify-domain-${domain.id}`}
                      >
                        {verifyBusyId === domain.id && <Spinner className="h-3 w-3 text-white" />}
                        {verifyBusyId === domain.id ? "Checking…" : "Verify"}
                      </button>
                    )}

                    {isOwner && verified && !domain.is_primary && (
                      <button
                        type="button"
                        onClick={() => void setPrimary(domain.id)}
                        disabled={primaryBusyId !== 0}
                        className="rounded-md px-2.5 py-1 text-xs font-medium text-indigo-600 ring-1 ring-inset ring-indigo-200 transition hover:bg-indigo-50 disabled:opacity-50"
                        data-testid={`set-primary-${domain.id}`}
                      >
                        {primaryBusyId === domain.id ? "Setting…" : "Set primary"}
                      </button>
                    )}

                    {isOwner &&
                      (confirmDeleteId === domain.id ? (
                        <span className="flex items-center gap-2">
                          <button
                            type="button"
                            onClick={() => void deleteDomain(domain.id)}
                            disabled={deleteBusyId !== 0}
                            className="rounded-md bg-red-600 px-2.5 py-1 text-xs font-semibold text-white hover:bg-red-500 disabled:opacity-50"
                          >
                            {deleteBusyId === domain.id ? "Removing…" : "Confirm"}
                          </button>
                          <button
                            type="button"
                            onClick={() => setConfirmDeleteId(0)}
                            disabled={deleteBusyId !== 0}
                            className="rounded-md px-2 py-1 text-xs font-medium text-slate-500 hover:text-slate-700"
                          >
                            Cancel
                          </button>
                        </span>
                      ) : (
                        <button
                          type="button"
                          onClick={() => {
                            setConfirmDeleteId(domain.id);
                            setActionError("");
                          }}
                          className="rounded-md px-2.5 py-1 text-xs font-medium text-red-600 transition hover:bg-red-50"
                          data-testid={`delete-domain-${domain.id}`}
                        >
                          Remove
                        </button>
                      ))}
                  </div>

                  {pending && <TxtInstructions domain={domain} />}

                  {verifyError && (
                    <div className="mt-3" data-testid={`verify-error-${domain.id}`}>
                      <ErrorBanner message={verifyError} />
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
}
