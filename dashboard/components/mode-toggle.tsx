'use client'

import { usePathname, useSearchParams } from 'next/navigation'
import { modeHref, resolveMode } from '../lib/dashboard-data'

export function ModeToggle() {
  const pathname = usePathname()
  const searchParams = useSearchParams()
  const activeMode = resolveMode(searchParams.get('mode'))

  return (
    <div className="flex overflow-hidden rounded-sm border border-seam-strong bg-surface text-xs font-semibold" role="group" aria-label="Dashboard data mode">
      {(['demo', 'live'] as const).map((mode) => (
        <a
          aria-current={activeMode === mode ? 'true' : undefined}
          className={`px-3 py-1.5 transition-colors ${activeMode === mode ? 'bg-petrol text-white' : 'text-soft hover:text-ink'}`}
          href={modeHref(pathname, mode)}
          key={mode}
        >
          {mode === 'demo' ? 'Demo' : 'Live'}
        </a>
      ))}
    </div>
  )
}
