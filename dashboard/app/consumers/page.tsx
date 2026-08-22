'use client'

import { useSearchParams } from 'next/navigation'
import { Layers3 } from 'lucide-react'
import { demoConsumerPartitionData, resolveMode } from '../../lib/dashboard-data'
import { SiteFooter } from '../../components/site-footer'

export default function ConsumersPage() {
  const searchParams = useSearchParams()
  const mode = resolveMode(searchParams.get('mode'))
  const partitions = demoConsumerPartitionData()

  return (
    <div className="mx-auto max-w-[1500px] space-y-5">
      <header className="border-b border-seam pb-5">
        <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-petrol">
          <Layers3 aria-hidden="true" className="h-3.5 w-3.5" />
          Delivery topology
        </div>
        <h1 className="mt-2 font-display text-2xl tracking-tight text-ink md:text-3xl">Consumer groups</h1>
        <p className="mt-1 text-sm text-soft">{mode === 'demo' ? 'Deterministic lease, retry, and poison-policy evidence' : 'Consumer telemetry is not exposed by the live API yet'}</p>
      </header>
      {mode === 'live' ? <Unavailable /> : <div className="overflow-hidden rounded-md border border-seam bg-surface">
        <div className="hidden grid-cols-[minmax(180px,1.2fr)_90px_minmax(150px,1fr)_90px_80px_120px] gap-4 border-b border-seam px-5 py-3 text-[10px] font-semibold uppercase tracking-[0.15em] text-faint md:grid"><span>Consumer group</span><span>Partition</span><span>Lease owner</span><span>Generation</span><span>Lag</span><span>State</span></div>
        <div className="divide-y divide-seam">{partitions.map((partition) => <article className="grid gap-2 px-5 py-4 transition-colors hover:bg-raised/60 md:grid-cols-[minmax(180px,1.2fr)_90px_minmax(150px,1fr)_90px_80px_120px] md:items-center md:gap-4" key={`${partition.group}-${partition.partition}`}><p className="text-sm font-semibold text-ink">{partition.group}</p><p className="font-data text-xs text-soft">{partition.partition}</p><p className="font-data text-xs text-soft">{partition.owner}</p><p className="font-data text-xs text-soft">{partition.generation}</p><p className="font-data text-xs text-soft">{partition.lag}</p><div><StateBadge state={partition.state} />{partition.nextRetryAt && <p className="mt-1 font-data text-[11px] text-faint">retry {new Date(partition.nextRetryAt).toLocaleTimeString()}</p>}</div></article>)}</div>
      </div>}
      <SiteFooter />
    </div>
  )
}

function StateBadge({ state }: { state: 'active' | 'retry-delayed' | 'halted' }) {
  const className = state === 'active' ? 'border-petrol/25 bg-petrol/10 text-petrol-deep' : state === 'retry-delayed' ? 'border-amber-500/30 bg-amber-400/10 text-amber-800' : 'border-rose-500/25 bg-rose-500/10 text-rose-800'
  return <span className={`w-fit rounded-sm border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.1em] ${className}`}>{state}</span>
}

function Unavailable() {
  return <div className="flex min-h-72 flex-col items-center justify-center rounded-md border border-dashed border-seam-strong bg-surface/70 px-4 text-center"><p className="text-sm font-semibold text-body">Live consumer telemetry is unavailable</p><p className="mt-1 max-w-md text-sm text-soft">The gRPC consumer API exists, but the dashboard endpoint for group membership and offsets is still pending.</p></div>
}
