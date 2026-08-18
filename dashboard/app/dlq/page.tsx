'use client'

import { useEffect, useState } from 'react'

interface DLQEntry {
  id: number
  event_id: string
  source_id: string
  failure_reason: string
  status: string
  created_at: string
}

export default function DLQPage() {
  const [entries, setEntries] = useState<DLQEntry[]>([])

  useEffect(() => {
    fetch('/api/v1/dlq')
      .then(r => r.json())
      .then(data => setEntries(data.entries || []))
      .catch(console.error)
  }, [])

  return (
    <div className="space-y-6">
      <h2 className="text-2xl font-bold">Dead Letter Queue</h2>

      <div className="bg-gray-900 rounded-lg border border-gray-800 overflow-hidden">
        <table className="w-full">
          <thead className="bg-gray-800">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase">ID</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase">Source</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase">Reason</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase">Status</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800">
            {entries.map(entry => (
              <tr key={entry.id} className="hover:bg-gray-800">
                <td className="px-6 py-4 font-mono text-sm">{entry.id}</td>
                <td className="px-6 py-4">{entry.source_id}</td>
                <td className="px-6 py-4 text-red-400">{entry.failure_reason}</td>
                <td className="px-6 py-4">
                  <span className={`px-2 py-1 rounded-full text-xs font-medium ${
                    entry.status === 'pending' ? 'bg-yellow-900 text-yellow-300' : 'bg-gray-700'
                  }`}>
                    {entry.status}
                  </span>
                </td>
                <td className="px-6 py-4">
                  <button className="text-blue-400 hover:text-blue-300 text-sm">Retry</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {entries.length === 0 && (
          <div className="text-center py-12 text-gray-400">
            DLQ is empty
          </div>
        )}
      </div>
    </div>
  )
}