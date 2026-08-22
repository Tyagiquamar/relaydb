import { NextRequest, NextResponse } from 'next/server'

// BFF proxy (KTD-25): browser calls same-origin /api/v1/*, this route forwards
// to the RelayDB API with the reader API key attached server-side. The key is
// never exposed to browser JS.

const API_URL = process.env.RELAYDB_API_URL || 'http://localhost:8080'
const KEY_ID = process.env.RELAYDB_READER_KEY_ID || 'reader'
const KEY = process.env.RELAYDB_READER_KEY || 'dev-reader-key-change-in-production'

async function proxy(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  const { path } = await params
  const target = `${API_URL}/api/v1/${path.join('/')}${req.nextUrl.search}`

  const headers: Record<string, string> = {
    Authorization: `Bearer ${KEY_ID}:${KEY}`,
    'Content-Type': 'application/json',
  }

  const body = req.method === 'GET' || req.method === 'HEAD' ? undefined : await req.text()

  // Free-tier instances sleep; first request wakes them (~30-60s). One patient
  // attempt, then a single retry before surfacing a gateway error.
  const send = () =>
    fetch(target, { method: req.method, headers, body, cache: 'no-store', signal: AbortSignal.timeout(25_000) })

  try {
    let res: Response
    try {
      res = await send()
    } catch {
      await new Promise((resolve) => setTimeout(resolve, 8_000))
      res = await send()
    }
    const text = await res.text()
    return new NextResponse(text, {
      status: res.status,
      headers: { 'Content-Type': res.headers.get('Content-Type') || 'application/json' },
    })
  } catch (err) {
    return NextResponse.json(
      { error: 'Bad Gateway', message: `relaydb api unreachable: ${String(err)}` },
      { status: 502 },
    )
  }
}

export const GET = proxy
export const POST = proxy
export const PUT = proxy
export const DELETE = proxy
