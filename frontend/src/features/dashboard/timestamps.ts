import type { ApiTimestamp } from '@/data/contracts'

export function formatTimestamp(timestamp: ApiTimestamp): string {
  const wholeSecond = timestamp.replace(/\.\d{1,9}(?=Z|[+-]\d{2}:\d{2}$)/, '')
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
    timeZoneName: 'shortOffset',
  }).format(Date.parse(wholeSecond))
}
