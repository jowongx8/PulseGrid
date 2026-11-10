// One identity presentation for tiles, search and details; future logos can use
// this same boundary and retain initials as their fallback.
export function ServiceIdentity({ name, size }: { name: string; size: 18 | 20 | 44 }) {
  const words = name.trim().split(/\s+/u)
  const initials = words.length > 1
    ? words.slice(0, 2).map((word) => Array.from(word)[0] ?? '').join('')
    : Array.from(words[0]).slice(0, 2).join('')

  return (
    <span
      aria-hidden="true"
      data-service-identity="monogram"
      className={`flex shrink-0 items-center justify-center overflow-hidden rounded-[5px] bg-primary leading-none font-semibold text-primary-foreground ${size === 44 ? 'text-sm' : 'text-[9px]'}`}
      style={{ width: size, height: size }}
    >
      {Array.from(initials.toUpperCase()).slice(0, 2).join('') || '?'}
    </span>
  )
}
