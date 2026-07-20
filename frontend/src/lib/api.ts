// Typed API client for the ogtr backend (Gofr).
//
// - Base URL from VITE_API_URL; defaults to "" (same origin — the Vite dev
//   server proxies /api/* to the backend, and production can serve the SPA
//   and API behind one host or set VITE_API_URL at build time).
// - Attaches the access token, transparently refreshes on 401 (single-flight:
//   concurrent 401s all await one refresh, then retry once).
// - Unwraps Gofr's {"data": ...} success envelope and surfaces its
//   {"error": {"message": ...}} error envelope as ApiError; an optional
//   machine-readable {"error": {"code": ...}} (e.g. LIMIT_REACHED) is
//   surfaced as ApiError.code.

import type {
  ApiKey,
  AuthProvidersInfo,
  AuthResult,
  CreatedApiKey,
  CreateOrgResult,
  DeeplinkConfig,
  Invite,
  LinkEditInput,
  LinkPage,
  Member,
  MeResult,
  LinkStatsReport,
  OrgDomain,
  RuleInput,
  ShortURL,
  ShortenInput,
  TargetRule,
  TokenPair,
  UniqueClicksResult,
  UTMAnalysis,
} from "./types";

const BASE: string = import.meta.env.VITE_API_URL ?? "";

const TOKENS_KEY = "ogtr.tokens";

/**
 * Machine-readable error code sent when a resource-creating action is blocked
 * by the deployment's limits policy (HTTP 403). The response message is meant
 * to be shown to the user verbatim.
 */
export const LIMIT_REACHED = "LIMIT_REACHED";

export class ApiError extends Error {
  readonly status: number;
  /** Machine-readable code from the error envelope (e.g. LIMIT_REACHED), if any. */
  readonly code?: string;

