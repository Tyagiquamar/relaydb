'use client'

import { useEffect, useState } from 'react'

interface Stats {
  sources: number
  eventsPerSecond: number
  captureLag: string
  consumers: number
  dlqDepth: number
}

export default function Dashboard() {
  const [stats, setStats] = useState<Stats>({
    sources: 0,
    eventsPerSecond: 0,
    captureLag: '0s',
    consumers: 0,
    dlqDepth: 0,
  })

  useEffect(() => {
    // Fetch stats from API
    fetch('/api/v1/stats')
      .then(r => r.json())
      .then(data => setStats(data))
      .catch(console.error)
  }, [])

  return (
    <div className="space-y-8">
      <h2 className="text-2xl font-bold">Overview</h2>
      
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-4">
        <StatCard title="Sources" value={stats.sources} />
        <StatCard title="Events/sec" value={stats.eventsPerSecond} />
        <StatCard title="Capture Lag" value={stats.captureLag} />
        <StatCard title="Consumers" value={stats.consumers} />
        <StatCard title="DLQ Depth" value={stats.dlqDepth} />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="bg-gray-900 rounded-lg p-6 border border-gray-800">
          <h3 className="text-lg font-semibold mb-4">Recent Activity</h3>
          <p className="text-gray-400">Event stream will appear here</p>
        </div>
        
        <div className="bg-gray-900 rounded-lg p-6 border border-gray-800">
          <h3 className="text-lg font-semibold mb-4">System Health</h3>
          <p className="text-gray-400">Capture status will appear here</p>
        </div>
      </div>
    </div>
  )
}

function StatCard({ title, value }: { title: string; value: string | number }) {
  return (
    <div className="bg-gray-900 rounded-lg p-6 border border-gray-800">
      <h3 className="text-sm font-medium text-gray-400">{title}</h3>
      <p className="text-2xl font-bold mt-2">{value}</p>
    </div>
  )
}