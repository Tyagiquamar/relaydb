'use client'

import { useEffect, useState } from 'react'
import { CircleAlert, RadioTower } from 'lucide-react'
import { EventRecord, EventTable } from '../../components/event-table'

export default function EventsPage() {
  const [events, setEvents] = useState<EventRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    async function loadEvents() {
      try {
        const response = await fetch('/api/v1/events?limit=50', { cache: 'no-store' })
        if (!response.ok) throw new Error('Unable to load events')
        const data = await response.json() as { events: EventRecord[] | null }
        setEvents(data.events ?? [])
        setError('')
      } catch (loadError) {
        setError(loadError instanceof Error ? loadError.message : 'Unable to load events')
      } finally {
        setLoading(false)
      }
    }

    void loadEvents()
  }, [])

  return (
    <div className="mx-auto max-w-[1500px] space-y-5">
      <header className="border-b border-[#263246] pb-5">
        <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-cyan-300">
          <RadioTower aria-hidden="true" className="h-3.5 w-3.5" />
          Durable event store
        </div>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight text-slate-50">Event stream</h1>
        <p className="mt-1 text-sm text-slate-500">Most recent committed changes, ordered by WAL position</p>
      </header>

      {error && <p className="flex items-center gap-2 text-sm text-amber-200"><CircleAlert aria-hidden="true" className="h-4 w-4" />{error}</p>}
      <EventTable events={events} loading={loading} />
    </div>
  )
}
