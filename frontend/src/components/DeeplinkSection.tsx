// Deep-link config editor for the link detail page (phase 4): platform-specific
// app links served to mobile visitors instead of the web destination.
// Android needs intent + package + scheme + fallback URL; iOS just an intent.

import { useEffect, useState, type FormEvent } from "react";

import { ApiError, endpoints } from "../lib/api";
import type { DeeplinkConfig, ShortURL } from "../lib/types";
import { Spinner, primaryButtonClass, secondaryButtonClass } from "./ui";

interface Props {
  link: ShortURL;
  onUpdated: (link: ShortURL) => void;
}

interface FormState {
  androidIntent: string;
  androidPackage: string;
  androidScheme: string;
  androidFallback: string;
  iosIntent: string;
}

function formFromConfig(config: DeeplinkConfig | null | undefined): FormState {
  return {
    androidIntent: config?.android?.intent ?? "",
    androidPackage: config?.android?.package ?? "",
    androidScheme: config?.android?.scheme ?? "",
    androidFallback: config?.android?.fallback_url ?? "",
    iosIntent: config?.ios?.intent ?? "",
  };
}

const inputClass =
  "w-full rounded-lg border border-slate-300 px-3 py-2 text-sm text-slate-900 shadow-sm " +
  "focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200";

function Field({
  id,
  label,
  value,
  placeholder,
  onChange,
}: {
  id: string;
  label: string;
  value: string;
  placeholder: string;
  onChange: (value: string) => void;
}) {
  return (
    <div>
      <label htmlFor={id} className="mb-1 block text-xs font-medium text-slate-500">
        {label}
      </label>
      <input
        id={id}
        type="text"
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        className={inputClass}
      />
    </div>
  );
}

