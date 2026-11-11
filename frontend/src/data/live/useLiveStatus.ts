import { useEffect, useState, useSyncExternalStore } from 'react'
import { createLiveStatusCoordinator, type LiveStatusSnapshot } from './coordinator'

export function useLiveStatus(): LiveStatusSnapshot {
  const [coordinator] = useState(createLiveStatusCoordinator)
  const snapshot = useSyncExternalStore(coordinator.subscribe, coordinator.getSnapshot)
  useEffect(() => coordinator.start(), [coordinator])
  return snapshot
}
