export type GovernanceReviewAction = 'submit' | 'withdraw' | 'approve' | 'reject'
export type GovernanceRowAction = GovernanceReviewAction | 'delete'

export interface DocumentBatchAction {
  action: GovernanceRowAction
  count: number
}

export interface GovernanceRowItem {
  created_by?: string
  current_version_id?: string
  pending_version_id?: string
  parse_status?: string
}

interface GovernanceActionContext {
  enabled: boolean
  canContribute: boolean
  canReview: boolean
  currentUserId: string
}

export function getGovernanceRowActions(item: GovernanceRowItem, context: GovernanceActionContext): GovernanceRowAction[] {
  if (!context.enabled || !item.pending_version_id) return []

  const isOwnContribution = item.created_by === context.currentUserId
  if (isOwnContribution && context.canContribute) {
    if (item.parse_status === 'pending_review') return item.current_version_id ? ['withdraw'] : ['withdraw', 'delete']
    if (item.parse_status === 'draft' || item.parse_status === 'rejected') {
      return item.current_version_id ? ['submit'] : ['submit', 'delete']
    }
  }
  if (!isOwnContribution && context.canReview && item.parse_status === 'pending_review') {
    return ['approve', 'reject']
  }
  return []
}

export function isGovernanceRowActionDisabled(item: GovernanceRowItem, action: GovernanceRowAction): boolean {
  return action === 'delete' && item.parse_status === 'pending_review'
}

export function canExecuteGovernanceRowAction(
  item: GovernanceRowItem,
  context: GovernanceActionContext,
  action: GovernanceRowAction,
): boolean {
  return getGovernanceRowActions(item, context).includes(action) && !isGovernanceRowActionDisabled(item, action)
}

export function canOperateGovernanceRow(item: GovernanceRowItem, context: GovernanceActionContext): boolean {
  return getGovernanceRowActions(item, context).some(action => canExecuteGovernanceRowAction(item, context, action))
}

const governanceActionNextStatus: Record<GovernanceReviewAction, string> = {
  submit: 'pending_review',
  withdraw: 'draft',
  approve: 'pending',
  reject: 'rejected',
}

export function getGovernanceActionNextStatus(action: GovernanceReviewAction): string {
  return governanceActionNextStatus[action]
}
