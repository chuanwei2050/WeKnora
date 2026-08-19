import { describe, expect, it } from 'vitest'
import {
  canExecuteGovernanceRowAction,
  canOperateGovernanceRow,
  getGovernanceActionNextStatus,
  getGovernanceRowActions,
  isGovernanceRowActionDisabled,
} from '../src/views/knowledge/components/knowledge-governance-actions'

const draft = {
  created_by: 'user-1',
  pending_version_id: 'version-1',
  parse_status: 'draft',
}

describe('getGovernanceRowActions', () => {
  it('allows the contributor to submit or delete an unused draft', () => {
    expect(getGovernanceRowActions(draft, {
      enabled: true,
      canContribute: true,
      canReview: false,
      currentUserId: 'user-1',
    })).toEqual(['submit', 'delete'])
  })

  it('allows the contributor to withdraw a pending submission', () => {
    expect(getGovernanceRowActions({ ...draft, parse_status: 'pending_review' }, {
      enabled: true,
      canContribute: true,
      canReview: false,
      currentUserId: 'user-1',
    })).toEqual(['withdraw', 'delete'])
    expect(isGovernanceRowActionDisabled({ ...draft, parse_status: 'pending_review' }, 'delete')).toBe(true)
  })

  it('switches a successful submission to pending review immediately', () => {
    expect(getGovernanceActionNextStatus('submit')).toBe('pending_review')
  })

  it('disables selection for a row without any enabled action', () => {
    const context = { enabled: true, canContribute: true, canReview: false, currentUserId: 'user-1' }
    expect(canOperateGovernanceRow({ ...draft, parse_status: 'pending_review' }, context)).toBe(true)
    expect(canOperateGovernanceRow({ ...draft, created_by: 'user-2', parse_status: 'completed' }, context)).toBe(false)
  })

  it('allows a reviewer to approve or reject another user submission', () => {
    expect(getGovernanceRowActions({ ...draft, parse_status: 'pending_review' }, {
      enabled: true,
      canContribute: false,
      canReview: true,
      currentUserId: 'reviewer-1',
    })).toEqual(['approve', 'reject'])
  })

  it('does not offer whole-document deletion when a version is already active', () => {
    expect(getGovernanceRowActions({ ...draft, current_version_id: 'version-active' }, {
      enabled: true,
      canContribute: true,
      canReview: false,
      currentUserId: 'user-1',
    })).toEqual(['submit'])
  })

  it('calculates each batch action from eligible rows in a mixed selection', () => {
    const context = { enabled: true, canContribute: true, canReview: false, currentUserId: 'user-1' }
    const pending = { ...draft, pending_version_id: 'version-2', parse_status: 'pending_review' }

    expect([draft, pending].filter(item => canExecuteGovernanceRowAction(item, context, 'submit'))).toEqual([draft])
    expect([draft, pending].filter(item => canExecuteGovernanceRowAction(item, context, 'withdraw'))).toEqual([pending])
    expect([draft, pending].filter(item => canExecuteGovernanceRowAction(item, context, 'delete'))).toEqual([draft])
  })
})
