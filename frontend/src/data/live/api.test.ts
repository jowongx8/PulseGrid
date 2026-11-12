import { beforeEach, describe, expect, it, vi } from 'vitest'
import { incidentsResponseFixture, statusResponseFixture } from '../fixtures'
import { ApiError, fetchIncidentsSnapshot, fetchStatusSnapshot } from './api'

let fetchMock: ReturnType<typeof vi.fn<typeof fetch>>
beforeEach(() => {
  fetchMock = vi.fn<typeof fetch>()
  vi.stubGlobal('fetch', fetchMock)
})

describe.each([
  { name: 'status', endpoint: '/api/v1/status', load: fetchStatusSnapshot, body: statusResponseFixture, empty: { services: [] } },
  { name: 'incidents', endpoint: '/api/v1/incidents', load: fetchIncidentsSnapshot, body: incidentsResponseFixture, empty: { active: [], resolved: [] } },
])('$name snapshots', ({ endpoint, load, body, empty }) => {
  it('requests the relative endpoint without caching and preserves the DTO', async () => {
    const controller = new AbortController()
    fetchMock.mockResolvedValue(Response.json(body))
    const result = await load(controller.signal)
    expect(result).toEqual(body)
    expect(fetchMock).toHaveBeenCalledExactlyOnceWith(endpoint, {
      method: 'GET', headers: { Accept: 'application/json' }, cache: 'no-store', signal: controller.signal,
    })
  })

  it('accepts valid empty collections without fabricating data', async () => {
    fetchMock.mockResolvedValue(Response.json(empty))
    expect(await load()).toEqual(empty)
  })

  it('returns the validated object without mapping, stripping or normalizing it', async () => {
    const payload = { ...body, futureProperty: 'preserved' }
    const response = Response.json(payload)
    vi.spyOn(response, 'json').mockResolvedValue(payload)
    fetchMock.mockResolvedValue(response)
    expect(await load()).toBe(payload)
  })

  it.each([404, 429, 500, 503])('reports HTTP %s before attempting JSON parsing', async (status) => {
    const response = new Response('Internal Server Error\n', { status })
    const parse = vi.spyOn(response, 'json')
    fetchMock.mockResolvedValue(response)
    await expect(load()).rejects.toMatchObject({ name: 'ApiError', kind: 'http', endpoint, status })
    expect(parse).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it.each(['not JSON', '{', ''])('classifies malformed or empty JSON: %j', async (json) => {
    fetchMock.mockResolvedValue(new Response(json))
    await expect(load()).rejects.toMatchObject({ name: 'ApiError', kind: 'invalid-json', endpoint, cause: expect.any(SyntaxError) })
  })

  it('rejects structurally invalid JSON without falling back to fixtures', async () => {
    fetchMock.mockResolvedValue(Response.json({ incompatible: true }))
    await expect(load()).rejects.toMatchObject({ name: 'ApiError', kind: 'invalid-response', endpoint, cause: expect.any(TypeError) })
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it('preserves genuine network failure causes', async () => {
    const cause = new TypeError('Failed to fetch')
    fetchMock.mockRejectedValue(cause)
    await expect(load()).rejects.toMatchObject({ kind: 'network', endpoint, cause, status: undefined })
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it('classifies an interrupted response-body read as a network failure', async () => {
    const cause = new TypeError('Connection lost')
    const response = Response.json(body)
    vi.spyOn(response, 'json').mockRejectedValue(cause)
    fetchMock.mockResolvedValue(response)
    await expect(load()).rejects.toMatchObject({ kind: 'network', endpoint, cause })
  })

  it('preserves native cancellation before fetching', async () => {
    const controller = new AbortController()
    controller.abort()
    await expect(load(controller.signal)).rejects.toBe(controller.signal.reason)
    expect(controller.signal.reason).toMatchObject({ name: 'AbortError' })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it.each([new Error('Superseded request'), 'Stopped by caller'])('preserves an in-flight custom abort reason: %s', async (reason) => {
    const controller = new AbortController()
    fetchMock.mockImplementation((_input, options) => new Promise((_resolve, reject) => {
      options?.signal?.addEventListener('abort', () => reject(options.signal?.reason), { once: true })
    }))
    const request = load(controller.signal)
    const rejection = expect(request).rejects.toBe(reason)
    controller.abort(reason)
    await rejection
  })

  it('keeps body-read cancellation distinct from a broken connection', async () => {
    const controller = new AbortController()
    const reason = new Error('Discard this snapshot')
    const response = Response.json(body)
    vi.spyOn(response, 'json').mockImplementation(async () => {
      controller.abort(reason)
      throw new DOMException('Body read aborted', 'AbortError')
    })
    fetchMock.mockResolvedValue(response)
    await expect(load(controller.signal)).rejects.toBe(reason)
  })

  it('does not return data if cancellation occurs as the body finishes', async () => {
    const controller = new AbortController()
    const reason = new DOMException('Snapshot canceled', 'AbortError')
    const response = Response.json(body)
    vi.spyOn(response, 'json').mockImplementation(async () => {
      controller.abort(reason)
      return body
    })
    fetchMock.mockResolvedValue(response)
    await expect(load(controller.signal)).rejects.toBe(reason)
  })

  it('propagates a native fetch AbortError without classifying it as a network failure', async () => {
    const reason = new DOMException('The operation was aborted', 'AbortError')
    fetchMock.mockRejectedValue(reason)
    await expect(load()).rejects.toBe(reason)
  })
})

it('rejects the entire status snapshot for an unknown category with a useful field path', async () => {
  fetchMock.mockResolvedValue(Response.json({ services: [
    statusResponseFixture.services[0],
    { ...statusResponseFixture.services[1], category: 'future-category' },
  ] }))
  await expect(fetchStatusSnapshot()).rejects.toMatchObject({
    kind: 'invalid-response', endpoint: '/api/v1/status', message: expect.stringContaining('services[1].category'),
  })
})

it('provides an Error instance for predictable failure handling', async () => {
  fetchMock.mockResolvedValue(new Response('', { status: 503 }))
  await expect(fetchStatusSnapshot()).rejects.toBeInstanceOf(ApiError)
  await expect(fetchStatusSnapshot()).rejects.toBeInstanceOf(Error)
})

it('accepts public Go handler examples without changing timestamp precision, offsets or nullability', async () => {
  // Examples from backend/internal/httpapi/{status,incidents}_test.go.
  const statusJSON = '{"services":[{"id":"github","name":"GitHub","category":"developer-cloud","websiteUrl":"https://github.com","weight":10,"status":{"state":"up","lastCheckedAt":"2026-09-21T14:51:58.123456789+10:00"}},{"id":"discord","name":"Discord","category":"communication","websiteUrl":"https://discord.com","weight":8,"status":{"state":"unknown","lastCheckedAt":null}}]}'
  const incidentsJSON = '{"active":[{"serviceId":"github","startedAt":"2026-09-22T14:00:00.123456789+10:00","resolvedAt":null}],"resolved":[{"serviceId":"discord","startedAt":"2026-09-22T11:00:00.123456789+10:00","resolvedAt":"2026-09-22T12:00:00.123456789+10:00"}]}'
  fetchMock.mockResolvedValueOnce(new Response(statusJSON)).mockResolvedValueOnce(new Response(incidentsJSON))
  const status = await fetchStatusSnapshot()
  const incidents = await fetchIncidentsSnapshot()
  expect(status.services[0].status.lastCheckedAt).toBe('2026-09-21T14:51:58.123456789+10:00')
  expect(status.services[1].status.lastCheckedAt).toBeNull()
  expect(incidents.active[0].resolvedAt).toBeNull()
  expect(`${incidents.active[0].serviceId}|${incidents.active[0].startedAt}`).toBe('github|2026-09-22T14:00:00.123456789+10:00')
  expect(incidents.resolved[0].resolvedAt).toBe('2026-09-22T12:00:00.123456789+10:00')
})
