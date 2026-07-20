// Developer API keys: create (with a one-time plaintext reveal), list with
// hint/status/created/last-used, and disable with inline confirm. Keys are
// never hard-deleted — disabled ones stay listed, greyed out. Any org member
// can manage keys (backend decision, ARCHITECTURE.md §4).

import { useCallback, useEffect, useState, type FormEvent } from "react";

import { ApiError, LIMIT_REACHED, endpoints } from "../lib/api";
import type { ApiKey, CreatedApiKey } from "../lib/types";
import { usePageTitle } from "../lib/usePageTitle";
import {
  CopyButton,
  EmptyIcon,
  EmptyState,
  ErrorBanner,
  NoticeBanner,
  PageLoader,
  Spinner,
  emptyIconPaths,
} from "../components/ui";

function friendlyError(err: unknown, fallback: string): string {
  if (err instanceof ApiError && err.message) {
    return err.message[0].toUpperCase() + err.message.slice(1);
  }

  return fallback;
}

function formatDate(iso: string | null): string {
  if (!iso) return "Never";

  const date = new Date(iso);

  return Number.isNaN(date.getTime())
    ? ""
    : date.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

function StatusBadge({ status }: { status: ApiKey["status"] }) {
  const styles =
    status === "ENABLED"
      ? "bg-emerald-50 text-emerald-700 ring-emerald-200"
      : "bg-slate-100 text-slate-500 ring-slate-200";

  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset ${styles}`}
    >
      {status.toLowerCase()}
    </span>
  );
}

/** One-time reveal of a freshly created key. */
function NewKeyPanel({ created, onDismiss }: { created: CreatedApiKey; onDismiss: () => void }) {
  return (
    <div
      className="space-y-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-4"
      data-testid="new-key-panel"
      role="status"
    >
      <p className="text-sm font-semibold text-amber-800">
        Your new key “{created.name}” is ready
      </p>

      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <code
          className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap rounded-md bg-white px-3 py-2 font-mono text-sm text-slate-800 ring-1 ring-inset ring-amber-200"
          data-testid="new-key-value"
        >
          {created.key}
        </code>
        <CopyButton text={created.key} label="Copy key" />
      </div>

      <p className="text-sm text-amber-700">
        Copy it now — for security, <strong>you won’t see this key again</strong>. Only a hash is
        stored on the server.
      </p>

      <button
        type="button"
        onClick={onDismiss}
        className="rounded-md px-2.5 py-1 text-xs font-medium text-amber-700 ring-1 ring-inset ring-amber-300 transition hover:bg-amber-100"
      >
        I’ve saved my key
      </button>
    </div>
  );
}

export default function ApiKeysPage() {
  usePageTitle("API keys");

  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");

  const load = useCallback(async () => {
    setLoadError("");

    try {
      setKeys(await endpoints.apiKeys());
    } catch (err: unknown) {
      setLoadError(friendlyError(err, "Could not load API keys. Please try again."));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // --- create (one-time key reveal) ---
  const [name, setName] = useState("");
  const [createBusy, setCreateBusy] = useState(false);
  const [createError, setCreateError] = useState("");
  // LIMIT_REACHED denial: the server's message, shown verbatim as a notice.
  const [limitNotice, setLimitNotice] = useState("");
  const [createdKey, setCreatedKey] = useState<CreatedApiKey | null>(null);

  const submitCreate = async (event: FormEvent) => {
    event.preventDefault();

    const trimmed = name.trim();
    if (!trimmed || createBusy) return;

    setCreateBusy(true);
    setCreateError("");
    setLimitNotice("");

    try {
      const created = await endpoints.createApiKey(trimmed);

      setCreatedKey(created);
      setName("");
      // The list row carries the hint only — never the plaintext key.
      setKeys((prev) => [
        {
          id: created.id,
          org_id: created.org_id,
          name: created.name,
          key_hint: created.key_hint,
          status: created.status,
          created_at: created.created_at,
          last_used_at: created.last_used_at,
        },
        ...prev,
      ]);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.code === LIMIT_REACHED) {
        setLimitNotice(err.message);
      } else {
        setCreateError(friendlyError(err, "Could not create the key."));
      }
    } finally {
      setCreateBusy(false);
    }
  };

  // --- disable (two-click inline confirm) ---
  const [confirmDisableId, setConfirmDisableId] = useState(0);
  const [disableBusyId, setDisableBusyId] = useState(0);
  const [disableError, setDisableError] = useState("");

  const disableKey = async (id: number) => {
    setDisableBusyId(id);
    setDisableError("");

    try {
      await endpoints.disableApiKey(id); // 204, no body

      setKeys((prev) => prev.map((k) => (k.id === id ? { ...k, status: "DISABLED" } : k)));
    } catch (err: unknown) {
      setDisableError(friendlyError(err, "Could not disable this key."));
    } finally {
      setDisableBusyId(0);
      setConfirmDisableId(0);
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
      {/* Create */}
      <section className="rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="border-b border-slate-100 px-4 py-4 sm:px-6">
          <h2 className="text-base font-semibold text-slate-900">Create an API key</h2>
          <p className="mt-0.5 text-sm text-slate-500">
            Use a key in the <code className="font-mono text-xs">X-API-Key</code> header to
            shorten links and resolve codes programmatically.
          </p>
        </div>

        <div className="space-y-4 px-4 py-4 sm:px-6">
          <form onSubmit={(e) => void submitCreate(e)} className="flex flex-col gap-2 sm:flex-row">
            <label htmlFor="key-name" className="sr-only">
              Key name
            </label>
            <input
              id="key-name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. CI pipeline"
              maxLength={255}
              required
              className="w-full flex-1 rounded-lg border border-slate-300 px-3 py-2 text-sm text-slate-900 shadow-sm placeholder:text-slate-400 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200"
            />
            <button
              type="submit"
              disabled={createBusy || name.trim() === ""}
              className="flex items-center justify-center gap-2 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-semibold text-white shadow-sm transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {createBusy && <Spinner className="h-4 w-4 text-white" />}
              {createBusy ? "Creating…" : "Create key"}
            </button>
          </form>

          {createError && <ErrorBanner message={createError} />}
          {limitNotice && <NoticeBanner message={limitNotice} />}
          {createdKey && (
            <NewKeyPanel created={createdKey} onDismiss={() => setCreatedKey(null)} />
          )}
        </div>
      </section>

      {/* List */}
      <section className="rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="border-b border-slate-100 px-4 py-4 sm:px-6">
          <h2 className="text-base font-semibold text-slate-900">API keys</h2>
          <p className="mt-0.5 text-sm text-slate-500">
            Disabled keys stop working immediately but stay listed — links they created keep
            their attribution.
          </p>
        </div>

        {disableError && (
          <div className="px-4 pt-4 sm:px-6">
            <ErrorBanner message={disableError} />
          </div>
        )}

        {keys.length === 0 ? (
          <EmptyState
            icon={<EmptyIcon path={emptyIconPaths.key} />}
            title="No API keys yet"
            hint="Create your first key above to use the API."
            testid="api-keys-empty"
          />
        ) : (
          <ul className="divide-y divide-slate-100" data-testid="api-keys-list">
            {keys.map((key) => {
              const disabled = key.status === "DISABLED";

              return (
                <li
                  key={key.id}
                  className={`flex flex-wrap items-center gap-x-4 gap-y-1.5 px-4 py-3.5 sm:px-6 ${
                    disabled ? "opacity-60" : ""
                  }`}
                  data-testid={`api-key-row-${key.id}`}
                >
                  <div className="min-w-0 flex-1 basis-48">
                    <p
                      className={`truncate text-sm font-medium ${
                        disabled ? "text-slate-400" : "text-slate-900"
                      }`}
                    >
                      {key.name}
                    </p>
                    <p className="truncate font-mono text-xs text-slate-400">{key.key_hint}…</p>
                  </div>

                  <div className="hidden text-right text-xs text-slate-400 sm:block">
                    <p>Created {formatDate(key.created_at)}</p>
                    <p>Last used {formatDate(key.last_used_at)}</p>
                  </div>

                  <StatusBadge status={key.status} />

                  {!disabled &&
                    (confirmDisableId === key.id ? (
                      <span className="flex items-center gap-2">
                        <button
                          type="button"
                          onClick={() => void disableKey(key.id)}
                          disabled={disableBusyId !== 0}
                          className="rounded-md bg-red-600 px-2.5 py-1 text-xs font-semibold text-white hover:bg-red-500 disabled:opacity-50"
                        >
                          {disableBusyId === key.id ? "Disabling…" : "Confirm"}
                        </button>
                        <button
                          type="button"
                          onClick={() => setConfirmDisableId(0)}
                          disabled={disableBusyId !== 0}
                          className="rounded-md px-2 py-1 text-xs font-medium text-slate-500 hover:text-slate-700"
                        >
                          Cancel
                        </button>
                      </span>
                    ) : (
                      <button
                        type="button"
                        onClick={() => {
                          setConfirmDisableId(key.id);
                          setDisableError("");
                        }}
                        className="rounded-md px-2.5 py-1 text-xs font-medium text-red-600 transition hover:bg-red-50"
                      >
                        Disable
                      </button>
                    ))}
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
}
