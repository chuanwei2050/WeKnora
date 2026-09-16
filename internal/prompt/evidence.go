package prompt

import "fmt"

// StructuredNoLeakRules is shared by agent final-answer synthesis and verified-answer rewrite.
const StructuredNoLeakRules = "Do not expose physical table names, internal identifiers, internally generated aliases, or raw result payloads. Convert structured results into natural language. Do not show the structured-query process or raw result shapes. If the user asks about SQL, table structure, or business column names, answer; otherwise do not expose SQL or the query process."

// FinalAnswerNoLeakRules extends StructuredNoLeakRules for agent finalize.
const FinalAnswerNoLeakRules = "Never expose internal identifiers, raw retrieval payloads, tool names, system metadata, physical table names, or internally generated aliases. Convert structured results into natural language. Do not show the structured-query process or raw result shapes. If the user asks about SQL, table structure, or business column names, answer; otherwise do not expose SQL or describe the query process."

// VerificationValidatorSystem is the system prompt for independent answer validation.
const VerificationValidatorSystem = "You are an independent answer validator. Score the draft against evidence. Return exactly one JSON object (no markdown) with keys fact_score, logic_score, citation_score, completeness_score (each 0..1), and issues (array; empty if none). Each issue: existing draft claim_id (usually \"answer-claim\"), existing evidence_ids, dimension in {fact,logic,citation,completeness}, severity in {info,warning,critical}, short message. Do not invent IDs. No chain-of-thought, fences, or prose."

// VerificationRewriteSystem is used after validation finds issues.
const VerificationRewriteSystem = "Rewrite the answer after validation issues. Use only supplied evidence; fix unsupported/incomplete claims; return only the revised answer. " +
	StructuredNoLeakRules +
	" No analysis, validator commentary, chain-of-thought, or confidence explanation."

// BuildFinalAnswerPrompt synthesizes the user-facing final answer from retrieved evidence.
func BuildFinalAnswerPrompt(query string) string {
	return fmt.Sprintf(`Based only on the retrieved evidence above, answer the user's question.

User question: %s

Requirements:
1. Only conclusions supported by evidence; if insufficient, say so.
2. Cite by user-facing document names when useful. %s
3. Output only the final answer — no analysis, plans, retrieval steps, or intent restatement.
4. Concise, structured, same language as the question.

只输出面向用户的最终答案，不要输出分析过程、检索过程或任何内部标识。`, query, FinalAnswerNoLeakRules)
}

// AgentMemoryConsolidationSystem summarizes conversation history for context compression.
const AgentMemoryConsolidationSystem = "" +
	"You are a conversation summarizer. Create a concise summary of a user–assistant conversation.\n\n" +
	"Checklist:\n" +
	"- Same language as the conversation\n" +
	"- Preserve key facts, numbers, details\n" +
	"- Include tool outcomes and errors\n" +
	"- Section by topic if multi-topic\n" +
	"- ≤30% of original length\n\n" +
	"Output only the summary."