export default function DeeplinkSection({ link, onUpdated }: Props) {
  const [form, setForm] = useState<FormState>(() => formFromConfig(link.deeplink_config));
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    setForm(formFromConfig(link.deeplink_config));
  }, [link.deeplink_config]);

  const set = (patch: Partial<FormState>) => {
    setForm((f) => ({ ...f, ...patch }));
    setError("");
    setNotice("");
  };

  const hasConfig = Boolean(link.deeplink_config?.android || link.deeplink_config?.ios);

  const androidFields = [
    form.androidIntent,
    form.androidPackage,
    form.androidScheme,
    form.androidFallback,
  ].map((v) => v.trim());
  const androidAny = androidFields.some((v) => v !== "");
  const androidComplete = androidFields.every((v) => v !== "");
  const iosIntent = form.iosIntent.trim();
  const formEmpty = !androidAny && iosIntent === "";

  const save = async (event: FormEvent) => {
    event.preventDefault();

    if (busy) return;

    if (androidAny && !androidComplete) {
      setError("Android needs all four fields: intent, package, scheme and fallback URL.");
      return;
    }

    const config: DeeplinkConfig | null = formEmpty
      ? null
      : {
          ...(androidAny
            ? {
                android: {
                  intent: androidFields[0],
                  package: androidFields[1],
                  scheme: androidFields[2],
                  fallback_url: androidFields[3],
                },
              }
            : {}),
          ...(iosIntent !== "" ? { ios: { intent: iosIntent } } : {}),
        };

    await submit(config, config === null ? "Deep links cleared." : "Deep links saved.");
  };

  const clear = async () => {
    if (busy) return;
    await submit(null, "Deep links cleared.");
  };

  const submit = async (config: DeeplinkConfig | null, successNotice: string) => {
    setBusy(true);
    setError("");
    setNotice("");

    try {
      const updated = await endpoints.setDeeplink(link.id, config);
      onUpdated(updated);
      setNotice(successNotice);
      setEditing(false); // collapse back to the summary view
    } catch (err: unknown) {
      setError(
        err instanceof ApiError && err.status === 422
          ? err.message
          : "Could not save the deep-link config. Please try again.",
      );
    } finally {
      setBusy(false);
    }
  };

  const startEdit = () => {
    setForm(formFromConfig(link.deeplink_config));
    setError("");
    setNotice("");
    setEditing(true);
  };

  const cancelEdit = () => {
    setForm(formFromConfig(link.deeplink_config));
    setError("");
    setEditing(false);
  };

  const android = link.deeplink_config?.android;
  const ios = link.deeplink_config?.ios;

  return (
    <section className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h3 className="text-base font-semibold text-slate-900">Mobile deep links</h3>
          <p className="mt-0.5 text-sm text-slate-500">
            Send app users straight into your app instead of the web destination.
          </p>
        </div>
        {!editing &&
          (hasConfig ? (
            <div className="flex shrink-0 gap-2">
              <button
                type="button"
                onClick={startEdit}
                className={`${secondaryButtonClass} px-3 py-1.5`}
                data-testid="deeplink-edit"
              >
                Edit
              </button>
              <button
                type="button"
                onClick={() => void clear()}
                disabled={busy}
                className="rounded-lg border border-red-200 px-3 py-1.5 text-sm font-medium text-red-600 transition hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-50"
                data-testid="deeplink-clear"
              >
                Clear
              </button>
            </div>
          ) : (
            <button
              type="button"
              onClick={startEdit}
              className={`${secondaryButtonClass} shrink-0 px-3 py-1.5`}
              data-testid="deeplink-configure"
            >
              Configure
            </button>
          ))}
      </div>

      {/* Collapsed states: one-line empty hint, or a readable summary */}
      {!editing && !hasConfig && (
        <p
          className="mt-4 rounded-lg border border-dashed border-slate-200 bg-slate-50/60 px-3 py-2.5 text-sm text-slate-400"
          data-testid="deeplink-empty"
        >
          Not configured — mobile visitors follow the web destination.
        </p>
      )}

      {!editing && hasConfig && (
        <div className="mt-4 space-y-2" data-testid="deeplink-badges">
          {android && (
            <div className="flex min-w-0 items-start gap-2.5 rounded-lg bg-slate-50 px-3 py-2">
              <span className="mt-0.5 shrink-0 rounded-full bg-emerald-50 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-emerald-700 ring-1 ring-inset ring-emerald-200">
                Android
              </span>
              <div className="min-w-0 text-sm">
                <p className="truncate font-mono text-xs text-slate-700" title={`${android.scheme}://${android.intent} · ${android.package}`}>
                  {android.scheme}://{android.intent}
                  <span className="text-slate-400"> · {android.package}</span>
                </p>
                <p className="mt-0.5 truncate text-xs text-slate-500" title={android.fallback_url}>
                  No app? Falls back to {android.fallback_url}
                </p>
              </div>
            </div>
          )}
          {ios && (
            <div className="flex min-w-0 items-start gap-2.5 rounded-lg bg-slate-50 px-3 py-2">
              <span className="mt-0.5 shrink-0 rounded-full bg-emerald-50 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-emerald-700 ring-1 ring-inset ring-emerald-200">
                iOS
              </span>
              <p className="min-w-0 truncate font-mono text-xs leading-5 text-slate-700" title={ios.intent}>
                {ios.intent}
              </p>
            </div>
          )}
        </div>
      )}

      {editing && (
      <form onSubmit={(e) => void save(e)} className="mt-4 space-y-4">
        <fieldset className="rounded-lg border border-slate-200 p-3">
          <legend className="px-1 text-xs font-semibold uppercase tracking-wide text-slate-400">
            Android
          </legend>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field
              id="dl-android-intent"
              label="Intent"
              value={form.androidIntent}
              placeholder="shop/product/42"
              onChange={(v) => set({ androidIntent: v })}
            />
            <Field
              id="dl-android-package"
              label="Package"
              value={form.androidPackage}
              placeholder="com.example.app"
              onChange={(v) => set({ androidPackage: v })}
            />
            <Field
              id="dl-android-scheme"
              label="Scheme"
              value={form.androidScheme}
              placeholder="exampleapp"
              onChange={(v) => set({ androidScheme: v })}
            />
            <Field
              id="dl-android-fallback"
              label="Fallback URL"
              value={form.androidFallback}
              placeholder="https://example.com/get-the-app"
              onChange={(v) => set({ androidFallback: v })}
            />
          </div>
        </fieldset>

        <fieldset className="rounded-lg border border-slate-200 p-3">
          <legend className="px-1 text-xs font-semibold uppercase tracking-wide text-slate-400">
            iOS
          </legend>
          <Field
            id="dl-ios-intent"
            label="Intent / universal link"
            value={form.iosIntent}
            placeholder="https://apps.apple.com/app/id0000000000"
            onChange={(v) => set({ iosIntent: v })}
          />
        </fieldset>

        <div className="flex flex-wrap gap-2">
          <button
            type="submit"
            disabled={busy || (formEmpty && !hasConfig)}
            className={primaryButtonClass}
            data-testid="deeplink-save"
          >
            {busy && <Spinner className="h-4 w-4 text-white" />}
            {busy ? "Saving…" : "Save deep links"}
          </button>
          <button
            type="button"
            onClick={cancelEdit}
            disabled={busy}
            className={secondaryButtonClass}
            data-testid="deeplink-cancel"
          >
            Cancel
          </button>
        </div>
      </form>
      )}

      {error && (
        <p className="mt-2 text-sm text-red-600" role="alert" data-testid="deeplink-error">
          {error}
        </p>
      )}
      {notice && (
        <p className="mt-2 text-sm text-emerald-700" role="status" data-testid="deeplink-notice">
          {notice}
        </p>
      )}
    </section>
  );
}
