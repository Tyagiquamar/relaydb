'use client'

import { useSearchParams } from 'next/navigation'
import { ListRestart } from 'lucide-react'
import { demoReplayData, resolveMode } from '../../lib/dashboard-data'

export default function ReplaysPage() {
  const searchParams = useSearchParams()
  const mode = resolveMode(searchParams.get('mode'))
  const replays = demoReplayData()

  return (
    <div className="mx-auto max-w-[1500px] space-y-5">
      <header className="border-b border-[#263246] pb-5">
        <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-cyan-300">
          <ListRestart aria-hidden="true" className="h-3.5 w-3.5" />
          Historical processing
        </div>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight text-slate-50">Replay sessions</h1>
        <p className="mt-1 text-sm text-slate-500">{mode === 'demo' ? 'Deterministic sessions show cursor and destination state' : 'Reprocess retained events by position, time range, or destination'}</p>
      </header>
      {mode === 'live' ? <Unavailable /> : <div className="overflow-hidden rounded-lg border border-[#263246] bg-[#101827]">
        <div className="hidden grid-cols-[minmax(200px,1.2fr)_120px_120px_minmax(120px,1fr)_minmax(140px,1fr)] gap-4 border-b border-[#263246] px-5 py-3 text-[10px] font-semibold uppercase tracking-[0.15em] text-slate-500 md:grid"><span>Session</span><span>Destination</span><span>State</span><span>Progress</span><span>Cursor</span></div>
        <div className="divide-y divide-[#1d293a]">{replays.map((replay) => <article className="grid gap-2 px-5 py-4 md:grid-cols-[minmax(200px,1.2fr)_120px_120px_minmax(120px,1fr)_minmax(140px,1fr)] md:items-center md:gap-4" key={replay.id}><div><p className="text-sm font-semibold text-slate-100">{replay.name}</p><p className="mt-1 font-data text-[11px] text-slate-600">{replay.id}</p></div><p className="text-xs text-slate-400">{replay.destination}</p><span className={`w-fit rounded border px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.1em] ${replay.status === 'running' ? 'border-cyan-400/20 bg-cyan-400/10 text-cyan-200' : 'border-emerald-400/20 bg-emerald-400/10 text-emerald-200'}`}>{replay.status}</span><p className="font-data text-xs text-slate-300">{replay.processed.toLocaleString()} / {replay.total.toLocaleString()}</p><p className="font-data text-xs text-slate-400">{replay.cursor}</p></article>)}</div>
      </div>}
    </div>
  )
}

function Unavailable() {
  return <div className="flex min-h-72 flex-col items-center justify-center rounded-lg border border-dashed border-[#34445e] bg-[#101827]/70 px-4 text-center"><p className="text-sm font-semibold text-slate-200">Replay execution is not available yet</p><p className="mt-1 max-w-md text-sm text-slate-500">Session storage exists in RelayDB, but cursor execution and destination routing still need implementation.</p></div>
}