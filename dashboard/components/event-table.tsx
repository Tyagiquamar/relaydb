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
  insert: 'border-emerald-400/20 bg-emerald-400/10 text-emerald-200',
  update: 'border-cyan-400/20 bg-cyan-400/10 text-cyan-200',
  delete: 'border-rose-400/20 bg-rose-400/10 text-rose-200',
}

export function EventTable({ events, loading }: { events: EventRecord[]; loading: boolean }) {
  if (loading) {
    return <div className="h-72 animate-pulse rounded-lg border border-[#263246] bg-[#101827]" aria-label="Loading event stream" />
  }

  if (events.length === 0) {
    return (
      <div className="flex min-h-72 flex-col items-center justify-center rounded-lg border border-dashed border-[#34445e] bg-[#101827]/70 px-4 text-center">
        <DatabaseZap aria-hidden="true" className="h-6 w-6 text-cyan-300" />
        <p className="mt-3 text-sm font-semibold text-slate-200">No captured events yet</p>
        <p className="mt-1 max-w-sm text-sm text-slate-500">Start the demo commerce service or write to a published source table to populate this stream.</p>
      </div>
    )
  }

  return (
    <div className="overflow-hidden rounded-lg border border-[#263246] bg-[#101827]">
      <div className="hidden grid-cols-[minmax(150px,1.1fr)_minmax(140px,1fr)_96px_110px] gap-4 border-b border-[#263246] px-4 py-3 text-[10px] font-semibold uppercase tracking-[0.15em] text-slate-500 md:grid">
        <span>Timestamp</span>
        <span>Relation</span>
        <span>Operation</span>
        <span>LSN</span>
      </div>
      <div className="divide-y divide-[#1d293a]">
        {events.map((event) => (
          <article className="grid gap-2 px-4 py-3 md:grid-cols-[minmax(150px,1.1fr)_minmax(140px,1fr)_96px_110px] md:items-center md:gap-4" key={event.id}>
            <time className="font-data text-xs text-slate-400" dateTime={event.created_at}>{new Date(event.created_at).toLocaleString()}</time>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium text-slate-100">{event.schema_name}<span className="text-slate-500">.</span>{event.table_name}</p>
              <p className="mt-0.5 truncate font-data text-[11px] text-slate-600 md:hidden">{event.commit_end_lsn} · {event.id.slice(0, 12)}</p>
            </div>
            <span className={`w-fit rounded border px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.1em] ${operationStyles[event.operation]}`}>{event.operation}</span>
            <span className="hidden font-data text-xs text-slate-400 md:block">{event.commit_end_lsn}</span>
          </article>
        ))}
      </div>
    </div>
  )
}