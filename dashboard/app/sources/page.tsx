'use client'

import { useEffect, useState } from 'react'
import { CircleAlert, Database, Server } from 'lucide-react'
import { useSearchParams } from 'next/navigation'
import { getDashboardData, resolveMode, SourceRecord } from '../../lib/dashboard-data'
import { SiteFooter } from '../../components/site-footer'

export default function SourcesPage() {
  const searchParams = useSearchParams()
  const mode = resolveMode(searchParams.get('mode'))
  const [sources, setSources] = useState<SourceRecord[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    async function loadSources() {
      try {
        const data = await getDashboardData(mode)
        setSources(data.sources)
        setError('')
      } catch (loadError) {
        setError(loadError instanceof Error ? loadError.message : 'Unable to load sources')
      }
    }

    void loadSources()
  }, [mode])

  return (
    <div className="mx-auto max-w-[1500px] space-y-5">
      <header className="flex items-end justify-between border-b border-seam pb-5">
        <div>
          <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-petrol">
            <Database aria-hidden="true" className="h-3.5 w-3.5" />
            Capture topology
          </div>
          <h1 className="mt-2 font-display text-2xl tracking-tight text-ink md:text-3xl">Sources</h1>
            <p className="mt-1 text-sm text-soft">{mode === 'demo' ? 'Deterministic publisher and slot evidence' : 'Registered PostgreSQL publishers and replication slots'}</p>
        </div>
        <span className="hidden rounded-sm border border-seam-strong px-3 py-2 text-xs text-faint sm:block">Registration via API</span>
      </header>

      {error && <p className="flex items-center gap-2 text-sm text-amber-800"><CircleAlert aria-hidden="true" className="h-4 w-4" />{error}</p>}

      <div className="overflow-hidden rounded-md border border-seam bg-surface">
        <div className="hidden grid-cols-[minmax(180px,1.3fr)_120px_minmax(180px,1fr)_140px] gap-4 border-b border-seam px-5 py-3 text-[10px] font-semibold uppercase tracking-[0.15em] text-faint md:grid">
          <span>Source</span><span>State</span><span>Replication slot</span><span>Registered</span>
        </div>
        {sources.length === 0 ? (
          <div className="flex min-h-64 flex-col items-center justify-center text-center">
            <Server aria-hidden="true" className="h-6 w-6 text-faint" />
            <p className="mt-3 text-sm font-semibold text-body">No sources registered</p>
            <p className="mt-1 text-sm text-soft">Create a source through the RelayDB API to begin capture.</p>
          </div>
        ) : sources.map((source) => (
          <article className="grid gap-2 border-b border-seam px-5 py-4 last:border-0 transition-colors hover:bg-raised/60 md:grid-cols-[minmax(180px,1.3fr)_120px_minmax(180px,1fr)_140px] md:items-center md:gap-4" key={source.id}>
            <div><p className="text-sm font-semibold text-ink">{source.name}</p><p className="mt-1 font-data text-[11px] text-faint md:hidden">{source.id}</p></div>
            <StatusBadge status={source.status} />
            <p className="font-data text-xs text-soft">{source.replication_slot}</p>
            <time className="text-xs text-faint" dateTime={source.created_at}>{new Date(source.created_at).toLocaleDateString()}</time>
          </article>
        ))}
      </div>
      <SiteFooter />
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    streaming: 'border-petrol/25 bg-petrol/10 text-petrol-deep',
    error: 'border-rose-500/25 bg-rose-500/10 text-rose-800',
    paused: 'border-amber-500/30 bg-amber-400/10 text-amber-800',
    registered: 'border-seam-strong bg-raised text-soft',
  }

  return (
    <span className={`w-fit rounded-sm border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.1em] ${colors[status] || colors.registered}`}>
      {status}
    </span>
  )
}
