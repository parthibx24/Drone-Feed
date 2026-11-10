import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { api } from "../services/api";
import { AUTH_EXPIRED_EVENT } from "../services/http";
import type { Me } from "../types/gateway";

interface AuthState {
  /** undefined while checking the session, null when logged out. */
  me: Me | null | undefined;
  canOperate: boolean;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null | undefined>(undefined);

  useEffect(() => {
    api.me().then(setMe).catch(() => setMe(null));
    const expired = () => setMe(null);
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);
    return () => window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
  }, []);

  const login = useCallback(async (username: string, password: string) => {
    await api.login(username, password);
    setMe(await api.me());
  }, []);

  const logout = useCallback(async () => {
    await api.logout().catch(() => {});
    setMe(null);
  }, []);

  const value = useMemo(
    () => ({ me, canOperate: me?.role === "operator", login, logout }),
    [me, login, logout],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth outside AuthProvider");
  return ctx;
}
