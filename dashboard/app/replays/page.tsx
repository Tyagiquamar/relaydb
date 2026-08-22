'use client'

import { useSearchParams } from 'next/navigation'
import { ListRestart } from 'lucide-react'
import { demoReplayData, resolveMode } from '../../lib/dashboard-data'
import { SiteFooter } from '../../components/site-footer'

export default function ReplaysPage() {
  const searchParams = useSearchParams()
  const mode = resolveMode(searchParams.get('mode'))
  const replays = demoReplayData()

  return (
    <div className="mx-auto max-w-[1500px] space-y-5">
      <header className="border-b border-seam pb-5">
        <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-petrol">
          <ListRestart aria-hidden="true" className="h-3.5 w-3.5" />
          Historical processing
        </div>
        <h1 className="mt-2 font-display text-2xl tracking-tight text-ink md:text-3xl">Replay sessions</h1>
        <p className="mt-1 text-sm text-soft">{mode === 'demo' ? 'Deterministic sessions show cursor and destination state' : 'Reprocess retained events by position, time range, or destination'}</p>
      </header>
      {mode === 'live' ? <Unavailable /> : <div className="overflow-hidden rounded-md border border-seam bg-surface">
        <div className="hidden grid-cols-[minmax(200px,1.2fr)_120px_120px_minmax(120px,1fr)_minmax(140px,1fr)] gap-4 border-b border-seam px-5 py-3 text-[10px] font-semibold uppercase tracking-[0.15em] text-faint md:grid"><span>Session</span><span>Destination</span><span>State</span><span>Progress</span><span>Cursor</span></div>
        <div className="divide-y divide-seam">{replays.map((replay) => <article className="grid gap-2 px-5 py-4 transition-colors hover:bg-raised/60 md:grid-cols-[minmax(200px,1.2fr)_120px_120px_minmax(120px,1fr)_minmax(140px,1fr)] md:items-center md:gap-4" key={replay.id}><div><p className="text-sm font-semibold text-ink">{replay.name}</p><p className="mt-1 font-data text-[11px] text-faint">{replay.id}</p></div><p className="text-xs text-soft">{replay.destination}</p><span className={`w-fit rounded-sm border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.1em] ${replay.status === 'running' ? 'border-petrol/25 bg-petrol/10 text-petrol-deep' : 'border-emerald-600/25 bg-emerald-500/10 text-emerald-800'}`}>{replay.status}</span><p className="font-data text-xs text-body">{replay.processed.toLocaleString()} / {replay.total.toLocaleString()}</p><p className="font-data text-xs text-soft">{replay.cursor}</p></article>)}</div>
      </div>}
      <SiteFooter />
    </div>
  )
}

function Unavailable() {
  return <div className="flex min-h-72 flex-col items-center justify-center rounded-md border border-dashed border-seam-strong bg-surface/70 px-4 text-center"><p className="text-sm font-semibold text-body">Replay execution is not available yet</p><p className="mt-1 max-w-md text-sm text-soft">Session storage exists in RelayDB, but cursor execution and destination routing still need implementation.</p></div>
}
