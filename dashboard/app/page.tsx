'use client'

import { useCallback, useEffect, useState } from 'react'
import { CircleAlert, Database, RefreshCw, Send, ShieldCheck } from 'lucide-react'
import { useSearchParams } from 'next/navigation'
import { EventRecord, EventTable } from '../components/event-table'
import { DashboardStats, MetricStrip } from '../components/metric-strip'
import { DashboardMode, getDashboardData, resolveMode, SourceRecord } from '../lib/dashboard-data'
import { WhySection } from '../components/why-section'
import { SiteFooter } from '../components/site-footer'

const initialStats: DashboardStats = {
  sources: 0,
  eventsPerSecond: 0,
  captureLag: '0s',
  consumers: 0,
  dlqDepth: 0,
}

const demoApiUrl = process.env.NEXT_PUBLIC_DEMO_API_URL

export default function Dashboard() {
  const searchParams = useSearchParams()
  const mode = resolveMode(searchParams.get('mode'))
  const [stats, setStats] = useState<DashboardStats>(initialStats)
  const [events, setEvents] = useState<EventRecord[]>([])
  const [sources, setSources] = useState<SourceRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState('')
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null)

  const refresh = useCallback(async (manual = false) => {
    if (manual) setRefreshing(true)

    try {
      const data = await getDashboardData(mode)
      setStats(data.stats)
      setEvents(data.events.slice(0, 12))
      setSources(data.sources)
      setLastUpdated(new Date(data.observedAt))
      setError('')
    } catch (fetchError) {
      setError(fetchError instanceof Error ? fetchError.message : 'Unable to refresh pipeline state')
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [mode])

  useEffect(() => {
    void refresh()
    const interval = window.setInterval(() => void refresh(), 10_000)
    return () => window.clearInterval(interval)
  }, [refresh])

  const source = sources[0]
  const sourceHealthy = source?.status === 'streaming' || Boolean(source)
  const lagging = stats.captureLag !== 'n/a' && Number.parseFloat(stats.captureLag) > 60

  async function createDemoOrder() {
    if (!demoApiUrl) return

    setRefreshing(true)
    try {
      const response = await fetch(`${demoApiUrl}/orders`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ customer_id: 1, items: [{ product_id: 1, quantity: 1 }] }),
      })
      if (!response.ok) throw new Error('Demo order failed')
      window.setTimeout(() => void refresh(), 800)
    } catch (orderError) {
      setError(orderError instanceof Error ? orderError.message : 'Demo order failed')
      setRefreshing(false)
    }
  }

  return (
    <div className="mx-auto max-w-[1500px] space-y-5">
      <header className="flex flex-col gap-4 border-b border-seam pb-5 md:flex-row md:items-end md:justify-between">
        <div>
          <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-petrol">
            <Database aria-hidden="true" className="h-3.5 w-3.5" />
            Pipeline overview
          </div>
          <h1 className="mt-2 font-display text-2xl tracking-tight text-ink md:text-3xl">Capture operations</h1>
          <p className="mt-1 text-sm text-soft">{mode === 'demo' ? 'Deterministic fixture set for CDC inspection' : source ? `${source.name} · ${source.status}` : 'Waiting for a registered source'}</p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <p className="mr-1 font-data text-xs text-faint">{lastUpdated ? `Updated ${lastUpdated.toLocaleTimeString()}` : 'Loading pipeline state'}</p>
          <button className="inline-flex h-9 items-center gap-2 rounded-sm border border-seam-strong bg-surface px-3 text-sm font-medium text-body transition-colors hover:border-petrol/40 hover:text-petrol-deep disabled:cursor-not-allowed disabled:opacity-60" disabled={refreshing} onClick={() => void refresh(true)} type="button">
            <RefreshCw aria-hidden="true" className={`h-3.5 w-3.5 ${refreshing ? 'animate-spin' : ''}`} />
            Refresh
          </button>
          <button className="inline-flex h-9 items-center gap-2 rounded-sm bg-petrol px-3 text-sm font-semibold text-white transition-colors hover:bg-petrol-deep disabled:cursor-not-allowed disabled:opacity-40" disabled={mode === 'demo' || !demoApiUrl || refreshing} onClick={() => void createDemoOrder()} title={mode === 'demo' ? 'Switch to Live to generate a real demo order' : demoApiUrl ? 'Create a demo order' : 'Set NEXT_PUBLIC_DEMO_API_URL to enable demo orders'} type="button">
            <Send aria-hidden="true" className="h-3.5 w-3.5" />
            Generate event
          </button>
        </div>
      </header>

      {error && (
        <div className="flex items-center gap-2 rounded-sm border border-amber-500/30 bg-amber-400/10 px-3 py-2 text-sm text-amber-800" role="status">
          <CircleAlert aria-hidden="true" className="h-4 w-4 shrink-0" />
          {error}. Showing the last successful snapshot — the pipeline auto-refreshes every 10s.
        </div>
      )}

      <MetricStrip stats={stats} />

      <section className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_330px]">
        <div>
          <div className="mb-3 flex items-center justify-between">
            <div>
              <h2 className="text-sm font-semibold text-ink">Recent event stream</h2>
              <p className="mt-1 text-xs text-faint">Latest committed changes across published relations</p>
            </div>
            <a className="text-xs font-semibold text-petrol hover:text-petrol-deep" href="/events">View all events</a>
          </div>
          <EventTable events={events} loading={loading} />
        </div>

        <aside className="rounded-md border border-seam bg-surface p-5">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-[10px] font-semibold uppercase tracking-[0.15em] text-faint">Capture health</p>
              <h2 className="mt-2 text-lg font-semibold text-ink">{sourceHealthy && !lagging ? 'Pipeline is flowing' : 'Pipeline needs attention'}</h2>
            </div>
            <ShieldCheck aria-hidden="true" className={`h-6 w-6 ${sourceHealthy && !lagging ? 'text-petrol' : 'text-amber-600'}`} />
          </div>

          <dl className="mt-6 divide-y divide-seam border-y border-seam">
            <HealthRow label="Source" value={source?.name ?? 'Not registered'} />
            <HealthRow label="State" value={source?.status ?? 'Unknown'} valueClass={sourceHealthy ? 'text-petrol-deep' : 'text-amber-700'} />
            <HealthRow label="Capture lag" value={stats.captureLag} valueClass={lagging ? 'text-amber-700' : 'text-body'} />
            <HealthRow label="Dead letters" value={String(stats.dlqDepth)} valueClass={stats.dlqDepth > 0 ? 'text-rose-700' : 'text-body'} />
          </dl>

          <p className="mt-5 text-xs leading-5 text-soft">RelayDB persists each committed transaction before acknowledging its WAL position.</p>
        </aside>
      </section>

      <WhySection />
      <SiteFooter />
    </div>
  )
}

function HealthRow({ label, value, valueClass = 'text-body' }: { label: string; value: string; valueClass?: string }) {
  return (
    <div className="flex items-center justify-between gap-3 py-3 text-sm">
      <dt className="text-faint">{label}</dt>
      <dd className={`font-data text-xs ${valueClass}`}>{value}</dd>
    </div>
  )
}
