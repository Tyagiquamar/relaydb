export function SiteFooter() {
  return (
    <footer className="mt-12 border-t border-seam pt-5 text-[13px] text-faint">
      <p>
        Part of a trio of durable-execution systems —{' '}
        <a className="text-soft underline decoration-seam-strong underline-offset-4 hover:text-petrol" href="https://github.com/Tyagiquamar/relaydb">source</a>
        {' · '}
        <a className="text-soft underline decoration-seam-strong underline-offset-4 hover:text-petrol" href="https://quamar.vercel.app">portfolio</a>.
        Every guarantee here is reproducible from the repo: run <span className="font-data">go test ./...</span>.
      </p>
    </footer>
  )
}
