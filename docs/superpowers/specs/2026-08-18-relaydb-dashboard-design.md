# RelayDB Dense Operations Console

## Goal

Replace the generic starter dashboard with a compact, responsive control room
for observing a PostgreSQL CDC pipeline. It must prioritize current capture
state and recent events over decorative content.

## Layout

- A fixed desktop sidebar provides product identity, source state, and primary
  navigation. On narrow screens it becomes a compact horizontal rail.
- The overview has a shallow top bar with source identity, connection state,
  last refresh time, and explicit refresh/demo-order commands.
- A five-column metrics strip shows sources, event rate, capture lag, consumer
  groups, and DLQ depth without tall cards.
- The main area has a live event stream and a capture health/status panel.
- Sources, Events, and DLQ pages share the same compact table treatment.

## Visual System

- Dark graphite surface with off-white type, muted steel borders, cyan for
  healthy/live state, amber for degraded attention, and red for failure/DLQ.
- Use the existing Lucide dependency for navigation and status icons.
- Keep typography practical and information-dense; do not use hero-style type
  or floating card grids.

## Data And States

- Read dashboard data through the existing same-origin `/api/v1/*` BFF routes.
- Auto-refresh overview and event data every ten seconds, with manual refresh.
- A failed request shows a visible degraded state while retaining prior data.
- The demo-order command calls the optional demo-commerce service only when
  `NEXT_PUBLIC_DEMO_API_URL` is configured; otherwise it remains unavailable.

## Verification

- Build the Next dashboard production bundle.
- Run the Compose dashboard with the live API and capture a desktop screenshot.
- Verify the overview and event table consume live `/api/v1/stats` and
  `/api/v1/events` responses.