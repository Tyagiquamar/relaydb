import type { DashboardStats } from '../components/metric-strip'
import type { EventRecord } from '../components/event-table'

export type DashboardMode = 'demo' | 'live'

export type SourceRecord = {
  id: string
  name: string
  status: string
  replication_slot: string
  created_at: string
}

export type DLQRecord = {
  id: number
  event_id: string
  source_id: string
  failure_reason: string
  status: string
  created_at: string
}

export type ConsumerPartitionRecord = {
  group: string
  partition: number
  owner: string
  generation: number
  lag: number
  state: 'active' | 'retry-delayed' | 'halted'
  nextRetryAt?: string
}

export type ReplayRecord = {
  id: string
  name: string
  destination: 'webhook' | 'consumer' | 'jsonl'
  status: 'pending' | 'running' | 'completed' | 'failed'
  processed: number
  total: number
  cursor: string
}

export type DashboardData = {
  mode: DashboardMode
  stats: DashboardStats
  events: EventRecord[]
  sources: SourceRecord[]
  dlqEntries: DLQRecord[]
  observedAt: string
}

const observedAt = '2026-08-19T14:30:00.000Z'

const demoSources: SourceRecord[] = [
  {
    id: '4a8e9c70-12f3-4c5d-a041-5e80f72df401',
    name: 'commerce-primary',
    status: 'streaming',
    replication_slot: 'relaydb_commerce_slot',
    created_at: '2026-08-19T12:00:00.000Z',
  },
]

const demoEvents: EventRecord[] = [
  { id: '01HZWQ9M9GQPWGKJY3K07TQJFM', schema_name: 'public', table_name: 'orders', operation: 'insert', commit_end_lsn: '0/16B374D848', created_at: '2026-08-19T14:29:42.000Z' },
  { id: '01HZWQ9M9H2GVF9P5J35MRNC7E', schema_name: 'public', table_name: 'order_items', operation: 'insert', commit_end_lsn: '0/16B374D848', created_at: '2026-08-19T14:29:42.000Z' },
  { id: '01HZWQ9M9J6V6E15W3S4W0KHZD', schema_name: 'public', table_name: 'inventory', operation: 'update', commit_end_lsn: '0/16B374D848', created_at: '2026-08-19T14:29:42.000Z' },
  { id: '01HZWQ6P3PH8Q01QXS0TPH8DTW', schema_name: 'public', table_name: 'payments', operation: 'update', commit_end_lsn: '0/16B374C810', created_at: '2026-08-19T14:24:18.000Z' },
  { id: '01HZWQ4F4YAFJ5SA3P4BGT3S5T', schema_name: 'public', table_name: 'orders', operation: 'update', commit_end_lsn: '0/16B374A0F8', created_at: '2026-08-19T14:18:06.000Z' },
]

const demoDLQEntries: DLQRecord[] = [
  {
    id: 41,
    event_id: '01HZWQ4F4YAFJ5SA3P4BGT3S5T',
    source_id: demoSources[0].id,
    failure_reason: 'webhook delivery exhausted after 5 attempts: upstream returned 503',
    status: 'pending',
    created_at: '2026-08-19T14:20:09.000Z',
  },
]

const demoConsumerPartitions: ConsumerPartitionRecord[] = [
  { group: 'warehouse-sync', partition: 3, owner: 'warehouse-worker-b', generation: 12, lag: 0, state: 'active' },
  { group: 'fraud-review', partition: 7, owner: 'fraud-worker-a', generation: 5, lag: 1, state: 'retry-delayed', nextRetryAt: '2026-08-19T14:31:00.000Z' },
  { group: 'analytics-export', partition: 11, owner: 'analytics-worker-c', generation: 8, lag: 4, state: 'halted' },
]

const demoReplays: ReplayRecord[] = [
  { id: 'rpl_01HZWQ9M', name: 'warehouse reconciliation', destination: 'webhook', status: 'running', processed: 8421, total: 12000, cursor: '0/16B374D848 · 3' },
  { id: 'rpl_01HZWQ6P', name: 'backfill order analytics', destination: 'jsonl', status: 'completed', processed: 28004, total: 28004, cursor: '0/16B374C810 · 1' },
]

// Live is the default view; demo fixtures are opt-in via ?mode=demo.
export function resolveMode(value: string | null): DashboardMode {
  return value === 'demo' ? 'demo' : 'live'
}

export function modeHref(pathname: string, mode: DashboardMode): string {
  return mode === 'demo' ? `${pathname}?mode=demo` : pathname
}

export function demoDashboardData(): DashboardData {
  return {
    mode: 'demo',
    stats: {
      sources: demoSources.length,
      eventsPerSecond: 18.4,
      captureLag: '0.4s',
      consumers: 3,
      dlqDepth: demoDLQEntries.length,
    },
    events: demoEvents,
    sources: demoSources,
    dlqEntries: demoDLQEntries,
    observedAt,
  }
}

export async function getDashboardData(mode: DashboardMode): Promise<DashboardData> {
  if (mode === 'demo') return demoDashboardData()

  const [statsResponse, eventsResponse, sourcesResponse, dlqResponse] = await Promise.all([
    fetch('/api/v1/stats', { cache: 'no-store' }),
    fetch('/api/v1/events?limit=50', { cache: 'no-store' }),
    fetch('/api/v1/sources', { cache: 'no-store' }),
    fetch('/api/v1/dlq', { cache: 'no-store' }),
  ])

  if (!statsResponse.ok || !eventsResponse.ok || !sourcesResponse.ok || !dlqResponse.ok) {
    throw new Error('RelayDB API is unavailable')
  }

  const [stats, eventsBody, sourcesBody, dlqBody] = await Promise.all([
    statsResponse.json() as Promise<DashboardStats>,
    eventsResponse.json() as Promise<{ events: EventRecord[] | null }>,
    sourcesResponse.json() as Promise<{ sources: SourceRecord[] | null }>,
    dlqResponse.json() as Promise<{ entries: DLQRecord[] | null }>,
  ])

  return {
    mode,
    stats,
    events: eventsBody.events ?? [],
    sources: sourcesBody.sources ?? [],
    dlqEntries: dlqBody.entries ?? [],
    observedAt: new Date().toISOString(),
  }
}

export function demoConsumerPartitionData(): ConsumerPartitionRecord[] {
  return demoConsumerPartitions
}

export function demoReplayData(): ReplayRecord[] {
  return demoReplays
}