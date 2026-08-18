'use client'

import { useEffect, useState } from 'react'

interface Event {
  id: string
  schema_name: string
  table_name: string
  operation: string
  commit_end_lsn: string
  created_at: string
}

export default function EventsPage() {
  const [events, setEvents] = useState<Event[]>([])

  useEffect(() => {
    fetch('/api/v1/events?limit=50')
      .then(r => r.json())
      .then(data => setEvents(data.events || []))
      .catch(console.error)
  }, [])

  return (
    <div className="space-y-6">
      <h2 className="text-2xl font-bold">Event Explorer</h2>

      <div className="bg-gray-900 rounded-lg border border-gray-800 overflow-hidden">
        <table className="w-full">
          <thead className="bg-gray-800">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase">Table</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase">Operation</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase">LSN</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase">Time</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800">
            {events.map(event => (
              <tr key={event.id} className="hover:bg-gray-800 cursor-pointer">
                <td className="px-6 py-4">
                  <span className="text-blue-400">{event.schema_name}.</span>
                  <span>{event.table_name}</span>
                </td>
                <td className="px-6 py-4">
                  <OperationBadge op={event.operation} />
                </td>
                <td className="px-6 py-4 font-mono text-sm text-gray-400">{event.commit_end_lsn}</td>
                <td className="px-6 py-4 text-gray-400">{new Date(event.created_at).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {events.length === 0 && (
          <div className="text-center py-12 text-gray-400">
            No events captured yet
          </div>
        )}
      </div>
    </div>
  )
}

function OperationBadge({ op }: { op: string }) {
  const colors: Record<string, string> = {
    insert: 'bg-green-900 text-green-300',
    update: 'bg-blue-900 text-blue-300',
    delete: 'bg-red-900 text-red-300',
  }
  
  return (
    <span className={`px-2 py-1 rounded-full text-xs font-medium ${colors[op] || 'bg-gray-700'}`}>
      {op}
    </span>
  )
}