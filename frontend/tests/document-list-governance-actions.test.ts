import { describe, expect, it } from 'vitest'
import {
  canContributorDeleteKnowledge,
  canExecuteGovernanceRowAction,
  canOperateGovernanceRow,
  getGovernanceActionNextStatus,
  getGovernanceRowActions,
  isKnowledgeDeleteDisabled,
  isGovernanceRowActionDisabled,
} from '../src/views/knowledge/components/knowledge-governance-actions'

const draft = {
  created_by: 'user-1',
  pending_version_id: 'version-1',
  parse_status: 'draft',
}

const contributorOptions = { currentUserId: 'user-1' }
const adminOptions = { canManage: true, currentUserId: 'user-1' }

describe('getGovernanceRowActions', () => {
  it('allows the contributor to submit or delete an unused draft', () => {
    expect(getGovernanceRowActions(draft, {
      enabled: true,
      canContribute: true,
      canReview: false,
      currentUserId: 'user-1',
    })).toEqual(['submit', 'delete'])
  })

  it('allows the contributor to withdraw a pending submission without deleting it', () => {
    expect(getGovernanceRowActions({ ...draft, parse_status: 'pending_review' }, {
      enabled: true,
      canContribute: true,
      canReview: false,
      currentUserId: 'user-1',
    })).toEqual(['withdraw'])
    expect(isGovernanceRowActionDisabled({ ...draft, parse_status: 'pending_review' }, 'delete', contributorOptions)).toBe(true)
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
    expect(isKnowledgeDeleteDisabled({ ...draft, current_version_id: 'version-active' }, contributorOptions)).toBe(true)
  })

  it('allows admins to delete any document regardless of status', () => {
    const active = { ...draft, current_version_id: 'version-active', parse_status: 'completed' }
    const pending = { ...draft, parse_status: 'pending_review' }

    expect(isKnowledgeDeleteDisabled(active, adminOptions)).toBe(false)
    expect(isKnowledgeDeleteDisabled(pending, adminOptions)).toBe(false)
  })

  it('limits contributors to their own unsubmitted drafts or rejected items', () => {
    const active = { ...draft, current_version_id: 'version-active' }
    const pending = { ...draft, parse_status: 'pending_review' }
    const rejected = { ...draft, parse_status: 'rejected' }
    const otherDraft = { ...draft, created_by: 'user-2' }

    expect(canContributorDeleteKnowledge(draft, 'user-1')).toBe(true)
    expect(canContributorDeleteKnowledge(rejected, 'user-1')).toBe(true)
    expect(canContributorDeleteKnowledge(active, 'user-1')).toBe(false)
    expect(canContributorDeleteKnowledge(pending, 'user-1')).toBe(false)
    expect(canContributorDeleteKnowledge(otherDraft, 'user-1')).toBe(false)
  })

  it('calculates each batch action from eligible rows in a mixed selection', () => {
    const context = { enabled: true, canContribute: true, canReview: false, currentUserId: 'user-1' }
    const pending = { ...draft, pending_version_id: 'version-2', parse_status: 'pending_review' }

    expect([draft, pending].filter(item => canExecuteGovernanceRowAction(item, context, 'submit', contributorOptions))).toEqual([draft])
    expect([draft, pending].filter(item => canExecuteGovernanceRowAction(item, context, 'withdraw', contributorOptions))).toEqual([pending])
    expect([draft, pending].filter(item => canExecuteGovernanceRowAction(item, context, 'delete', contributorOptions))).toEqual([draft])
  })
})
