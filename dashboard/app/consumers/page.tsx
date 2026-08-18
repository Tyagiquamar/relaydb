'use client'

import { Layers3, Wrench } from 'lucide-react'

export default function ConsumersPage() {
  return (
    <div className="mx-auto max-w-[1500px] space-y-5">
      <header className="border-b border-[#263246] pb-5">
        <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-cyan-300">
          <Layers3 aria-hidden="true" className="h-3.5 w-3.5" />
          Delivery topology
        </div>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight text-slate-50">Consumer groups</h1>
        <p className="mt-1 text-sm text-slate-500">Partition ownership and consumer offset visibility</p>
      </header>
      <div className="flex min-h-72 flex-col items-center justify-center rounded-lg border border-dashed border-[#34445e] bg-[#101827]/70 px-4 text-center">
        <Wrench aria-hidden="true" className="h-6 w-6 text-slate-500" />
        <p className="mt-3 text-sm font-semibold text-slate-200">Consumer telemetry is not exposed yet</p>
        <p className="mt-1 max-w-md text-sm text-slate-500">The gRPC consumer API exists, but the dashboard endpoint for group membership and offsets is still pending.</p>
      </div>
    </div>
  )
}