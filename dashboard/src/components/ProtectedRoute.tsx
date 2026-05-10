import { Navigate } from "react-router-dom";
import type { ReactNode } from "react";

function getRole(): string {
  const token = localStorage.getItem("totra_token");
  if (!token) return "";
  try {
    return JSON.parse(atob(token.split(".")[1])).role ?? "";
  } catch {
    return "";
  }
}

export function ProtectedRoute({ children, adminOnly }: { children: ReactNode; adminOnly?: boolean }) {
  const token = localStorage.getItem("totra_token");
  if (!token) return <Navigate to="/login" replace />;
  if (adminOnly && getRole() !== "admin") return <Navigate to="/me" replace />;
  return <>{children}</>;
}
