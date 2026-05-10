import { Link, useLocation, Outlet, useNavigate } from "react-router-dom";
import { cn } from "../lib/utils";

function getRole(): string {
  const token = localStorage.getItem("totra_token");
  if (!token) return "";
  try {
    const payload = JSON.parse(atob(token.split(".")[1]));
    return payload.role ?? "";
  } catch {
    return "";
  }
}

const adminNavItems = [
  { label: "Dashboard", href: "/admin/dashboard" },
  { label: "Employees", href: "/admin/users" },
  { label: "Models", href: "/admin/models" },
  { label: "Quota Requests", href: "/admin/quota" },
  { label: "KPI", href: "/admin/kpi" },
  { label: "Integrations", href: "/admin/integrations" },
  { label: "My Usage", href: "/me" },
];

const employeeNavItems = [
  { label: "My Usage", href: "/me" },
  { label: "Quota Requests", href: "/admin/quota" },
];

export function Layout() {
  const location = useLocation();
  const navigate = useNavigate();
  const role = getRole();
  const isAdmin = role === "admin";
  const navItems = isAdmin ? adminNavItems : employeeNavItems;

  function handleLogout() {
    localStorage.removeItem("totra_token");
    navigate("/login");
  }

  return (
    <div className="flex min-h-screen bg-zinc-950 text-zinc-100">
      <aside className="w-56 border-r border-zinc-800 flex flex-col py-6 px-4 gap-1 shrink-0">
        <div className="mb-6 px-2">
          <span className="font-bold text-lg text-indigo-400">ToTra</span>
        </div>
        {navItems.map((item) => (
          <Link
            key={item.href}
            to={item.href}
            className={cn(
              "px-3 py-2 rounded-md text-sm font-medium transition-colors",
              location.pathname === item.href
                ? "bg-indigo-600 text-white"
                : "text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800"
            )}
          >
            {item.label}
          </Link>
        ))}
        <div className="mt-auto">
          <button
            onClick={handleLogout}
            className="w-full px-3 py-2 rounded-md text-sm font-medium text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800 text-left transition-colors"
          >
            Sign Out
          </button>
        </div>
      </aside>
      <main className="flex-1 p-8 overflow-auto">
        <Outlet />
      </main>
    </div>
  );
}
