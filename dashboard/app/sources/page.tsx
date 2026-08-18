'use client'

import { useEffect, useState } from 'react'

interface Source {
  id: string
  name: string
  status: string
  replication_slot: string
  created_at: string
}

export default function SourcesPage() {
  const [sources, setSources] = useState<Source[]>([])

  useEffect(() => {
    fetch('/api/v1/sources')
      .then(r => r.json())
      .then(data => setSources(data.sources || []))
      .catch(console.error)
  }, [])

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h2 className="text-2xl font-bold">Sources</h2>
        <button className="bg-blue-600 hover:bg-blue-700 px-4 py-2 rounded-md text-sm font-medium">
          Add Source
        </button>
      </div>

      <div className="bg-gray-900 rounded-lg border border-gray-800 overflow-hidden">
        <table className="w-full">
          <thead className="bg-gray-800">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase">Name</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase">Status</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase">Slot</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase">Created</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800">
            {sources.map(source => (
              <tr key={source.id} className="hover:bg-gray-800">
                <td className="px-6 py-4">{source.name}</td>
                <td className="px-6 py-4">
                  <StatusBadge status={source.status} />
                </td>
                <td className="px-6 py-4 text-gray-400">{source.replication_slot}</td>
                <td className="px-6 py-4 text-gray-400">{new Date(source.created_at).toLocaleDateString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {sources.length === 0 && (
          <div className="text-center py-12 text-gray-400">
            No sources configured
          </div>
        )}
      </div>
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    streaming: 'bg-green-900 text-green-300',
    error: 'bg-red-900 text-red-300',
    paused: 'bg-yellow-900 text-yellow-300',
    registered: 'bg-gray-700 text-gray-300',
  }
  
  return (
    <span className={`px-2 py-1 rounded-full text-xs font-medium ${colors[status] || colors.registered}`}>
      {status}
    </span>
  )
}