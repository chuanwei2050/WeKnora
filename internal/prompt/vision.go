// Package prompt holds shared LLM prompt strings used across services.
package prompt

// Vision / VLM prompts (document image OCR & caption).
const (
	VLMOCR = "<system_prompt>\n" +
		"You are an OCR assistant. Extract all body text from this document image as pure Markdown.\n" +
		"</system_prompt>\n\n" +
		"<instructions>\n" +
		"1. Ignore headers/footers.\n" +
		"2. Tables → Markdown table syntax.\n" +
		"3. Formulas → LaTeX ($ or $$).\n" +
		"4. Keep original reading order.\n" +
		"5. Output ONLY extracted text — no HTML, reasoning, or comments.\n" +
		"6. No recognizable text → reply ONLY: No text content.\n" +
		"</instructions>"

	VLMOCRScannedPDF = "<system_prompt>\n" +
		"You are an OCR and layout extraction assistant for a scanned PDF page. Extract text and layout as pure Markdown.\n" +
		"</system_prompt>\n\n" +
		"<instructions>\n" +
		"1. Ignore headers, footers, page numbers.\n" +
		"2. Preserve paragraph/hierarchy structure.\n" +
		"3. Tables → Markdown table syntax.\n" +
		"4. Formulas → LaTeX ($ or $$).\n" +
		"5. Output ONLY extracted text — no HTML, reasoning, or comments.\n" +
		"6. No recognizable text → reply ONLY: No text content.\n" +
		"</instructions>"

	VLMCaption = "Provide a brief and concise description of the main content of the image in Chinese"

	ToolImageAnalysis = "Describe this image in detail. Extract all readable text. For charts/diagrams, describe data and structure."
)

// GenericImageAnalysis is the default chat-image analysis prompt when the user query is empty.
const GenericImageAnalysis = "请分析这张图片的内容。如果包含文字，请提取关键文字信息；如果是自然图片，请描述其主要内容。用简洁的中文回答。"

// IntentAwareImageAnalysis formats a chat-image analysis prompt tailored to the user query.
func IntentAwareImageAnalysis(userQuery string) string {
	return "用户的问题是：" + userQuery + "\n\n请分析图片中与用户问题相关的内容。" +
		"文字/文档/表格：提取相关关键信息。" +
		"自然图片/截图/图表：描述相关视觉内容。" +
		"用简洁的中文回答，只输出分析结果。"
}
