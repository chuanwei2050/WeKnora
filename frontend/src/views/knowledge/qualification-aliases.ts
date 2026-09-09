import type { QualificationAliasMapping } from '@/api/knowledge-base'

export type QualificationAliasValidation =
  | { kind: 'valid'; mappings: QualificationAliasMapping[] }
  | { kind: 'empty-field' }
  | { kind: 'duplicate'; alias: string }

export function validateQualificationAliases(
  input: QualificationAliasMapping[],
): QualificationAliasValidation {
  const mappings = input.map(mapping => ({
    alias: mapping.alias.trim(),
    standard_name: mapping.standard_name.trim(),
  }))
  const seen = new Set<string>()
  for (const mapping of mappings) {
    if (!mapping.alias || !mapping.standard_name) {
      return { kind: 'empty-field' }
    }
    const key = mapping.alias.toLocaleLowerCase()
    if (seen.has(key)) {
      return { kind: 'duplicate', alias: mapping.alias }
    }
    seen.add(key)
  }
  return { kind: 'valid', mappings }
}
