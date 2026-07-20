// Target-rules builder + list for the link detail page (phase 4).
// Rules route visitors by mobile OS and/or GeoIP city; they evaluate in
// creation order and the first rule whose conditions ALL match wins.

import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";

import { ApiError, endpoints } from "../lib/api";
import type {
  RuleCondition,
  RuleConditionType,
  RuleInput,
  ShortURL,
  TargetRule,
} from "../lib/types";
import { EmptyIcon, EmptyState, ErrorBanner, Spinner, primaryButtonClass } from "./ui";

const DEVICE_OPTIONS = ["iOS", "Android", "Windows", "Other"];

const inputClass =
  "w-full rounded-lg border border-slate-300 px-3 py-2 text-sm text-slate-900 shadow-sm " +
  "focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200";

const operatorClass =
  "rounded-lg border border-slate-300 px-2 py-1.5 text-sm text-slate-700 shadow-sm " +
  "focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200";

// ---------------------------------------------------------------------------
// Readable rule sentence: "If OS is Android and city is not Bengaluru → url"
// ---------------------------------------------------------------------------

function conditionPhrase(subject: string, cond: RuleCondition): string {
  const op = cond.type === "is_not" ? "is not" : "is";
  return `${subject} ${op} ${cond.values.join(" or ")}`;
}

export function ruleSentence(rule: TargetRule): string {
  const parts: string[] = [];
  if (rule.device_type) parts.push(conditionPhrase("OS", rule.device_type));
  if (rule.location) parts.push(conditionPhrase("city", rule.location));
  return `If ${parts.join(" and ")}`;
}

// ---------------------------------------------------------------------------
// City multi-select with debounced autocomplete (free text when the
// deployment has no city dataset — API returns 501)
// ---------------------------------------------------------------------------

