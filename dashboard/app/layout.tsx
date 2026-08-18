import type { Metadata } from 'next'
import './globals.css'

export const metadata: Metadata = {
  title: 'RelayDB Dashboard',
  description: 'PostgreSQL CDC and Replay Platform Operations',
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en">
      <body className="min-h-screen bg-gray-950 text-gray-100 antialiased">
        <nav className="border-b border-gray-800 px-6 py-4">
          <div className="flex items-center justify-between max-w-7xl mx-auto">
            <div className="flex items-center space-x-8">
              <h1 className="text-xl font-bold text-white">RelayDB</h1>
              <div className="flex space-x-4">
                <NavLink href="/">Overview</NavLink>
                <NavLink href="/sources">Sources</NavLink>
                <NavLink href="/events">Events</NavLink>
                <NavLink href="/consumers">Consumers</NavLink>
                <NavLink href="/replays">Replays</NavLink>
                <NavLink href="/dlq">DLQ</NavLink>
              </div>
            </div>
            <div className="text-sm text-gray-400">
              CDC Platform
            </div>
          </div>
        </nav>
        <main className="max-w-7xl mx-auto px-6 py-8">
          {children}
        </main>
      </body>
    </html>
  )
}

function NavLink({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a
      href={href}
      className="text-gray-300 hover:text-white transition-colors px-3 py-2 rounded-md hover:bg-gray-800"
    >
      {children}
    </a>
  )
}