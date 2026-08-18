'use client'

import { useEffect, useState } from 'react'
import { CircleAlert, ShieldAlert } from 'lucide-react'

interface DLQEntry {
  id: number
  event_id: string
  source_id: string
  failure_reason: string
  status: string
  created_at: string
}

export default function DLQPage() {
  const [entries, setEntries] = useState<DLQEntry[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    async function loadDLQ() {
      try {
        const response = await fetch('/api/v1/dlq', { cache: 'no-store' })
        if (!response.ok) throw new Error('Unable to load dead letters')
        const data = await response.json() as { entries: DLQEntry[] | null }
        setEntries(data.entries ?? [])
        setError('')
      } catch (loadError) {
        setError(loadError instanceof Error ? loadError.message : 'Unable to load dead letters')
      }
    }

    void loadDLQ()
  }, [])

  return (
    <div className="mx-auto max-w-[1500px] space-y-5">
      <header className="border-b border-[#263246] pb-5">
        <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-rose-300">
          <ShieldAlert aria-hidden="true" className="h-3.5 w-3.5" />
          Failure handling
        </div>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight text-slate-50">Dead letter queue</h1>
        <p className="mt-1 text-sm text-slate-500">Events that exhausted delivery attempts and require operator review</p>
      </header>

      {error && <p className="flex items-center gap-2 text-sm text-amber-200"><CircleAlert aria-hidden="true" className="h-4 w-4" />{error}</p>}

      <div className="overflow-hidden rounded-lg border border-[#263246] bg-[#101827]">
        {entries.length === 0 ? (
          <div className="flex min-h-72 flex-col items-center justify-center text-center">
            <ShieldAlert aria-hidden="true" className="h-7 w-7 text-cyan-300" />
            <p className="mt-3 text-sm font-semibold text-slate-200">Queue is clear</p>
            <p className="mt-1 text-sm text-slate-500">No events are awaiting manual intervention.</p>
          </div>
        ) : (
          <div className="divide-y divide-[#1d293a]">
            {entries.map((entry) => (
              <article className="grid gap-3 px-5 py-4 md:grid-cols-[90px_minmax(160px,1fr)_140px] md:items-center" key={entry.id}>
                <span className="font-data text-xs text-slate-500">#{entry.id}</span>
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-rose-100">{entry.failure_reason}</p>
                  <p className="mt-1 truncate font-data text-[11px] text-slate-600">{entry.source_id} · {entry.event_id}</p>
                </div>
                <div className="flex items-center justify-between gap-3 md:block">
                  <span className={`rounded border px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.1em] ${entry.status === 'pending' ? 'border-amber-400/20 bg-amber-400/10 text-amber-200' : 'border-slate-500/20 bg-slate-500/10 text-slate-300'}`}>{entry.status}</span>
                  <time className="mt-0 text-xs text-slate-600 md:mt-2" dateTime={entry.created_at}>{new Date(entry.created_at).toLocaleString()}</time>
                </div>
              </article>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}