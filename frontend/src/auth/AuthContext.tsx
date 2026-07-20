// Auth state for the SPA: who is signed in, their orgs, and the active org.
// Token persistence and refresh live in lib/api.ts; this context holds the
// user-visible session state and the actions that change it.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import {
  clearTokens,
  endpoints,
  getTokens,
  onSessionExpired,
  setTokens,
} from "../lib/api";
import type { AuthResult, MeResult, Org, OrgMembership, Role, User } from "../lib/types";

export type AuthStatus = "loading" | "anonymous" | "authed";

interface AuthState {
  status: AuthStatus;
  user: User | null;
  orgs: OrgMembership[];
  activeOrgId: number;
  /** Role in the active org; "" when org-less. */
  role: Role | "";
}

const ANONYMOUS: AuthState = {
  status: "anonymous",
  user: null,
  orgs: [],
  activeOrgId: 0,
  role: "",
};

interface AuthContextValue extends AuthState {
  /** True when the last logout was forced by a rejected refresh token. */
  sessionExpired: boolean;
  loginWithGoogle: (idToken: string) => Promise<void>;
  /** Microsoft ID token (obtained by the SPA PKCE flow) → ogtr session. */
  loginWithMicrosoft: (idToken: string) => Promise<void>;
  /** Dev-provider sign-in; only works when the server enables "dev". */
  loginWithDev: (email: string, name: string) => Promise<void>;
  createOrg: (name: string) => Promise<Org>;
  switchOrg: (orgId: number) => Promise<void>;
  refreshMe: () => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

function stateFromMe(me: MeResult): AuthState {
  return {
    status: "authed",
    user: me.user,
    orgs: me.orgs ?? [],
    activeOrgId: me.active_org_id,
    role: me.role ?? "",
  };
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ ...ANONYMOUS, status: "loading" });
  const [sessionExpired, setSessionExpired] = useState(false);

  // Bootstrap: a stored token pair (re)hydrates the session via GET /me
  // (the API client refreshes transparently if the access token is stale).
  useEffect(() => {
    let cancelled = false;

    if (getTokens() === null) {
      setState(ANONYMOUS);
      return;
    }

    endpoints
      .me()
      .then((me) => {
        if (!cancelled) setState(stateFromMe(me));
      })
      .catch(() => {
        if (!cancelled) {
          clearTokens();
          setState(ANONYMOUS);
        }
      });

    return () => {
      cancelled = true;
    };
  }, []);

  // Refresh-token rejection anywhere in the app logs the session out; the
  // login page shows a "session expired" notice via the flag.
  useEffect(
    () =>
      onSessionExpired(() => {
        setSessionExpired(true);
        setState(ANONYMOUS);
      }),
    [],
  );

  const refreshMe = useCallback(async () => {
    const me = await endpoints.me();
    setState(stateFromMe(me));
  }, []);

  // Google and dev sign-in return the same AuthResult; everything after the
  // endpoint call is provider-independent.
  const adoptAuthResult = useCallback((result: AuthResult) => {
    setTokens({ access_token: result.access_token, refresh_token: result.refresh_token });
    setSessionExpired(false);

    const orgs = result.orgs ?? [];

    setState({
      status: "authed",
      user: result.user,
      orgs,
      activeOrgId: result.active_org_id,
      role: orgs.find((o) => o.org_id === result.active_org_id)?.role ?? "",
    });
  }, []);

  const loginWithGoogle = useCallback(
    async (idToken: string) => {
      adoptAuthResult(await endpoints.googleLogin(idToken));
    },
    [adoptAuthResult],
  );

  const loginWithMicrosoft = useCallback(
    async (idToken: string) => {
      adoptAuthResult(await endpoints.microsoftLogin(idToken));
    },
    [adoptAuthResult],
  );

  const loginWithDev = useCallback(
    async (email: string, name: string) => {
      adoptAuthResult(await endpoints.devLogin(email, name));
    },
    [adoptAuthResult],
  );

  // POST /orgs returns the org plus a token pair already scoped to it —
  // adopt the pair, then re-read /me for the updated membership list.
  const createOrg = useCallback(
    async (name: string) => {
      const result = await endpoints.createOrg(name);

      setTokens({ access_token: result.access_token, refresh_token: result.refresh_token });
      await refreshMe();

      return result.org;
    },
    [refreshMe],
  );

  const switchOrg = useCallback(
    async (orgId: number) => {
      const pair = await endpoints.switchOrg(orgId);

      setTokens(pair);
      await refreshMe();
    },
    [refreshMe],
  );

  const logout = useCallback(() => {
    clearTokens();
    setSessionExpired(false); // user-initiated: no "expired" notice
    setState(ANONYMOUS);
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      ...state,
      sessionExpired,
      loginWithGoogle,
      loginWithMicrosoft,
      loginWithDev,
      createOrg,
      switchOrg,
      refreshMe,
      logout,
    }),
    [
      state,
      sessionExpired,
      loginWithGoogle,
      loginWithMicrosoft,
      loginWithDev,
      createOrg,
      switchOrg,
      refreshMe,
      logout,
    ],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (ctx === null) throw new Error("useAuth must be used inside <AuthProvider>");

  return ctx;
}
