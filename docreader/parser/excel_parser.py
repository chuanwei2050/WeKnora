"""
Excel Parser Module

Parses .xlsx/.xls via python-calamine (Rust), then emits one key-value line
per data row so Go can index person/certificate rows as atomic retrieval units.

Does not emit Markdown tables: length-based merging of table rows was burying
name rows under statistical sheets during RAG TopK.
"""
import logging
from datetime import date, datetime, time
from io import BytesIO
from typing import Any, List, Sequence

from python_calamine import CalamineWorkbook

from docreader.models.document import Chunk, Document
from docreader.parser.base_parser import BaseParser

logger = logging.getLogger(__name__)


def _cell_to_str(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, datetime):
        return value.isoformat(sep=" ", timespec="seconds")
    if isinstance(value, date):
        return value.isoformat()
    if isinstance(value, time):
        return value.isoformat(timespec="seconds")
    text = str(value).strip()
    return text.replace("\r\n", " ").replace("\n", " ")


def _is_empty_row(row: Sequence[Any]) -> bool:
    for value in row:
        if value is None:
            continue
        if isinstance(value, str) and not value.strip():
            continue
        return False
    return True


def _row_to_kv(headers: List[str], row: List[str]) -> str:
    pairs: List[str] = []
    width = max(len(headers), len(row))
    for i in range(width):
        header = headers[i] if i < len(headers) else f"col_{i + 1}"
        value = row[i] if i < len(row) else ""
        if not header and not value:
            continue
        if not value:
            continue
        if not header:
            header = f"col_{i + 1}"
        pairs.append(f"{header}: {value}")
    return ",".join(pairs)


def _open_workbook(content: bytes) -> CalamineWorkbook:
    buffer = BytesIO(content)
    if hasattr(CalamineWorkbook, "from_filelike"):
        return CalamineWorkbook.from_filelike(buffer)
    return CalamineWorkbook.from_object(buffer)


class ExcelParser(BaseParser):
    """Parser for Excel files (.xlsx, .xls).

    Converts each sheet into:
        ## SheetName
        col1: v1,col2: v2
        col1: v3,col2: v4

    First non-empty row is the header. Empty rows are skipped. Each data row
    becomes one line (and one Document.chunk) for Go tabular row splitting.
    """

    def parse_into_text(self, content: bytes) -> Document:
        workbook = _open_workbook(content)
        text: List[str] = []
        chunks: List[Chunk] = []
        start = 0

        for sheet_name in workbook.sheet_names:
            header_line = f"## {sheet_name}\n"
            end = start + len(header_line)
            text.append(header_line)
            chunks.append(Chunk(content=header_line, seq=len(chunks), start=start, end=end))
            start = end

            raw_rows = workbook.get_sheet_by_name(sheet_name).to_python()
            rows: List[List[str]] = []
            for raw in raw_rows:
                if _is_empty_row(raw):
                    continue
                rows.append([_cell_to_str(value) for value in raw])
            if not rows:
                continue

            headers = rows[0]
            for row in rows[1:]:
                kv = _row_to_kv(headers, row)
                if not kv:
                    continue
                content_row = kv + "\n"
                end = start + len(content_row)
                text.append(content_row)
                chunks.append(
                    Chunk(content=content_row, seq=len(chunks), start=start, end=end)
                )
                start = end

        return Document(content="".join(text), chunks=chunks)


if __name__ == "__main__":
    logging.basicConfig(level=logging.DEBUG)
    your_file = "/path/to/your/file.xlsx"
    parser = ExcelParser(file_name=your_file)
    with open(your_file, "rb") as f:
        document = parser.parse_into_text(f.read())
        logger.error(document.content[:2000])
