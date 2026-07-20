// Microsoft sign-in: the SPA half of the OAuth 2.0 authorization-code flow
// with PKCE against the Microsoft identity platform ("common" endpoint, so
// both work/school and personal accounts sign in). Implemented directly with
// WebCrypto + fetch — no MSAL, no external script.
//
// Flow: beginMicrosoftLogin() persists {state, verifier, clientId} in
// sessionStorage and redirects to the authorize endpoint; Microsoft returns
// the browser to /auth/microsoft?code=...&state=..., where the callback page
// validates state (CSRF), posts code+verifier to the token endpoint (which is
// CORS-enabled for apps registered under the SPA platform) and receives an
// ID token. That ID token then goes to POST /api/v1/auth/microsoft exactly
// like a Google credential — the backend verifies it independently; nothing
// in this file is trusted server-side.

const AUTHORIZE_URL = "https://login.microsoftonline.com/common/oauth2/v2.0/authorize";
const TOKEN_URL = "https://login.microsoftonline.com/common/oauth2/v2.0/token";
const SCOPE = "openid profile email";

const PENDING_KEY = "ogtr.ms.pkce";

/** What beginMicrosoftLogin stashes for the callback page (one-shot). */
export interface PendingMicrosoftLogin {
  state: string;
  verifier: string;
  clientId: string;
  /** Where to land after signing in (RequireAuth's original target). */
  from?: string;
}

/** The redirect URI registered in the Azure app (SPA platform). */
export function microsoftRedirectUri(): string {
  return `${window.location.origin}/auth/microsoft`;
}

/** 32 random bytes, base64url — valid as both code_verifier and state. */
function randomToken(): string {
  const bytes = new Uint8Array(32);
  crypto.getRandomValues(bytes);
  return base64url(bytes);
}

function base64url(bytes: Uint8Array): string {
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

/** S256: BASE64URL(SHA-256(verifier)). */
async function challengeFor(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier));
  return base64url(new Uint8Array(digest));
}

/**
 * Starts the sign-in: persists the pending state and navigates the whole
 * page to Microsoft's authorize endpoint. Only returns on failure to start
 * (it otherwise unloads the page).
 */
export async function beginMicrosoftLogin(clientId: string, from?: string): Promise<void> {
  const pending: PendingMicrosoftLogin = {
    state: randomToken(),
    verifier: randomToken(),
    clientId,
    from,
  };

  sessionStorage.setItem(PENDING_KEY, JSON.stringify(pending));

  const params = new URLSearchParams({
    client_id: clientId,
    response_type: "code",
    redirect_uri: microsoftRedirectUri(),
    response_mode: "query",
    scope: SCOPE,
    state: pending.state,
    code_challenge: await challengeFor(pending.verifier),
    code_challenge_method: "S256",
  });

  window.location.assign(`${AUTHORIZE_URL}?${params.toString()}`);
}

/**
 * Retrieves AND clears the pending login (one-shot: an auth code is
 * single-use, and a replayed/duplicated callback must not find a state to
 * match). Null when there is none or it does not parse.
 */
export function takePendingMicrosoftLogin(): PendingMicrosoftLogin | null {
  const raw = sessionStorage.getItem(PENDING_KEY);
  sessionStorage.removeItem(PENDING_KEY);
  if (!raw) return null;

  try {
    const parsed = JSON.parse(raw) as Partial<PendingMicrosoftLogin>;
    if (
      typeof parsed.state === "string" &&
      parsed.state !== "" &&
      typeof parsed.verifier === "string" &&
      typeof parsed.clientId === "string"
    ) {
      return {
        state: parsed.state,
        verifier: parsed.verifier,
        clientId: parsed.clientId,
        from: typeof parsed.from === "string" ? parsed.from : undefined,
      };
    }
  } catch {
    // corrupt value: treated as absent
  }

  return null;
}

/**
 * Exchanges the authorization code for an ID token at Microsoft's token
 * endpoint. Throws with a human-readable message on failure.
 */
export async function redeemMicrosoftCode(
  code: string,
  pending: PendingMicrosoftLogin,
): Promise<string> {
  let res: Response;

  try {
    res = await fetch(TOKEN_URL, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({
        client_id: pending.clientId,
        grant_type: "authorization_code",
        code,
        redirect_uri: microsoftRedirectUri(),
        code_verifier: pending.verifier,
        scope: SCOPE,
      }),
    });
  } catch {
    throw new Error("Could not reach Microsoft to complete sign-in. Check your connection and try again.");
  }

  const body = (await res.json().catch(() => null)) as {
    id_token?: string;
    error?: string;
    error_description?: string;
  } | null;

  if (!res.ok || typeof body?.id_token !== "string" || body.id_token === "") {
    const detail = body?.error_description ?? body?.error;
    throw new Error(
      detail
        ? `Microsoft sign-in failed: ${detail}`
        : "Microsoft sign-in failed at the token exchange. Please try again.",
    );
  }

  return body.id_token;
}
