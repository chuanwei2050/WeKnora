"""Smoke test for calamine-based ExcelParser (no package __init__ side effects)."""
from __future__ import annotations

import importlib.util
import sys
import types
from io import BytesIO
from pathlib import Path
from zipfile import ZIP_DEFLATED, ZipFile

ROOT = Path(__file__).resolve().parents[1]


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    assert spec and spec.loader
    mod = importlib.util.module_from_spec(spec)
    sys.modules[name] = mod
    spec.loader.exec_module(mod)
    return mod


# Fake package shells so relative-style imports inside modules resolve.
docreader_pkg = types.ModuleType("docreader")
docreader_pkg.__path__ = [str(ROOT)]
sys.modules["docreader"] = docreader_pkg

models_pkg = types.ModuleType("docreader.models")
models_pkg.__path__ = [str(ROOT / "models")]
sys.modules["docreader.models"] = models_pkg

parser_pkg = types.ModuleType("docreader.parser")
parser_pkg.__path__ = [str(ROOT / "parser")]
sys.modules["docreader.parser"] = parser_pkg

load("docreader.models.document", ROOT / "models" / "document.py")
load("docreader.parser.base_parser", ROOT / "parser" / "base_parser.py")
excel = load("docreader.parser.excel_parser", ROOT / "parser" / "excel_parser.py")
ExcelParser = excel.ExcelParser


def minimal_xlsx() -> bytes:
    buf = BytesIO()
    with ZipFile(buf, "w", ZIP_DEFLATED) as z:
        z.writestr(
            "[Content_Types].xml",
            """<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
  <Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>
</Types>""",
        )
        z.writestr(
            "_rels/.rels",
            """<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>""",
        )
        z.writestr(
            "xl/workbook.xml",
            """<?xml version="1.0" encoding="UTF-8"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets><sheet name="Patents" sheetId="1" r:id="rId1"/></sheets>
</workbook>""",
        )
        z.writestr(
            "xl/_rels/workbook.xml.rels",
            """<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>""",
        )
        z.writestr(
            "xl/sharedStrings.xml",
            """<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="4" uniqueCount="4">
  <si><t>Name</t></si><si><t>ID</t></si><si><t>Alpha|Beta</t></si><si><t>1</t></si>
</sst>""",
        )
        z.writestr(
            "xl/worksheets/sheet1.xml",
            """<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>
    <row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2" t="s"><v>3</v></c></row>
    <row r="3"/>
  </sheetData>
</worksheet>""",
        )
    return buf.getvalue()


def main() -> None:
    doc = ExcelParser(file_name="t.xlsx").parse_into_text(minimal_xlsx())
    md = doc.content
    assert "## Patents" not in md, md
    assert "| Name | ID |" not in md, md
    assert "Name: Alpha|Beta,ID: 1" in md, md
    assert len(doc.chunks) == 1, doc.chunks
    assert doc.chunks[0].content.strip() == "Name: Alpha|Beta,ID: 1", doc.chunks[0].content
    print("OK")
    print(md)
    print("chunks", len(doc.chunks))


if __name__ == "__main__":
    main()
