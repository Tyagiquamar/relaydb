# RelayDB Dense Operations Console Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the generic RelayDB dashboard with a responsive CDC operations console driven by the existing BFF API routes.

**Architecture:** Keep each page as a client-side consumer of `/api/v1/*`, but move shared navigation and operational status framing into reusable dashboard components. The Overview page owns its polling state and renders a dense metric strip, event table, and capture health panel from existing stats/events/sources data.

**Tech Stack:** Next.js 15 App Router, React 19, TypeScript, Tailwind CSS, Lucide React, existing RelayDB BFF routes.

## Global Constraints

- Use existing `/api/v1/stats`, `/api/v1/events`, `/api/v1/sources`, and `/api/v1/dlq` endpoints; do not change API payloads.
- Retain server-side BFF authentication; never place API keys in browser code.
- Use Lucide React icons rather than custom SVGs.
- Use a dense, responsive operator-console layout; avoid hero sections and oversized cards.
- Auto-refresh Overview data every 10 seconds and retain last successful values on failure.

---

### Task 1: Build the operations shell

**Files:**
- Create: `dashboard/components/operations-sidebar.tsx`
- Modify: `dashboard/app/layout.tsx`
- Modify: `dashboard/app/globals.css`

**Interfaces:**
- Produces: `OperationsSidebar`, a navigation component using `usePathname()` and links for Overview, Sources, Events, Consumers, Replays, and DLQ.
- Consumes: Next `usePathname`, Lucide icons, and Tailwind utilities.

- [ ] **Step 1: Create the sidebar component**

```tsx
'use client'

export function OperationsSidebar() {
  const pathname = usePathname()
  return <aside aria-label="Primary navigation">...</aside>
}
```

Include a product mark, source-health chip, icon-and-label links, and a compact mobile navigation row. Set an active state from `pathname`.

- [ ] **Step 2: Replace the inline layout navigation**

```tsx
<body className="min-h-screen bg-[#080d16] text-slate-100">
  <OperationsSidebar />
  <main className="min-w-0 flex-1 px-4 py-5 lg:px-8 lg:py-7">{children}</main>
</body>
```

Use a `lg:flex` application shell. The sidebar must not overlap content on desktop or mobile.

- [ ] **Step 3: Add global console tokens**

Define `:root` colors for canvas, panel, border, cyan, amber, and red in `app/globals.css`; retain Tailwind base/components/utilities. Add a restrained grid texture to the canvas and selection styling.

- [ ] **Step 4: Build the dashboard**

Run: `pnpm --dir dashboard exec next build`

Expected: exit code 0 and the dashboard route list includes `/` and `/api/v1/[...path]`.

- [ ] **Step 5: Commit**

```powershell
git add dashboard/components/operations-sidebar.tsx dashboard/app/layout.tsx dashboard/app/globals.css
git commit -m "feat(dashboard): add operations console shell"
```

### Task 2: Implement the live overview

**Files:**
- Create: `dashboard/components/metric-strip.tsx`
- Create: `dashboard/components/event-table.tsx`
- Modify: `dashboard/app/page.tsx`

**Interfaces:**
- Produces: `MetricStrip({ stats }: { stats: DashboardStats })` and `EventTable({ events, loading }: { events: EventRecord[]; loading: boolean })`.
- Consumes: existing `/api/v1/stats`, `/api/v1/events?limit=12`, and `/api/v1/sources` JSON responses.

- [ ] **Step 1: Define dashboard response types and fetch behavior**

```tsx
type DashboardStats = {
  sources: number
  eventsPerSecond: number
  captureLag: string
  consumers: number
  dlqDepth: number
}

type EventRecord = {
  id: string
  schema_name: string
  table_name: string
  operation: 'insert' | 'update' | 'delete'
  commit_end_lsn: string
  created_at: string
}
```

Fetch all three endpoints in `Promise.all`, preserve existing state in `catch`, expose an `error` string, and use a ten-second `setInterval` cleaned up by the effect return callback.

- [ ] **Step 2: Create the compact metric strip**

Render five fixed-height metric cells with Lucide icons and visual severity: cyan for healthy sources, amber for capture lag, red for nonzero DLQ. Use responsive grid columns of two on mobile and five on desktop.

- [ ] **Step 3: Create the event table**

Render timestamp, table, operation badge, LSN, and truncated event ID. Render a fixed-height skeleton while loading and a no-events state with a database icon. Ensure tables turn into vertically spaced row blocks below the `md` breakpoint.

- [ ] **Step 4: Compose the overview**

Add source connection state, `Refresh` button, last-updated timestamp, metric strip, event stream table, and capture-health panel. The capture-health panel derives state from `captureLag`, source count, and `dlqDepth`; it must show the request error without replacing last-known metrics.

- [ ] **Step 5: Build and smoke test**

Run:

```powershell
docker compose build dashboard
docker compose up -d --force-recreate dashboard
curl.exe -s http://localhost:3000/api/v1/stats
```

Expected: dashboard build succeeds and the BFF returns a stats JSON object.

- [ ] **Step 6: Commit**

```powershell
git add dashboard/components/metric-strip.tsx dashboard/components/event-table.tsx dashboard/app/page.tsx
git commit -m "feat(dashboard): add live CDC overview"
```

### Task 3: Apply shared table and state treatment

**Files:**
- Modify: `dashboard/app/sources/page.tsx`
- Modify: `dashboard/app/events/page.tsx`
- Modify: `dashboard/app/dlq/page.tsx`

**Interfaces:**
- Consumes: existing page endpoint response shapes.
- Produces: visually consistent responsive operational tables with loading, empty, and error states.

- [ ] **Step 1: Replace generic card wrappers**

Use `border-[#263246] bg-[#101827]` panels, compact headers, and fixed table column widths. Use existing request fields; do not introduce new endpoint calls.

- [ ] **Step 2: Add visible request failure state**

For each page, track a request error string. Leave existing loaded rows in place if a later request fails and show a compact amber status line below the heading.

- [ ] **Step 3: Add operation and source state badges**

Use semantic colors for insert/update/delete and registered/streaming/error source state. Icons must have `aria-hidden` when their adjacent text conveys the meaning.

- [ ] **Step 4: Build and browser verify**

Run `pnpm --dir dashboard exec next build`, then use the local dashboard to inspect Overview, Sources, Events, and DLQ at desktop and mobile widths. Confirm no navigation clipping or horizontal page overflow.

- [ ] **Step 5: Commit**

```powershell
git add dashboard/app/sources/page.tsx dashboard/app/events/page.tsx dashboard/app/dlq/page.tsx
git commit -m "feat(dashboard): unify operational data views"
```

## Self-Review

- Spec coverage: Task 1 implements responsive navigation and the visual system; Task 2 implements live overview polling, metrics, events, and health; Task 3 applies responsive stateful tables to the remaining active operational pages.
- Placeholder scan: no unresolved requirements or deferred implementation labels remain.
- Type consistency: `DashboardStats` and `EventRecord` are defined in Task 2 and consumed only by the components created in that task.