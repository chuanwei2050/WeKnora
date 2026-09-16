"""LLM prompts for docreader VLM OCR.

Aligned with Go internal/prompt.VLMOCR (Markdown body text; ignore headers/footers;
tables; formulas; reading order). Chinese wording kept for this pipeline.
"""

VLMOCR = (
    "提取文档图片正文为 Markdown："
    "忽略页眉页脚；表格用 Markdown 表；公式用 LaTeX；按阅读顺序；只输出正文。"
)
