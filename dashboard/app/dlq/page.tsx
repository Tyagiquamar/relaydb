'use client'

import { useEffect, useState } from 'react'
import { CircleAlert, ShieldAlert } from 'lucide-react'
import { useSearchParams } from 'next/navigation'
import { DLQRecord, getDashboardData, resolveMode } from '../../lib/dashboard-data'
import { SiteFooter } from '../../components/site-footer'

export default function DLQPage() {
  const searchParams = useSearchParams()
  const mode = resolveMode(searchParams.get('mode'))
  const [entries, setEntries] = useState<DLQRecord[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    async function loadDLQ() {
      try {
        const data = await getDashboardData(mode)
        setEntries(data.dlqEntries)
        setError('')
      } catch (loadError) {
        setError(loadError instanceof Error ? loadError.message : 'Unable to load dead letters')
      }
    }

    void loadDLQ()
    const interval = window.setInterval(() => void loadDLQ(), 15_000)
    return () => window.clearInterval(interval)
  }, [mode])

  return (
    <div className="mx-auto max-w-[1500px] space-y-5">
      <header className="border-b border-seam pb-5">
        <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-rose-700">
          <ShieldAlert aria-hidden="true" className="h-3.5 w-3.5" />
          Failure handling
        </div>
        <h1 className="mt-2 font-display text-2xl tracking-tight text-ink md:text-3xl">Dead letter queue</h1>
        <p className="mt-1 text-sm text-soft">{mode === 'demo' ? 'Representative exhausted delivery for reliability review' : 'Events that exhausted delivery attempts and require operator review'}</p>
      </header>

      {error && <p className="flex items-center gap-2 text-sm text-amber-800"><CircleAlert aria-hidden="true" className="h-4 w-4" />{error}</p>}

      <div className="overflow-hidden rounded-md border border-seam bg-surface">
        {entries.length === 0 ? (
          <div className="flex min-h-72 flex-col items-center justify-center text-center">
            <ShieldAlert aria-hidden="true" className="h-7 w-7 text-petrol" />
            <p className="mt-3 text-sm font-semibold text-body">Queue is clear</p>
            <p className="mt-1 text-sm text-soft">No events are awaiting manual intervention.</p>
          </div>
        ) : (
          <div className="divide-y divide-seam">
            {entries.map((entry) => (
              <article className="grid gap-3 px-5 py-4 transition-colors hover:bg-raised/60 md:grid-cols-[90px_minmax(160px,1fr)_140px] md:items-center" key={entry.id}>
                <span className="font-data text-xs text-faint">#{entry.id}</span>
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-rose-900">{entry.failure_reason}</p>
                  <p className="mt-1 truncate font-data text-[11px] text-faint">{entry.source_id} · {entry.event_id}</p>
                </div>
                <div className="flex items-center justify-between gap-3 md:block">
                  <span className={`rounded-sm border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.1em] ${entry.status === 'pending' ? 'border-amber-500/30 bg-amber-400/10 text-amber-800' : 'border-seam-strong bg-raised text-soft'}`}>{entry.status}</span>
                  <time className="mt-0 font-data text-xs text-faint md:mt-2" dateTime={entry.created_at}>{new Date(entry.created_at).toLocaleString()}</time>
                </div>
              </article>
            ))}
          </div>
        )}
      </div>
      <SiteFooter />
    </div>
  )
}
