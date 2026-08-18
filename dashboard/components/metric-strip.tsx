import { AlertTriangle, Database, Gauge, Layers3, RadioTower } from 'lucide-react'

export type DashboardStats = {
  sources: number
  eventsPerSecond: number
  captureLag: string
  consumers: number
  dlqDepth: number
}

const metricDefinitions = [
  { key: 'sources', label: 'Sources', icon: Database },
  { key: 'eventsPerSecond', label: 'Events / sec', icon: RadioTower },
  { key: 'captureLag', label: 'Capture lag', icon: Gauge },
  { key: 'consumers', label: 'Consumer groups', icon: Layers3 },
  { key: 'dlqDepth', label: 'DLQ depth', icon: AlertTriangle },
] as const

export function MetricStrip({ stats }: { stats: DashboardStats }) {
  return (
    <section className="grid grid-cols-2 overflow-hidden rounded-lg border border-[#263246] bg-[#101827] sm:grid-cols-3 xl:grid-cols-5" aria-label="Pipeline metrics">
      {metricDefinitions.map(({ key, label, icon: Icon }, index) => {
        const value = stats[key]
        const warning = (key === 'dlqDepth' && value !== 0) || (key === 'captureLag' && value !== 'n/a' && Number.parseFloat(String(value)) > 60)

        return (
          <div className={`min-h-28 px-4 py-4 ${index > 0 ? 'border-l border-[#263246]' : ''}`} key={key}>
            <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.13em] text-slate-500">
              <Icon aria-hidden="true" className={warning ? 'h-3.5 w-3.5 text-amber-300' : 'h-3.5 w-3.5 text-cyan-300'} />
              <span>{label}</span>
            </div>
            <p className={`mt-4 font-data text-2xl font-semibold tracking-tight ${warning ? 'text-amber-200' : 'text-slate-100'}`}>
              {key === 'eventsPerSecond' ? Number(value).toFixed(1) : value}
            </p>
          </div>
        )
      })}
    </section>
  )
}