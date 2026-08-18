'use client'

import { ListRestart, Wrench } from 'lucide-react'

export default function ReplaysPage() {
  return (
    <div className="mx-auto max-w-[1500px] space-y-5">
      <header className="border-b border-[#263246] pb-5">
        <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-cyan-300">
          <ListRestart aria-hidden="true" className="h-3.5 w-3.5" />
          Historical processing
        </div>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight text-slate-50">Replay sessions</h1>
        <p className="mt-1 text-sm text-slate-500">Reprocess retained events by position, time range, or destination</p>
      </header>
      <div className="flex min-h-72 flex-col items-center justify-center rounded-lg border border-dashed border-[#34445e] bg-[#101827]/70 px-4 text-center">
        <Wrench aria-hidden="true" className="h-6 w-6 text-slate-500" />
        <p className="mt-3 text-sm font-semibold text-slate-200">Replay execution is not available yet</p>
        <p className="mt-1 max-w-md text-sm text-slate-500">Session storage exists in RelayDB, but cursor execution and destination routing still need implementation.</p>
      </div>
    </div>
  )
}