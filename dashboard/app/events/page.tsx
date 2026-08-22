'use client'

import { useEffect, useState } from 'react'
import { CircleAlert, RadioTower } from 'lucide-react'
import { useSearchParams } from 'next/navigation'
import { EventRecord, EventTable } from '../../components/event-table'
import { getDashboardData, resolveMode } from '../../lib/dashboard-data'
import { SiteFooter } from '../../components/site-footer'

export default function EventsPage() {
  const searchParams = useSearchParams()
  const mode = resolveMode(searchParams.get('mode'))
  const [events, setEvents] = useState<EventRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    async function loadEvents() {
      try {
        const data = await getDashboardData(mode)
        setEvents(data.events)
        setError('')
      } catch (loadError) {
        setError(loadError instanceof Error ? loadError.message : 'Unable to load events')
      } finally {
        setLoading(false)
      }
    }

    void loadEvents()
    const interval = window.setInterval(() => void loadEvents(), 10_000)
    return () => window.clearInterval(interval)
  }, [mode])

  return (
    <div className="mx-auto max-w-[1500px] space-y-5">
      <header className="border-b border-seam pb-5">
        <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-petrol">
          <RadioTower aria-hidden="true" className="h-3.5 w-3.5" />
          Durable event store
        </div>
        <h1 className="mt-2 font-display text-2xl tracking-tight text-ink md:text-3xl">Event stream</h1>
        <p className="mt-1 text-sm text-soft">{mode === 'demo' ? 'Deterministic multi-table transaction ordered by WAL position' : 'Most recent committed changes, ordered by WAL position'}</p>
      </header>

      {error && <p className="flex items-center gap-2 text-sm text-amber-800"><CircleAlert aria-hidden="true" className="h-4 w-4" />{error}</p>}
      <EventTable events={events} loading={loading} />
      <SiteFooter />
    </div>
  )
}
