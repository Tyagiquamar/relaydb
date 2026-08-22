import type { Metadata } from 'next'
import { Suspense } from 'react'
import { Geist, Geist_Mono, Libre_Baskerville } from 'next/font/google'
import './globals.css'
import { OperationsSidebar } from '../components/operations-sidebar'

const geistSans = Geist({
  subsets: ['latin'],
  variable: '--font-sans',
})

const geistMono = Geist_Mono({
  subsets: ['latin'],
  variable: '--font-mono',
})

const libreBaskerville = Libre_Baskerville({
  subsets: ['latin'],
  weight: ['400', '700'],
  variable: '--font-display',
})

export const metadata: Metadata = {
  title: 'RelayDB — CDC Control Room',
  description:
    'PostgreSQL change data capture with exactly-once persistence, fenced LSN checkpoints, and signed webhook delivery — proven by end-to-end WAL tests.',
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en" className={`${geistSans.variable} ${geistMono.variable} ${libreBaskerville.variable}`}>
      <body className="min-h-screen bg-canvas text-ink antialiased">
        <Suspense fallback={<div className="min-h-screen bg-canvas" />}>
          <div className="min-h-screen lg:flex">
            <OperationsSidebar />
            <main className="min-w-0 flex-1 px-4 py-5 sm:px-6 lg:px-8 lg:py-7">{children}</main>
          </div>
        </Suspense>
      </body>
    </html>
  )
}
