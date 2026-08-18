'use client'

import {
  Activity,
  Database,
  Layers3,
  ListRestart,
  RadioTower,
  ShieldAlert,
} from 'lucide-react'
import { usePathname } from 'next/navigation'

const navigation = [
  { href: '/', label: 'Overview', icon: Activity },
  { href: '/sources', label: 'Sources', icon: Database },
  { href: '/events', label: 'Event stream', icon: RadioTower },
  { href: '/consumers', label: 'Consumers', icon: Layers3 },
  { href: '/replays', label: 'Replays', icon: ListRestart },
  { href: '/dlq', label: 'Dead letters', icon: ShieldAlert },
]

export function OperationsSidebar() {
  const pathname = usePathname()

  return (
    <aside className="border-b border-[#263246] bg-[#0b1220] lg:sticky lg:top-0 lg:h-screen lg:w-60 lg:shrink-0 lg:border-b-0 lg:border-r">
      <div className="flex h-full flex-col">
        <div className="flex items-center justify-between px-4 py-4 lg:px-5 lg:py-6">
          <div>
            <p className="font-display text-lg font-semibold tracking-[0.08em] text-slate-50">RELAYDB</p>
            <p className="mt-1 text-[10px] font-medium uppercase tracking-[0.18em] text-slate-500">CDC control room</p>
          </div>
          <span className="inline-flex items-center gap-2 rounded-full border border-cyan-400/20 bg-cyan-400/10 px-2.5 py-1 text-[10px] font-semibold uppercase tracking-[0.12em] text-cyan-300">
            <span className="h-1.5 w-1.5 rounded-full bg-cyan-300 shadow-[0_0_10px_#67e8f9]" />
            Live
          </span>
        </div>

        <nav className="no-scrollbar flex gap-1 overflow-x-auto border-t border-[#1d293a] px-3 py-3 lg:block lg:overflow-visible lg:border-0 lg:px-3 lg:py-2" aria-label="Primary navigation">
          {navigation.map(({ href, label, icon: Icon }) => {
            const active = href === '/' ? pathname === '/' : pathname.startsWith(href)

            return (
              <a
                className={`group inline-flex shrink-0 items-center gap-3 rounded-md px-3 py-2.5 text-sm font-medium transition-colors lg:mb-1 lg:flex ${
                  active
                    ? 'bg-cyan-400/10 text-cyan-200 shadow-[inset_2px_0_0_#22d3ee]'
                    : 'text-slate-400 hover:bg-slate-800/70 hover:text-slate-100'
                }`}
                href={href}
                key={href}
              >
                <Icon aria-hidden="true" className="h-4 w-4 shrink-0" strokeWidth={active ? 2.4 : 1.8} />
                <span>{label}</span>
              </a>
            )
          })}
        </nav>

        <div className="mt-auto hidden border-t border-[#1d293a] px-5 py-5 lg:block">
          <p className="text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-500">Environment</p>
          <p className="mt-1 font-mono text-xs text-slate-300">development</p>
        </div>
      </div>
    </aside>
  )
}