import type { LiveEvent } from './events'
import {
  applyLiveEvent, createInitialReconciliationState, reconcileSnapshots,
  type ReconciliationResult, type ReconciliationSnapshots,
} from './reconciliation'

export type ReconciliationAction =
  | { type: 'snapshots'; snapshots: ReconciliationSnapshots; events?: readonly LiveEvent[] }
  | { type: 'event'; event: LiveEvent }

export function createInitialReconciliationResult(): ReconciliationResult {
  return { state: createInitialReconciliationState(), successful: true, issues: [] }
}

// Diagnostics describe the latest operation. The future synchronization owner
// must not use an unrelated successful event to clear a failed recovery attempt.
export function reconciliationReducer(
  previous: ReconciliationResult, action: ReconciliationAction,
): ReconciliationResult {
  switch (action.type) {
    case 'snapshots':
      return reconcileSnapshots(previous.state, action.snapshots, action.events)
    case 'event':
      return applyLiveEvent(previous.state, action.event)
  }
}
