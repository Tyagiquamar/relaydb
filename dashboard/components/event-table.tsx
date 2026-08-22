import { DatabaseZap } from 'lucide-react'

export type EventRecord = {
  id: string
  schema_name: string
  table_name: string
  operation: 'insert' | 'update' | 'delete'
  commit_end_lsn: string
  created_at: string
}

const operationStyles: Record<EventRecord['operation'], string> = {
  insert: 'border-emerald-600/25 bg-emerald-500/10 text-emerald-800',
  update: 'border-petrol/25 bg-petrol/10 text-petrol-deep',
  delete: 'border-rose-500/25 bg-rose-500/10 text-rose-800',
}

export function EventTable({ events, loading }: { events: EventRecord[]; loading: boolean }) {
  if (loading) {
    return <div className="h-72 animate-pulse rounded-md border border-seam bg-surface" aria-label="Loading event stream" />
  }

  if (events.length === 0) {
    return (
      <div className="flex min-h-72 flex-col items-center justify-center rounded-md border border-dashed border-seam-strong bg-surface/70 px-4 text-center">
        <DatabaseZap aria-hidden="true" className="h-6 w-6 text-petrol" />
        <p className="mt-3 text-sm font-semibold text-body">No captured events yet</p>
        <p className="mt-1 max-w-sm text-sm text-soft">Start the demo commerce service or write to a published source table to populate this stream.</p>
      </div>
    )
  }

  return (
    <div className="overflow-hidden rounded-md border border-seam bg-surface">
      <div className="hidden grid-cols-[minmax(150px,1.1fr)_minmax(140px,1fr)_96px_110px] gap-4 border-b border-seam px-4 py-3 text-[10px] font-semibold uppercase tracking-[0.15em] text-faint md:grid">
        <span>Timestamp</span>
        <span>Relation</span>
        <span>Operation</span>
        <span>LSN</span>
      </div>
      <div className="divide-y divide-seam">
        {events.map((event) => (
          <article className="grid gap-2 px-4 py-3 transition-colors hover:bg-raised/60 md:grid-cols-[minmax(150px,1.1fr)_minmax(140px,1fr)_96px_110px] md:items-center md:gap-4" key={event.id}>
            <time className="font-data text-xs text-soft" dateTime={event.created_at}>{new Date(event.created_at).toLocaleString()}</time>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium text-ink">{event.schema_name}<span className="text-faint">.</span>{event.table_name}</p>
              <p className="mt-0.5 truncate font-data text-[11px] text-faint md:hidden">{event.commit_end_lsn} · {event.id.slice(0, 12)}</p>
            </div>
            <span className={`w-fit rounded-sm border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.1em] ${operationStyles[event.operation]}`}>{event.operation}</span>
            <span className="hidden font-data text-xs text-soft md:block">{event.commit_end_lsn}</span>
          </article>
        ))}
      </div>
    </div>
  )
}
