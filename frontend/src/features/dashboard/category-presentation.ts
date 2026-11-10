import type { ServiceCategory } from '@/data/contracts'

// Labels and insertion order are presentation policy; membership comes from DTOs.
export const categoryLabels = {
  'developer-cloud': 'Developer & Cloud',
  ai: 'AI',
  communication: 'Communication',
  'social-media': 'Social Media',
  productivity: 'Productivity',
  entertainment: 'Entertainment',
  gaming: 'Gaming',
  commerce: 'Commerce',
} satisfies Record<ServiceCategory, string>

export function isKnownCategory(value: unknown): value is ServiceCategory {
  return typeof value === 'string' && Object.hasOwn(categoryLabels, value)
}

export function getCategoryLabel(value: unknown): string {
  if (isKnownCategory(value)) return categoryLabels[value]
  return typeof value === 'string' && value ? `Unsupported category (${value})` : 'Unsupported category'
}
