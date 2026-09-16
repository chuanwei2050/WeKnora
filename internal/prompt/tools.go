package prompt

import "strconv"

// Tool / pipeline prompts used outside the main YAML template catalog.

const (
	WebFetchSystem = "Answer the user's request from the provided web page text only. Never fabricate information absent from the text."

	WebFetchUser = "User request:\n%s\n\nWeb page content:\n%s"

	LLMRerankSystem = "You are a search reranking expert. Score passage relevance to the query (does it answer / provide needed info). Scores only — no explanations."

	FinalAnswerToolNudge = "Please provide your answer by calling the final_answer tool."

	ComplexityJSONFieldsAppend = "\nReturn JSON fields complexity_level (L1/L2/L3/L4), reasoning_subtype (one of explicit_fact, contextual_fact, comparison, multi_hop, causal, hypothetical, transfer, unknown), needs_entity_relation (true only when entity relations, hierarchy, or multi-hop graph reasoning is needed), confidence (0..1), and rationale_summary (one short sentence, no chain-of-thought)."

	MultiImageDescriptionsAppend = "\n当用户上传多张图片时，请额外返回 image_descriptions 数组，按图片出现顺序逐项描述；数组长度必须与图片数量一致。image_description 仍返回所有图片的合并说明。"
)

// SubQuestionPlan builds the Chinese planner prompt for multi-hop retrieval.
func SubQuestionPlan(maxQuestions int, query string) string {
	return "将用户问题拆解为有序、有限的检索子问题。只输出一个 JSON 对象，不要 Markdown、解释或思维过程。\n" +
		"每个子问题字段为 index、query、depends_on、required；index 从 1 开始，depends_on 只能引用更早的 index。\n" +
		"仅在确实需要多步证据时返回多个子问题；简单问题返回一个子问题。后续子问题不得使用未解析的代词，必须能结合原问题和前序结果独立检索。\n" +
		"最多 " + strconv.Itoa(maxQuestions) + " 个子问题。原问题：" + query
}

// LLMRerankUser is the scoring rubric + passages payload for LLM rerank.
func LLMRerankUser(query, passages string, count int) string {
	n := strconv.Itoa(count)
	return "Rerank passages for query relevance.\n\n" +
		"User Query: " + query + "\n\n" +
		"Score 0.0–1.0: 0.9–1.0 direct answer · 0.7–0.8 strong · 0.5–0.6 partial · 0.3–0.4 weak · 0.1–0.2 barely · 0.0 irrelevant.\n\n" +
		"Retrieved Passages:\n" + passages + "\n\n" +
		"Return exactly " + n + " scores, one per line:\n" +
		"Passage 1: X.XX\nPassage 2: X.XX\n...\nPassage " + n + ": X.XX\n\n" +
		"Scores only — no explanations."
}