  constructor(status: number, message: string, code?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

// ---------------------------------------------------------------------------
// Token storage (localStorage)
// ---------------------------------------------------------------------------

export function getTokens(): TokenPair | null {
  const raw = localStorage.getItem(TOKENS_KEY);
  if (!raw) return null;

  try {
    const parsed = JSON.parse(raw) as Partial<TokenPair>;
    if (
      typeof parsed.access_token === "string" &&
      typeof parsed.refresh_token === "string"
    ) {
      return { access_token: parsed.access_token, refresh_token: parsed.refresh_token };
    }
  } catch {
    // corrupt value: fall through and clear it
  }

  localStorage.removeItem(TOKENS_KEY);
  return null;
}

export function setTokens(pair: TokenPair): void {
  localStorage.setItem(TOKENS_KEY, JSON.stringify(pair));
}

export function clearTokens(): void {
  localStorage.removeItem(TOKENS_KEY);
}

// ---------------------------------------------------------------------------
// Session-expired notification (refresh failed -> app logs out)
// ---------------------------------------------------------------------------

type SessionExpiredListener = () => void;

const sessionExpiredListeners = new Set<SessionExpiredListener>();

/** Subscribe to "session expired" (refresh token rejected). Returns unsubscribe. */
export function onSessionExpired(listener: SessionExpiredListener): () => void {
  sessionExpiredListeners.add(listener);
  return () => sessionExpiredListeners.delete(listener);
}

function emitSessionExpired(): void {
  for (const listener of sessionExpiredListeners) listener();
}

// ---------------------------------------------------------------------------
// Envelope parsing
// ---------------------------------------------------------------------------

async function parseBody(res: Response): Promise<unknown> {
  try {
    return await res.json();
  } catch {
    return null;
  }
}

function errorMessage(body: unknown, fallback: string): string {
  if (body !== null && typeof body === "object" && "error" in body) {
    const err = (body as { error: unknown }).error;
    if (typeof err === "string" && err !== "") return err;
    if (err !== null && typeof err === "object" && "message" in err) {
      const msg = (err as { message: unknown }).message;
      if (typeof msg === "string" && msg !== "") return msg;
    }
  }

  return fallback;
}

/** The envelope's machine-readable {"error": {"code": ...}}, if present. */
function errorCode(body: unknown): string | undefined {
  if (body !== null && typeof body === "object" && "error" in body) {
    const err = (body as { error: unknown }).error;
    if (err !== null && typeof err === "object" && "code" in err) {
      const code = (err as { code: unknown }).code;
      if (typeof code === "string" && code !== "") return code;
    }
  }

  return undefined;
}

// ---------------------------------------------------------------------------
// Single-flight refresh
// ---------------------------------------------------------------------------

let refreshInFlight: Promise<boolean> | null = null;

function refreshSession(): Promise<boolean> {
  refreshInFlight ??= doRefresh().finally(() => {
    refreshInFlight = null;
  });

  return refreshInFlight;
}

async function doRefresh(): Promise<boolean> {
  const tokens = getTokens();
  if (!tokens) return false;

  let res: Response;
  try {
    res = await fetch(`${BASE}/api/v1/auth/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: tokens.refresh_token }),
    });
  } catch {
    // Network failure: keep the tokens; the caller's request fails but the
    // session is not destroyed.
    return false;
  }

  if (!res.ok) {
    clearTokens();
    emitSessionExpired();
    return false;
  }

  const body = await parseBody(res);
  const pair = (body as { data?: Partial<TokenPair> } | null)?.data;

  if (
    typeof pair?.access_token !== "string" ||
    typeof pair.refresh_token !== "string"
  ) {
    clearTokens();
    emitSessionExpired();
    return false;
  }

  setTokens({ access_token: pair.access_token, refresh_token: pair.refresh_token });
  return true;
}

// ---------------------------------------------------------------------------
// Core request
// ---------------------------------------------------------------------------

interface RequestOptions {
  method?: string;
  body?: unknown;
  /** Attach the access token (default true). Login/refresh set false. */
  auth?: boolean;
}

const SESSION_EXPIRED_MSG = "Your session has expired. Please sign in again.";

export async function api<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const { method = "GET", body, auth = true } = opts;

  const doFetch = async (): Promise<Response> => {
    const headers: Record<string, string> = {};

    if (body !== undefined) headers["Content-Type"] = "application/json";

    if (auth) {
      const tokens = getTokens();
      if (tokens) headers.Authorization = `Bearer ${tokens.access_token}`;
    }

    try {
      return await fetch(`${BASE}${path}`, {
        method,
        headers,
        body: body !== undefined ? JSON.stringify(body) : undefined,
      });
    } catch {
      throw new ApiError(0, "Could not reach the server. Check your connection and try again.");
    }
  };

  let res = await doFetch();

  // Access token rejected: refresh once (single-flight) and retry once.
  if (res.status === 401 && auth && getTokens() !== null) {
    const refreshed = await refreshSession();
    if (!refreshed) throw new ApiError(401, SESSION_EXPIRED_MSG);

    res = await doFetch();

    if (res.status === 401) {
      clearTokens();
      emitSessionExpired();
      throw new ApiError(401, SESSION_EXPIRED_MSG);
    }
  }

  const parsed = await parseBody(res);

  if (!res.ok) {
    throw new ApiError(
      res.status,
      errorMessage(parsed, `Request failed (${res.status})`),
      errorCode(parsed),
    );
  }

  return ((parsed as { data?: unknown } | null)?.data ?? null) as T;
}

// ---------------------------------------------------------------------------
// Endpoint helpers (ARCHITECTURE.md §4)
// ---------------------------------------------------------------------------

export const endpoints = {
  /** Which sign-in methods the server offers; drives the login page. */
  authProviders: () => api<AuthProvidersInfo>("/api/v1/auth/providers", { auth: false }),

  googleLogin: (idToken: string) =>
    api<AuthResult>("/api/v1/auth/google", {
      method: "POST",
      body: { id_token: idToken },
      auth: false,
    }),

  /** Dev-provider sign-in — only functional when the server enables "dev". */
  devLogin: (email: string, name: string) =>
    api<AuthResult>("/api/v1/auth/dev", {
      method: "POST",
      body: { email, name },
      auth: false,
    }),

  me: () => api<MeResult>("/api/v1/me"),

  switchOrg: (orgId: number) =>
    api<TokenPair>("/api/v1/auth/switch-org", {
      method: "POST",
      body: { org_id: orgId },
    }),

  createOrg: (name: string, autoJoinDomain = "") =>
    api<CreateOrgResult>("/api/v1/orgs", {
      method: "POST",
      body: { name, auto_join_domain: autoJoinDomain },
    }),

  members: async () => (await api<Member[] | null>("/api/v1/org/members")) ?? [],

  removeMember: (userId: number) =>
    api<{ removed: boolean }>(`/api/v1/org/members/${userId}`, { method: "DELETE" }),

  invites: async () => (await api<Invite[] | null>("/api/v1/org/invites")) ?? [],

  createInvite: (email: string) =>
    api<Invite>("/api/v1/org/invites", { method: "POST", body: { email } }),

  revokeInvite: (inviteId: number) =>
    api<{ revoked: boolean }>(`/api/v1/org/invites/${inviteId}`, { method: "DELETE" }),

  domains: async () => (await api<OrgDomain[] | null>("/api/v1/org/domains")) ?? [],

  /** Registers a hostname (OWNER); the response carries the TXT record to set. */
  createDomain: (hostname: string) =>
    api<OrgDomain>("/api/v1/org/domains", { method: "POST", body: { hostname } }),

  /**
   * Runs the DNS TXT ownership check (OWNER). Resolves with the VERIFIED
   * domain; while DNS does not prove ownership the server answers 409 with a
   * human-readable reason (surface it verbatim, then retry after DNS
   * propagation).
   */
  verifyDomain: (id: number) =>
    api<OrgDomain>(`/api/v1/org/domains/${id}/verify`, { method: "POST" }),

  /** Makes this the org's single primary domain (OWNER; VERIFIED only). */
  setPrimaryDomain: (id: number) =>
    api<OrgDomain>(`/api/v1/org/domains/${id}/primary`, { method: "PUT" }),

  /** Removes a domain (OWNER). Gofr DELETEs respond 204, no body. */
  deleteDomain: (id: number) => api<null>(`/api/v1/org/domains/${id}`, { method: "DELETE" }),

  shorten: (input: ShortenInput) =>
    api<ShortURL>("/api/v1/links", { method: "POST", body: input }),

  links: (page = 1) => api<LinkPage>(`/api/v1/links?page=${page}`),

  link: (id: number) => api<ShortURL>(`/api/v1/links/${id}`),

  setAlias: (id: number, alias: string) =>
    api<ShortURL>(`/api/v1/links/${id}/alias`, { method: "PUT", body: { alias } }),

  /**
   * Repoints a link at a new destination (creator or org OWNER; others 403).
   * Validation matches creation; every applied edit is audited server-side.
   */
  editLink: (id: number, input: LinkEditInput) =>
    api<ShortURL>(`/api/v1/links/${id}`, { method: "PATCH", body: input }),

  /** Replaces the deep-link config; pass null to clear it. */
  setDeeplink: (id: number, config: DeeplinkConfig | null) =>
    api<ShortURL>(`/api/v1/links/${id}/deeplink`, { method: "PUT", body: config }),

  rules: async (linkId: number) =>
    (await api<TargetRule[] | null>(`/api/v1/links/${linkId}/rules`)) ?? [],

  createRules: (linkId: number, rules: RuleInput[]) =>
    api<TargetRule[]>(`/api/v1/links/${linkId}/rules`, { method: "POST", body: rules }),

  updateRule: (ruleId: number, rule: RuleInput) =>
    api<TargetRule>(`/api/v1/rules/${ruleId}`, { method: "PUT", body: rule }),

  deleteRule: (ruleId: number) =>
    api<{ deleted: boolean }>(`/api/v1/rules/${ruleId}`, { method: "DELETE" }),

  /**
   * Full per-link analytics report. Dates are YYYY-MM-DD (both inclusive);
   * empty strings default to the last month on the server. A bad range is an
   * ApiError 400.
   */
  linkStats: (id: number, from = "", to = "") => {
    const params = new URLSearchParams();
    if (from) params.set("from", from);
    if (to) params.set("to", to);

    const qs = params.toString();

    return api<LinkStatsReport>(`/api/v1/links/${id}/stats${qs ? `?${qs}` : ""}`);
  },

  uniqueClicks: (linkIds: number[]) =>
    api<UniqueClicksResult>(`/api/v1/stats/unique-clicks?link_ids=${linkIds.join(",")}`),

  statsTags: async () => (await api<string[] | null>("/api/v1/stats/tags")) ?? [],

  utmAnalysis: () => api<UTMAnalysis>("/api/v1/stats/utm"),

  apiKeys: async () => (await api<ApiKey[] | null>("/api/v1/api-keys")) ?? [],

  /** The response's `key` is the plaintext — shown once, never retrievable again. */
  createApiKey: (name: string) =>
    api<CreatedApiKey>("/api/v1/api-keys", { method: "POST", body: { name } }),

  /** Disables (never hard-deletes) a key. Gofr DELETEs respond 204, no body. */
  disableApiKey: (id: number) => api<null>(`/api/v1/api-keys/${id}`, { method: "DELETE" }),

  /** City-name autocomplete; throws ApiError 501 when the deployment has no dataset. */
  cities: async (q: string) =>
    (await api<string[] | null>(`/api/v1/cities?q=${encodeURIComponent(q)}`)) ?? [],
};

/**
 * Fetches a link's QR PNG (auth header attached — an <img src> can't send it)
 * and returns an object URL. Callers must revoke it when done. Retries once
 * through the shared refresh flow on a stale access token.
 */
export async function fetchQRObjectURL(linkId: number): Promise<string> {
  const doFetch = () => {
    const tokens = getTokens();

    return fetch(`${BASE}/api/v1/links/${linkId}/qr`, {
      headers: tokens ? { Authorization: `Bearer ${tokens.access_token}` } : {},
    });
  };

  let res = await doFetch();

  if (res.status === 401 && getTokens() !== null) {
    const refreshed = await refreshSession();
    if (!refreshed) throw new ApiError(401, SESSION_EXPIRED_MSG);

    res = await doFetch();
  }

  if (!res.ok) throw new ApiError(res.status, `Could not load the QR code (${res.status}).`);

  return URL.createObjectURL(await res.blob());
}
