import { CircleCheck, CircleHelp, OctagonX } from 'lucide-react'

export const servicePresentation = {
  up: { label: 'Operational', Icon: CircleCheck, surface: 'bg-op-surface', dot: 'bg-op' },
  down: { label: 'Down', Icon: OctagonX, surface: 'bg-down-surface', dot: 'bg-down' },
  unknown: { label: 'Unknown', Icon: CircleHelp, surface: 'bg-unknown-surface', dot: 'bg-unknown' },
}
