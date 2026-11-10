import { expect, it } from 'vitest'
import { getCategoryLabel, isKnownCategory } from './category-presentation'

it('uses curated presentation labels and safe fallbacks for unexpected IDs', () => {
  expect(getCategoryLabel('developer-cloud')).toBe('Developer & Cloud')
  for (const value of ['future-category', '__proto__', 'constructor', '', null, undefined, {}]) {
    expect(isKnownCategory(value)).toBe(false)
    expect(getCategoryLabel(value)).toMatch(/^Unsupported category/)
  }
  expect(getCategoryLabel('future-category')).toBe('Unsupported category (future-category)')
})
