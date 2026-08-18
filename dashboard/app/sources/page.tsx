'use client'

import { useEffect, useState } from 'react'
import { CircleAlert, Database, Server } from 'lucide-react'

interface Source {
  id: string
  name: string
  status: string
  replication_slot: string
  created_at: string
}

export default function SourcesPage() {
  const [sources, setSources] = useState<Source[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    async function loadSources() {
      try {
        const response = await fetch('/api/v1/sources', { cache: 'no-store' })
        if (!response.ok) throw new Error('Unable to load sources')
        const data = await response.json() as { sources: Source[] | null }
        setSources(data.sources ?? [])
        setError('')
      } catch (loadError) {
        setError(loadError instanceof Error ? loadError.message : 'Unable to load sources')
      }
    }

    void loadSources()
  }, [])

  return (
    <div className="mx-auto max-w-[1500px] space-y-5">
      <header className="flex items-end justify-between border-b border-[#263246] pb-5">
        <div>
          <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-cyan-300">
            <Database aria-hidden="true" className="h-3.5 w-3.5" />
            Capture topology
          </div>
          <h1 className="mt-2 text-2xl font-semibold tracking-tight text-slate-50">Sources</h1>
          <p className="mt-1 text-sm text-slate-500">Registered PostgreSQL publishers and replication slots</p>
        </div>
        <span className="hidden rounded-md border border-[#34445e] px-3 py-2 text-xs text-slate-500 sm:block">Registration via API</span>
      </header>

      {error && <p className="flex items-center gap-2 text-sm text-amber-200"><CircleAlert aria-hidden="true" className="h-4 w-4" />{error}</p>}

      <div className="overflow-hidden rounded-lg border border-[#263246] bg-[#101827]">
        <div className="hidden grid-cols-[minmax(180px,1.3fr)_120px_minmax(180px,1fr)_140px] gap-4 border-b border-[#263246] px-5 py-3 text-[10px] font-semibold uppercase tracking-[0.15em] text-slate-500 md:grid">
          <span>Source</span><span>State</span><span>Replication slot</span><span>Registered</span>
        </div>
        {sources.length === 0 ? (
          <div className="flex min-h-64 flex-col items-center justify-center text-center">
            <Server aria-hidden="true" className="h-6 w-6 text-slate-600" />
            <p className="mt-3 text-sm font-semibold text-slate-300">No sources registered</p>
            <p className="mt-1 text-sm text-slate-500">Create a source through the RelayDB API to begin capture.</p>
          </div>
        ) : sources.map((source) => (
          <article className="grid gap-2 border-b border-[#1d293a] px-5 py-4 last:border-0 md:grid-cols-[minmax(180px,1.3fr)_120px_minmax(180px,1fr)_140px] md:items-center md:gap-4" key={source.id}>
            <div><p className="text-sm font-semibold text-slate-100">{source.name}</p><p className="mt-1 font-data text-[11px] text-slate-600 md:hidden">{source.id}</p></div>
            <StatusBadge status={source.status} />
            <p className="font-data text-xs text-slate-400">{source.replication_slot}</p>
            <time className="text-xs text-slate-500" dateTime={source.created_at}>{new Date(source.created_at).toLocaleDateString()}</time>
          </article>
        ))}
      </div>
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    streaming: 'border-cyan-400/20 bg-cyan-400/10 text-cyan-200',
    error: 'border-rose-400/20 bg-rose-400/10 text-rose-200',
    paused: 'border-amber-400/20 bg-amber-400/10 text-amber-200',
    registered: 'border-slate-500/20 bg-slate-500/10 text-slate-300',
  }
  
  return (
    <span className={`w-fit rounded border px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.1em] ${colors[status] || colors.registered}`}>
      {status}
    </span>
  )
}