function CityMultiSelect({
  values,
  onChange,
}: {
  values: string[];
  onChange: (values: string[]) => void;
}) {
  const [query, setQuery] = useState("");
  const [suggestions, setSuggestions] = useState<string[]>([]);
  const [open, setOpen] = useState(false);
  const [freeText, setFreeText] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const search = (q: string) => {
    setQuery(q);

    if (timer.current) clearTimeout(timer.current);

    const trimmed = q.trim();
    if (freeText || trimmed === "") {
      setSuggestions([]);
      setOpen(false);
      return;
    }

    timer.current = setTimeout(() => {
      endpoints
        .cities(trimmed)
        .then((cities) => {
          setSuggestions(cities.filter((c) => !values.includes(c)));
          setOpen(true);
        })
        .catch((err: unknown) => {
          if (err instanceof ApiError && err.status === 501) {
            setFreeText(true); // no dataset on this deployment: type city names manually
          }
          setSuggestions([]);
          setOpen(false);
        });
    }, 250);
  };

  const add = (city: string) => {
    const trimmed = city.trim();
    if (trimmed !== "" && !values.includes(trimmed)) onChange([...values, trimmed]);

    setQuery("");
    setSuggestions([]);
    setOpen(false);
  };

  const remove = (city: string) => onChange(values.filter((v) => v !== city));

  return (
    <div>
      {values.length > 0 && (
        <div className="mb-2 flex flex-wrap gap-1.5">
          {values.map((city) => (
            <span
              key={city}
              className="inline-flex items-center gap-1 rounded-full bg-indigo-50 px-2.5 py-0.5 text-xs font-medium text-indigo-700 ring-1 ring-inset ring-indigo-200"
            >
              {city}
              <button
                type="button"
                onClick={() => remove(city)}
                aria-label={`Remove ${city}`}
                className="text-indigo-400 hover:text-indigo-700"
              >
                &times;
              </button>
            </span>
          ))}
        </div>
      )}

      <div className="relative">
        <div className="flex gap-2">
          <input
            type="text"
            value={query}
            placeholder={freeText ? "Type a city and press Add" : "Start typing a city…"}
            onChange={(e) => search(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                if (freeText || suggestions.length === 0) add(query);
                else add(suggestions[0]);
              }
            }}
            className={inputClass}
            data-testid="city-input"
          />
          {freeText && (
            <button
              type="button"
              onClick={() => add(query)}
              disabled={query.trim() === ""}
              className="rounded-lg border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
            >
              Add
            </button>
          )}
        </div>

        {open && suggestions.length > 0 && (
          <ul
            className="absolute z-10 mt-1 max-h-48 w-full overflow-auto rounded-lg border border-slate-200 bg-white py-1 shadow-lg"
            data-testid="city-suggestions"
          >
            {suggestions.map((city) => (
              <li key={city}>
                <button
                  type="button"
                  onClick={() => add(city)}
                  className="block w-full px-3 py-1.5 text-left text-sm text-slate-700 hover:bg-indigo-50"
                >
                  {city}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      {freeText && (
        <p className="mt-1 text-xs text-slate-400">
          City autocomplete is not configured on this deployment — enter city names manually.
        </p>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Rule builder (create + edit)
// ---------------------------------------------------------------------------

interface BuilderState {
  targetName: string;
  deviceEnabled: boolean;
  deviceOp: RuleConditionType;
  deviceValues: string[];
  locationEnabled: boolean;
  locationOp: RuleConditionType;
  locationValues: string[];
  url: string;
}

function emptyBuilder(): BuilderState {
  return {
    targetName: "",
    deviceEnabled: false,
    deviceOp: "is",
    deviceValues: [],
    locationEnabled: false,
    locationOp: "is",
    locationValues: [],
    url: "",
  };
}

function builderFromRule(rule: TargetRule): BuilderState {
  return {
    targetName: rule.target_name,
    deviceEnabled: Boolean(rule.device_type),
    deviceOp: rule.device_type?.type ?? "is",
    deviceValues: rule.device_type?.values ?? [],
    locationEnabled: Boolean(rule.location),
    locationOp: rule.location?.type ?? "is",
    locationValues: rule.location?.values ?? [],
    url: rule.url,
  };
}

function RuleBuilder({
  editing,
  busy,
  onSubmit,
  onCancel,
}: {
  editing: TargetRule | null;
  busy: boolean;
  onSubmit: (input: RuleInput) => void;
  onCancel: () => void;
}) {
  const [form, setForm] = useState<BuilderState>(() =>
    editing ? builderFromRule(editing) : emptyBuilder(),
  );
  const [validation, setValidation] = useState("");

  const set = (patch: Partial<BuilderState>) => {
    setForm((f) => ({ ...f, ...patch }));
    setValidation("");
  };

  const toggleDevice = (value: string) => {
    set({
      deviceValues: form.deviceValues.includes(value)
        ? form.deviceValues.filter((v) => v !== value)
        : [...form.deviceValues, value],
    });
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();

    if (busy) return;

    if (form.targetName.trim() === "") {
      setValidation("Give this rule a name.");
      return;
    }

    if (!form.deviceEnabled && !form.locationEnabled) {
      setValidation("Add at least one condition (device or location).");
      return;
    }

    if (form.deviceEnabled && form.deviceValues.length === 0) {
      setValidation("Pick at least one operating system for the device condition.");
      return;
    }

    if (form.locationEnabled && form.locationValues.length === 0) {
      setValidation("Add at least one city for the location condition.");
      return;
    }

    if (form.url.trim() === "") {
      setValidation("Set the destination URL this rule redirects to.");
      return;
    }

    onSubmit({
      target_name: form.targetName.trim(),
      device_type: form.deviceEnabled
        ? { type: form.deviceOp, values: form.deviceValues }
        : undefined,
      location: form.locationEnabled
        ? { type: form.locationOp, values: form.locationValues }
        : undefined,
      url: form.url.trim(),
    });
  };

  return (
    <form
      onSubmit={submit}
      className="space-y-4 rounded-lg border border-indigo-200 bg-indigo-50/40 p-3 sm:p-4"
      data-testid="rule-builder"
    >
      <div>
        <label htmlFor="rule-name" className="mb-1 block text-xs font-medium text-slate-500">
          Rule name
        </label>
        <input
          id="rule-name"
          type="text"
          value={form.targetName}
          placeholder="Android users in Bengaluru"
          onChange={(e) => set({ targetName: e.target.value })}
          className={inputClass}
        />
      </div>

      {/* Device condition */}
      <div className="rounded-lg border border-slate-200 bg-white p-3">
        <label className="flex items-center gap-2 text-sm font-medium text-slate-700">
          <input
            type="checkbox"
            checked={form.deviceEnabled}
            onChange={(e) => set({ deviceEnabled: e.target.checked })}
            className="h-4 w-4 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
            data-testid="rule-device-toggle"
          />
          Device condition
        </label>

        {form.deviceEnabled && (
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <span className="text-sm text-slate-500">OS</span>
            <select
              value={form.deviceOp}
              onChange={(e) => set({ deviceOp: e.target.value as RuleConditionType })}
              className={operatorClass}
              aria-label="Device operator"
            >
              <option value="is">is</option>
              <option value="is_not">is not</option>
            </select>
            <div className="flex flex-wrap gap-1.5">
              {DEVICE_OPTIONS.map((os) => (
                <button
                  key={os}
                  type="button"
                  onClick={() => toggleDevice(os)}
                  className={`rounded-full px-3 py-1 text-xs font-medium ring-1 ring-inset transition ${
                    form.deviceValues.includes(os)
                      ? "bg-indigo-600 text-white ring-indigo-600"
                      : "bg-white text-slate-600 ring-slate-300 hover:bg-slate-50"
                  }`}
                  aria-pressed={form.deviceValues.includes(os)}
                >
                  {os}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Location condition */}
      <div className="rounded-lg border border-slate-200 bg-white p-3">
        <label className="flex items-center gap-2 text-sm font-medium text-slate-700">
          <input
            type="checkbox"
            checked={form.locationEnabled}
            onChange={(e) => set({ locationEnabled: e.target.checked })}
            className="h-4 w-4 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
            data-testid="rule-location-toggle"
          />
          Location condition
        </label>

        {form.locationEnabled && (
          <div className="mt-3 space-y-2">
            <div className="flex items-center gap-2">
              <span className="text-sm text-slate-500">City</span>
              <select
                value={form.locationOp}
                onChange={(e) => set({ locationOp: e.target.value as RuleConditionType })}
                className={operatorClass}
                aria-label="Location operator"
              >
                <option value="is">is</option>
                <option value="is_not">is not</option>
              </select>
            </div>
            <CityMultiSelect
              values={form.locationValues}
              onChange={(values) => set({ locationValues: values })}
            />
          </div>
        )}
      </div>

      <div>
        <label htmlFor="rule-url" className="mb-1 block text-xs font-medium text-slate-500">
          Redirect to
        </label>
        <input
          id="rule-url"
          type="text"
          value={form.url}
          placeholder="https://play.google.com/store/apps/details?id=…"
          onChange={(e) => set({ url: e.target.value })}
          className={inputClass}
        />
      </div>

      {validation && (
        <p className="text-sm text-red-600" role="alert" data-testid="rule-validation">
          {validation}
        </p>
      )}

      <div className="flex flex-wrap gap-2">
        <button type="submit" disabled={busy} className={primaryButtonClass} data-testid="rule-save">
          {busy && <Spinner className="h-4 w-4 text-white" />}
          {editing ? "Save changes" : "Add rule"}
        </button>
        <button
          type="button"
          onClick={onCancel}
          disabled={busy}
          className="rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 transition hover:bg-slate-50"
        >
          Cancel
        </button>
      </div>
    </form>
  );
}

// ---------------------------------------------------------------------------
// Section: rule list + builder orchestration
// ---------------------------------------------------------------------------

export default function TargetRulesSection({ link }: { link: ShortURL }) {
  const [rules, setRules] = useState<TargetRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");

  const [builderOpen, setBuilderOpen] = useState(false);
  const [editing, setEditing] = useState<TargetRule | null>(null);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState("");

  const load = useCallback(async () => {
    setLoadError("");

    try {
      setRules(await endpoints.rules(link.id));
    } catch {
      setLoadError("Could not load the target rules. Please try again.");
    } finally {
      setLoading(false);
    }
  }, [link.id]);

  useEffect(() => {
    void load();
  }, [load]);

  const startCreate = () => {
    setEditing(null);
    setBuilderOpen(true);
    setActionError("");
  };

  const startEdit = (rule: TargetRule) => {
    setEditing(rule);
    setBuilderOpen(true);
    setActionError("");
  };

  const closeBuilder = () => {
    setBuilderOpen(false);
    setEditing(null);
  };

  const save = async (input: RuleInput) => {
    setBusy(true);
    setActionError("");

    try {
      if (editing) {
        const updated = await endpoints.updateRule(editing.id, input);
        setRules((rs) => rs.map((r) => (r.id === updated.id ? updated : r)));
      } else {
        const created = await endpoints.createRules(link.id, [input]);
        setRules((rs) => [...rs, ...created]);
      }

      closeBuilder();
    } catch (err: unknown) {
      setActionError(
        err instanceof ApiError && err.status === 422
          ? err.message
          : "Could not save the rule. Please try again.",
      );
    } finally {
      setBusy(false);
    }
  };

  const remove = async (rule: TargetRule) => {
    setActionError("");

    try {
      await endpoints.deleteRule(rule.id);
      setRules((rs) => rs.filter((r) => r.id !== rule.id));
    } catch {
      setActionError("Could not delete the rule. Please try again.");
    }
  };

  return (
    <section className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h3 className="text-base font-semibold text-slate-900">Target rules</h3>
          <p className="mt-0.5 text-sm text-slate-500">
            Route visitors by device or city. Rules run in order — the first match wins.
          </p>
        </div>
        {!builderOpen && !loading && rules.length > 0 && (
          <button
            type="button"
            onClick={startCreate}
            className="rounded-lg bg-indigo-600 px-3 py-1.5 text-sm font-semibold text-white shadow-sm transition hover:bg-indigo-500"
            data-testid="rule-add"
          >
            Add rule
          </button>
        )}
      </div>

      {loadError && (
        <div className="mt-4">
          <ErrorBanner message={loadError} />
        </div>
      )}

      {loading ? (
        <div className="flex justify-center py-6">
          <Spinner />
        </div>
      ) : (
        <>
          {rules.length === 0 && !builderOpen && !loadError && (
            <EmptyState
              icon={<EmptyIcon path="M22 3H2l8 9.46V19l4 2v-8.54L22 3z" />}
              title="No rules yet"
              hint="Every visitor goes to the default destination."
              action={
                <button
                  type="button"
                  onClick={startCreate}
                  className={`${primaryButtonClass} px-3 py-1.5`}
                  data-testid="rule-add"
                >
                  Add rule
                </button>
              }
              testid="rules-empty"
            />
          )}

          {rules.length > 0 && (
            <ol className="mt-4 space-y-2" data-testid="rule-list">
              {rules.map((rule, index) => (
                <li
                  key={rule.id}
                  className="rounded-lg border border-slate-200 p-3"
                  data-testid="rule-card"
                >
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-slate-100 text-xs font-semibold text-slate-500">
                          {index + 1}
                        </span>
                        <span className="truncate text-sm font-semibold text-slate-800">
                          {rule.target_name}
                        </span>
                      </div>
                      <p className="mt-1 break-all text-sm text-slate-600">
                        {ruleSentence(rule)}{" "}
                        <span className="text-slate-400">&rarr;</span>{" "}
                        <span className="text-indigo-600">{rule.url}</span>
                      </p>
                    </div>
                    <div className="flex shrink-0 gap-1">
                      <button
                        type="button"
                        onClick={() => startEdit(rule)}
                        className="rounded-md px-2 py-1 text-xs font-medium text-slate-600 ring-1 ring-inset ring-slate-200 hover:bg-slate-50"
                        data-testid="rule-edit"
                      >
                        Edit
                      </button>
                      <button
                        type="button"
                        onClick={() => void remove(rule)}
                        className="rounded-md px-2 py-1 text-xs font-medium text-red-600 ring-1 ring-inset ring-red-200 hover:bg-red-50"
                        data-testid="rule-delete"
                      >
                        Delete
                      </button>
                    </div>
                  </div>
                </li>
              ))}
            </ol>
          )}

          {builderOpen && (
            <div className="mt-4">
              <RuleBuilder
                key={editing?.id ?? "new"}
                editing={editing}
                busy={busy}
                onSubmit={(input) => void save(input)}
                onCancel={closeBuilder}
              />
            </div>
          )}

          {actionError && (
            <p className="mt-2 text-sm text-red-600" role="alert" data-testid="rule-error">
              {actionError}
            </p>
          )}
        </>
      )}
    </section>
  );
}
