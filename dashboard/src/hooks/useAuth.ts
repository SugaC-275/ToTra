import { useState, useCallback } from "react";
import { login } from "../api/client";

export function useAuth() {
  const [token, setToken] = useState<string | null>(
    localStorage.getItem("totra_token")
  );
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const signIn = useCallback(async (email: string, password: string) => {
    setLoading(true);
    setError(null);
    try {
      const { data } = await login(email, password);
      localStorage.setItem("totra_token", data.token);
      setToken(data.token);
      return true;
    } catch {
      setError("Invalid credentials");
      return false;
    } finally {
      setLoading(false);
    }
  }, []);

  const signOut = useCallback(() => {
    localStorage.removeItem("totra_token");
    setToken(null);
  }, []);

  return { token, isAuthenticated: !!token, signIn, signOut, error, loading };
}
