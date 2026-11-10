import type { ServiceState } from '@/data/contracts'
import { servicePresentation } from './service-presentation'

export function ServiceStatus({ state }: { state: ServiceState }) {
  const { label, Icon, surface } = servicePresentation[state]
  return (
    <span className={`inline-flex shrink-0 items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] leading-4 text-foreground ${surface}`}>
      <Icon size={12} aria-hidden="true" />
      {label}
    </span>
  )
}
