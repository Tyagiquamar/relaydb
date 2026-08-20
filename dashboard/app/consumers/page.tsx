'use client'

import { useSearchParams } from 'next/navigation'
import { Layers3 } from 'lucide-react'
import { demoConsumerPartitionData, resolveMode } from '../../lib/dashboard-data'

export default function ConsumersPage() {
  const searchParams = useSearchParams()
  const mode = resolveMode(searchParams.get('mode'))
  const partitions = demoConsumerPartitionData()

  return (
    <div className="mx-auto max-w-[1500px] space-y-5">
      <header className="border-b border-[#263246] pb-5">
        <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-cyan-300">
          <Layers3 aria-hidden="true" className="h-3.5 w-3.5" />
          Delivery topology
        </div>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight text-slate-50">Consumer groups</h1>
        <p className="mt-1 text-sm text-slate-500">{mode === 'demo' ? 'Deterministic lease, retry, and poison-policy evidence' : 'Consumer telemetry is not exposed by the live API yet'}</p>
      </header>
      {mode === 'live' ? <Unavailable /> : <div className="overflow-hidden rounded-lg border border-[#263246] bg-[#101827]">
        <div className="hidden grid-cols-[minmax(180px,1.2fr)_90px_minmax(150px,1fr)_90px_80px_120px] gap-4 border-b border-[#263246] px-5 py-3 text-[10px] font-semibold uppercase tracking-[0.15em] text-slate-500 md:grid"><span>Consumer group</span><span>Partition</span><span>Lease owner</span><span>Generation</span><span>Lag</span><span>State</span></div>
        <div className="divide-y divide-[#1d293a]">{partitions.map((partition) => <article className="grid gap-2 px-5 py-4 md:grid-cols-[minmax(180px,1.2fr)_90px_minmax(150px,1fr)_90px_80px_120px] md:items-center md:gap-4" key={`${partition.group}-${partition.partition}`}><p className="text-sm font-semibold text-slate-100">{partition.group}</p><p className="font-data text-xs text-slate-400">{partition.partition}</p><p className="font-data text-xs text-slate-400">{partition.owner}</p><p className="font-data text-xs text-slate-400">{partition.generation}</p><p className="font-data text-xs text-slate-400">{partition.lag}</p><div><StateBadge state={partition.state} />{partition.nextRetryAt && <p className="mt-1 text-[11px] text-slate-500">retry {new Date(partition.nextRetryAt).toLocaleTimeString()}</p>}</div></article>)}</div>
      </div>}
    </div>
  )
}

function StateBadge({ state }: { state: 'active' | 'retry-delayed' | 'halted' }) {
  const className = state === 'active' ? 'border-cyan-400/20 bg-cyan-400/10 text-cyan-200' : state === 'retry-delayed' ? 'border-amber-400/20 bg-amber-400/10 text-amber-200' : 'border-rose-400/20 bg-rose-400/10 text-rose-200'
  return <span className={`w-fit rounded border px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.1em] ${className}`}>{state}</span>
}

function Unavailable() {
  return <div className="flex min-h-72 flex-col items-center justify-center rounded-lg border border-dashed border-[#34445e] bg-[#101827]/70 px-4 text-center"><p className="text-sm font-semibold text-slate-200">Live consumer telemetry is unavailable</p><p className="mt-1 max-w-md text-sm text-slate-500">The gRPC consumer API exists, but the dashboard endpoint for group membership and offsets is still pending.</p></div>
}