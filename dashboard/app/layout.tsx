import type { Metadata } from 'next'
import { Suspense } from 'react'
import './globals.css'
import { OperationsSidebar } from '../components/operations-sidebar'

export const metadata: Metadata = {
  title: 'RelayDB | CDC Control Room',
  description: 'Operational visibility for PostgreSQL change data capture.',
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en">
      <body className="min-h-screen bg-[#080d16] text-slate-100 antialiased">
        <Suspense fallback={<div className="min-h-screen bg-[#080d16]" />}>
          <div className="min-h-screen lg:flex">
            <OperationsSidebar />
            <main className="min-w-0 flex-1 px-4 py-5 sm:px-6 lg:px-8 lg:py-7">{children}</main>
          </div>
        </Suspense>
      </body>
    </html>
  )
}
