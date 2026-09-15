"""
Excel Parser Module

Parses .xlsx/.xls into markdown tables via python-calamine (Rust), then returns
plain markdown for the Go app to chunk. Does not emit per-row chunks.
"""
import logging
from datetime import date, datetime, time
from io import BytesIO
from typing import Any, List, Sequence

from python_calamine import CalamineWorkbook

from docreader.models.document import Document
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
    return text.replace("\r\n", " ").replace("\n", " ").replace("|", "\\|")


def _is_empty_row(row: Sequence[Any]) -> bool:
    for value in row:
        if value is None:
            continue
        if isinstance(value, str) and not value.strip():
            continue
        return False
    return True


def _to_markdown_table(rows: List[List[str]]) -> str:
    if not rows:
        return ""

    width = max(len(row) for row in rows)
    normalized = [row + [""] * (width - len(row)) for row in rows]

    while width > 0 and all(not row[width - 1] for row in normalized):
        width -= 1
        for row in normalized:
            row.pop()
    if width == 0:
        return ""

    header = normalized[0]
    lines = [
        "| " + " | ".join(header) + " |",
        "| " + " | ".join("---" for _ in header) + " |",
    ]
    for row in normalized[1:]:
        lines.append("| " + " | ".join(row) + " |")
    return "\n".join(lines)


def _open_workbook(content: bytes) -> CalamineWorkbook:
    buffer = BytesIO(content)
    if hasattr(CalamineWorkbook, "from_filelike"):
        return CalamineWorkbook.from_filelike(buffer)
    return CalamineWorkbook.from_object(buffer)


class ExcelParser(BaseParser):
    """Parser for Excel files (.xlsx, .xls).

    Converts each sheet into a markdown section:
        ## SheetName
        | col1 | col2 |
        | --- | --- |
        | a | b |

    First non-empty row is treated as the table header. Completely empty rows
    are skipped. Chunking is left to the Go app.
    """

    def parse_into_text(self, content: bytes) -> Document:
        workbook = _open_workbook(content)
        parts: List[str] = []

        for sheet_name in workbook.sheet_names:
            parts.append(f"## {sheet_name}")
            raw_rows = workbook.get_sheet_by_name(sheet_name).to_python()
            rows: List[List[str]] = []
            for raw in raw_rows:
                if _is_empty_row(raw):
                    continue
                rows.append([_cell_to_str(value) for value in raw])

            table = _to_markdown_table(rows)
            if table:
                parts.append("")
                parts.append(table)
            parts.append("")

        markdown = "\n".join(parts).strip()
        if markdown:
            markdown += "\n"
        return Document(content=markdown)


if __name__ == "__main__":
    logging.basicConfig(level=logging.DEBUG)
    your_file = "/path/to/your/file.xlsx"
    parser = ExcelParser(file_name=your_file)
    with open(your_file, "rb") as f:
        document = parser.parse_into_text(f.read())
        logger.error(document.content[:2000])
