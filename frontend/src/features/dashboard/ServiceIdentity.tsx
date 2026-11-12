import { useState } from 'react'
import { getServiceLogo } from './serviceLogos'

// Key by asset so an error cannot carry over when a search row changes service.
export function ServiceIdentity({ serviceId, name, size }: { serviceId: string; name: string; size: number }) {
  const padding = 2
  const src = getServiceLogo(serviceId, size - padding * 2)
  return <IdentityImage key={src ?? serviceId} src={src} name={name} size={size} padding={padding} />
}

function IdentityImage({ src, name, size, padding }: { src: string | undefined; name: string; size: number; padding: number }) {
  const [failed, setFailed] = useState(false)
  const showLogo = src !== undefined && !failed
  const words = name.trim().split(/\s+/u)
  const initials = words.length > 1
    ? words.slice(0, 2).map((word) => Array.from(word)[0] ?? '').join('')
    : Array.from(words[0]).slice(0, 2).join('')

  return (
    <span
      aria-hidden="true"
      data-service-identity={showLogo ? 'logo' : 'monogram'}
      className={`flex shrink-0 items-center justify-center overflow-hidden rounded-[5px] leading-none font-semibold ${showLogo ? 'bg-white ring-1 ring-inset ring-black/10' : 'bg-primary text-primary-foreground'} ${size >= 32 ? 'text-sm' : 'text-[9px]'}`}
      style={{ width: size, height: size, padding: showLogo ? padding : undefined, colorScheme: showLogo ? 'light' : undefined }}
    >
      {showLogo ? (
        <img
          src={src}
          alt=""
          width={size - padding * 2}
          height={size - padding * 2}
          decoding="async"
          draggable={false}
          className="size-full object-contain"
          onError={() => setFailed(true)}
        />
      ) : Array.from(initials.toUpperCase()).slice(0, 2).join('') || '?'}
    </span>
  )
}
