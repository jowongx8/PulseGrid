/// <reference types="node" />
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it, vi } from 'vitest'
import { getServiceLogo, serviceLogos } from './serviceLogos'

const directory = resolve('public/logos')
const catalogue = readFileSync(resolve('../backend/internal/service/catalogue.go'), 'utf8')
const ids = [...catalogue.matchAll(/^\s*ID:\s+"([^"]+)"/gmu)].map((match) => match[1])
const sources = readFileSync(resolve(directory, 'SOURCES.md'), 'utf8')

describe('service logos', () => {
  it('accounts for every actual backend catalogue ID', () => {
    expect(Object.keys(serviceLogos).sort()).toEqual(ids.toSorted())
    expect(getServiceLogo('openai')).toBe('/logos/openai.svg')
    expect(getServiceLogo('chatgpt')).toBeUndefined()
    expect(Object.keys(serviceLogos).filter((id) => serviceLogos[id] === null).sort())
      .toEqual([])
    expect(getServiceLogo('amazon', 14)).toBe('/logos/amazon.svg')
  })

  it.each(ids)('associates %s with its local asset or documented monogram', (id) => {
    const asset = serviceLogos[id]
    expect(sources).toContain(`| \`${id}\` |`)
    if (asset === null) {
      expect(getServiceLogo(id)).toBeUndefined()
      expect(sources).toContain(`| \`${id}\` | Monogram`)
      return
    }
    expect(asset).toMatch(new RegExp(`^${id}\\.(svg|png|webp)$`, 'u'))
    const data = readFileSync(resolve(directory, asset))
    expect(data.byteLength).toBeGreaterThan(100)
    expect(data.byteLength).toBeLessThan(20_000)
    if (asset.endsWith('.svg')) {
      const document = new DOMParser().parseFromString(data.toString(), 'image/svg+xml')
      expect(document.querySelector('parsererror')).toBeNull()
      expect(document.documentElement.localName).toBe('svg')
      expect(document.querySelector('script, foreignObject, image, animate, set')).toBeNull()
      for (const element of document.querySelectorAll('*')) {
        for (const attribute of element.attributes) {
          expect(attribute.name).not.toMatch(/^on/iu)
          if (attribute.localName === 'href') expect(attribute.value).toMatch(/^#/u)
        }
      }
      expect(data.toString()).not.toMatch(/@import|url\(\s*["']?https?:/iu)
    } else {
      expect(data.subarray(1, 4).toString()).toBe('PNG')
      expect(data.readUInt32BE(16)).toBeLessThanOrEqual(96)
      expect(data.readUInt32BE(20)).toBeLessThanOrEqual(96)
    }
  })

  it('preserves all existing local asset coverage with a bounded payload', () => {
    const assets = readdirSync(directory).filter((name) => /\.(svg|png|webp)$/u.test(name))
    expect(assets.sort()).toEqual(Object.values(serviceLogos).filter((asset) => asset !== null).sort())
    expect(assets.reduce((sum, asset) => sum + statSync(resolve(directory, asset)).size, 0)).toBeLessThan(200_000)
  })

  it.each(['missing', 'nintendo-eshop', 'toString', '__proto__', '../github', 'https://example.com'])('keeps %s on the monogram fallback', (id) => {
    expect(getServiceLogo(id)).toBeUndefined()
  })

  it('respects artwork size limits', () => {
    expect(getServiceLogo('spotify', 14)).toBe('/logos/spotify.svg')
    expect(getServiceLogo('spotify', 21)).toBe('/logos/spotify.svg')
    expect(getServiceLogo('instagram', 14)).toBe('/logos/instagram.svg')
    expect(getServiceLogo('instagram', 29)).toBe('/logos/instagram.svg')
    expect(getServiceLogo('walmart', 15)).toBeUndefined()
    expect(getServiceLogo('walmart', 16)).toBe('/logos/walmart.svg')
  })

  it('honors the Vite deployment base path', () => {
    vi.stubEnv('BASE_URL', '/pulsegrid/')
    try {
      expect(getServiceLogo('github')).toBe('/pulsegrid/logos/github.svg')
    } finally {
      vi.unstubAllEnvs()
    }
  })
})
