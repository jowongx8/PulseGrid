// @vitest-environment node
import { createServer as createHTTPServer } from 'node:http'
import { createServer as createViteServer } from 'vite'
import { expect, it } from 'vitest'
import config from './vite.config.ts'

it('forwards SSE headers before any body data while preserving ordinary REST responses', async () => {
  const upstream = createHTTPServer((request, response) => {
    if (request.url === '/api/v1/events') {
      response.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' })
      response.flushHeaders()
      // No body is sent: the proxy must not wait for an event or heartbeat.
    } else {
      response.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' })
      response.end('{"services":[]}')
    }
  })
  const controller = new AbortController()
  let vite: Awaited<ReturnType<typeof createViteServer>> | undefined
  try {
    await new Promise<void>((resolve) => upstream.listen(0, '127.0.0.1', resolve))
    const address = upstream.address()
    if (!address || typeof address === 'string') throw new Error('Missing upstream address')
    const proxy = config.server?.proxy?.['/api']
    if (!proxy || typeof proxy === 'string') throw new Error('Missing API proxy configuration')
    expect(proxy.target).toBe('http://127.0.0.1:8080')
    expect(proxy.timeout).toBeUndefined()
    expect(proxy.proxyTimeout).toBeUndefined()
    vite = await createViteServer({
      ...config,
      configFile: false,
      logLevel: 'silent',
      server: {
        host: '127.0.0.1', port: 0, watch: null, hmr: false,
        proxy: { '/api': { ...proxy, target: `http://127.0.0.1:${address.port}` } },
      },
    })
    await vite.listen()
    const local = vite.httpServer?.address()
    if (!local || typeof local === 'string') throw new Error('Missing Vite address')
    const origin = `http://127.0.0.1:${local.port}`
    const stream = await fetch(`${origin}/api/v1/events`, { signal: AbortSignal.any([controller.signal, AbortSignal.timeout(3000)]) })
    expect(stream.status).toBe(200)
    expect(stream.headers.get('content-type')).toBe('text/event-stream')
    expect(stream.headers.get('cache-control')).toBe('no-cache')
    controller.abort()
    const rest = await fetch(`${origin}/api/v1/status`)
    expect(rest.headers.get('cache-control')).toBe('no-store')
    expect(await rest.json()).toEqual({ services: [] })
  } finally {
    controller.abort()
    await vite?.close()
    upstream.closeAllConnections()
    await new Promise<void>((resolve) => upstream.close(() => resolve()))
  }
})
