# Dashboard

The ToTra web UI — an admin console for cost analytics, compliance, budgets, agents, and
integrations, plus an employee self-service area. Built with React 19, Vite, Tailwind
CSS 4, TanStack Query, and Recharts.

- **Port:** 3000 (`DASHBOARD_PORT`)
- **Talks to:** the admin service (`VITE_API_URL`, default `http://localhost:8081`)

## Pages

- **Admin** (`src/pages/admin/`) — ~22 pages: dashboard, cost center, compliance,
  models, users, quota, SSO, SIEM, audit log, data retention, procurement, and more.
- **Employee** (`src/pages/employee/`) — `MyUsagePage`, `SelfServicePage`.
- **Auth** — `LoginPage`.

## Authentication

On login the admin service returns a JWT, stored in `localStorage` as `totra_token`.
`api/client.ts` attaches it as a `Bearer` header and clears it on a 401 response.
`components/ProtectedRoute.tsx` gates routes by authentication and (for admin pages)
by the `role` claim decoded from the token.

## Configuration

| Var | Default | Purpose |
|-----|---------|---------|
| `VITE_API_URL` | `http://localhost:8081` | Admin API base URL (dev) |
| `DASHBOARD_PORT` | `3000` | Port the container serves on |

In the Docker image, nginx serves the static build and proxies `/api/` to `admin:8081`
(see `nginx.conf`).

## Scripts

```bash
npm install
npm run dev         # dev server with HMR  (:3000)
npm run build       # type-check + production build
npm run lint        # ESLint
npm run preview     # preview the production build
npm run test:run    # run the test suite once (Vitest)
```

## Project layout

```
src/
  api/         admin API client (client.ts)
  components/  shared UI components, layouts, route guards
  hooks/       custom hooks (auth, …)
  lib/         utilities
  pages/       admin/ + employee/ + LoginPage
  App.tsx      routing
  main.tsx     entry point
```
