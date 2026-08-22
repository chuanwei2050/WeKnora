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

export interface KnowledgeDeleteOptions {
  canManage?: boolean
  currentUserId?: string
}

export function getGovernanceRowActions(item: GovernanceRowItem, context: GovernanceActionContext): GovernanceRowAction[] {
  if (!context.enabled || !item.pending_version_id) return []

  const isOwnContribution = item.created_by === context.currentUserId
  if (isOwnContribution && context.canContribute) {
    if (item.parse_status === 'pending_review') return ['withdraw']
    if (item.parse_status === 'draft' || item.parse_status === 'rejected') {
      return item.current_version_id ? ['submit'] : ['submit', 'delete']
    }
  }
  if (!isOwnContribution && context.canReview && item.parse_status === 'pending_review') {
    return ['approve', 'reject']
  }
  return []
}

export function canContributorDeleteKnowledge(item: GovernanceRowItem, currentUserId?: string): boolean {
  if (!currentUserId || item.created_by !== currentUserId) return false
  if (item.current_version_id) return false
  if (item.parse_status === 'pending_review') return false
  return item.parse_status === 'draft' || item.parse_status === 'rejected'
}

export function isKnowledgeDeleteDisabled(item: GovernanceRowItem, options?: KnowledgeDeleteOptions): boolean {
  if (options?.canManage) return false
  return !canContributorDeleteKnowledge(item, options?.currentUserId)
}

export function isGovernanceRowActionDisabled(
  item: GovernanceRowItem,
  action: GovernanceRowAction,
  options?: KnowledgeDeleteOptions,
): boolean {
  return action === 'delete' && isKnowledgeDeleteDisabled(item, options)
}

export function canExecuteGovernanceRowAction(
  item: GovernanceRowItem,
  context: GovernanceActionContext,
  action: GovernanceRowAction,
  options?: KnowledgeDeleteOptions,
): boolean {
  return getGovernanceRowActions(item, context).includes(action)
    && !isGovernanceRowActionDisabled(item, action, options)
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
