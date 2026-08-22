const points = [
  {
    kicker: 'The problem',
    title: 'Databases change. Everything downstream finds out late.',
    body: 'Caches drift, search indexes fall behind, invoices wait on batch jobs. Polling misses deletes, double-reads race transactions, and nobody can prove what was delivered — or to whom.',
  },
  {
    kicker: 'The guarantee',
    title: 'Every committed transaction, in order, exactly once.',
    body: 'RelayDB reads the write-ahead log itself (pgoutput logical replication), persists each source transaction before acknowledging its LSN, and fences checkpoints so a standby capture cannot double-deliver. Crash before ACK? The same transaction is re-ingested exactly once — proven by an end-to-end testcontainers suite.',
  },
  {
    kicker: 'The proof',
    title: 'The demo shop is real Postgres, streamed live.',
    body: 'A self-driving commerce simulator writes orders, payments, and inventory changes; this console shows them captured from WAL in commit order with fenced checkpoints. Webhooks carry HMAC signatures you can verify, and the SSRF-guarded dialer refuses to call itself home.',
  },
]

export function WhySection() {
  return (
    <section className="mt-10 border-t-2 border-seam-strong pt-8">
      <p className="text-[10px] font-semibold uppercase tracking-[0.2em] text-petrol">Why this exists</p>
      <h2 className="mt-3 max-w-2xl font-display text-[26px] leading-tight text-ink md:text-3xl">
        Change data capture that can prove what it delivered
      </h2>
      <div className="mt-6 grid gap-6 md:grid-cols-3">
        {points.map((point) => (
          <article className="border-t border-seam-strong pt-4" key={point.kicker}>
            <p className="text-[10px] font-semibold uppercase tracking-[0.16em] text-faint">{point.kicker}</p>
            <h3 className="mt-2 text-base font-semibold leading-snug text-ink">{point.title}</h3>
            <p className="mt-2 text-sm leading-relaxed text-soft">{point.body}</p>
          </article>
        ))}
      </div>
    </section>
  )
}
