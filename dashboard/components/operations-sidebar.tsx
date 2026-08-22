'use client'

import {
  Activity,
  Database,
  Layers3,
  ListRestart,
  RadioTower,
  ShieldAlert,
} from 'lucide-react'
import { usePathname, useSearchParams } from 'next/navigation'
import { modeHref, resolveMode } from '../lib/dashboard-data'
import { ModeToggle } from './mode-toggle'

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
  const searchParams = useSearchParams()
  const mode = resolveMode(searchParams.get('mode'))

  return (
    <aside className="border-b border-seam bg-surface lg:sticky lg:top-0 lg:h-screen lg:w-60 lg:shrink-0 lg:border-b-0 lg:border-r">
      <div className="flex h-full flex-col">
        <div className="flex items-center justify-between px-4 py-4 lg:block lg:px-5 lg:py-6">
          <div>
            <p className="font-display text-xl tracking-tight text-ink">RelayDB</p>
            <p className="mt-1 text-[10px] font-medium uppercase tracking-[0.18em] text-faint">CDC control room</p>
          </div>
          <span className={`inline-flex items-center gap-2 rounded-sm border px-2.5 py-1 text-[10px] font-semibold uppercase tracking-[0.12em] ${mode === 'demo' ? 'border-amber-500/30 bg-amber-400/10 text-amber-800' : 'border-petrol/25 bg-petrol/10 text-petrol-deep'}`}>
            <span className={`h-1.5 w-1.5 rounded-full ${mode === 'demo' ? 'bg-amber-600' : 'animate-pulse bg-petrol'}`} />
            {mode === 'demo' ? 'Demo' : 'Live'}
          </span>
        </div>

        <nav className="no-scrollbar flex gap-1 overflow-x-auto border-t border-seam px-3 py-3 lg:block lg:overflow-visible lg:border-0 lg:px-3 lg:py-2" aria-label="Primary navigation">
          {navigation.map(({ href, label, icon: Icon }) => {
            const active = href === '/' ? pathname === '/' : pathname.startsWith(href)

            return (
              <a
                className={`group inline-flex shrink-0 items-center gap-3 rounded-sm px-3 py-2.5 text-sm font-medium transition-colors lg:mb-1 lg:flex ${
                  active
                    ? 'bg-raised text-petrol-deep shadow-[inset_2px_0_0_#0f6b87]'
                    : 'text-soft hover:bg-raised hover:text-ink'
                }`}
                aria-current={active ? 'page' : undefined}
                href={modeHref(href, mode)}
                key={href}
              >
                <Icon aria-hidden="true" className="h-4 w-4 shrink-0" strokeWidth={active ? 2.4 : 1.8} />
                <span>{label}</span>
              </a>
            )
          })}
        </nav>

        <div className="mt-auto hidden border-t border-seam px-5 py-5 lg:block">
          <p className="text-[10px] font-semibold uppercase tracking-[0.16em] text-faint">Environment</p>
          <div className="mt-2"><ModeToggle /></div>
          <p className="mt-3 font-data text-xs text-soft">{mode === 'demo' ? 'deterministic fixtures' : 'configured API'}</p>
        </div>
      </div>
    </aside>
  )
}
