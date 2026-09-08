import io
import zipfile
from unittest.mock import patch

from docreader.parser.large_docx_parser import LargeDocxTextParser
from docreader.parser.parser import Parser


def _docx_bytes() -> bytes:
    target = io.BytesIO()
    with zipfile.ZipFile(target, "w") as archive:
        archive.writestr(
            "word/document.xml",
            """<?xml version="1.0" encoding="UTF-8"?>
            <w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
              <w:body>
                <w:p><w:r><w:t>第一段</w:t></w:r></w:p>
                <w:p><w:r><w:t>第二</w:t><w:tab/><w:t>列</w:t></w:r></w:p>
              </w:body>
            </w:document>""",
        )
        archive.writestr("word/media/large.bin", b"x" * 4096)
    return target.getvalue()


def test_large_docx_parser_extracts_text_without_media() -> None:
    result = LargeDocxTextParser(file_name="large.docx", file_type="docx").parse(
        _docx_bytes()
    )

    assert result.content == "第一段\n第二\t列"
    assert result.images == {}
    assert result.metadata["large_document_mode"] == "text_only"


def test_parser_routes_oversized_docx_to_text_only_mode() -> None:
    content = _docx_bytes()
    with patch("docreader.parser.parser.LARGE_DOCX_TEXT_ONLY_BYTES", 1):
        result = Parser().parse_file("large.docx", "docx", content)

    assert result.content == "第一段\n第二\t列"
    assert result.metadata["large_document_mode"] == "text_only"
