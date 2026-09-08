import io
import logging
import zipfile
from xml.etree import ElementTree

from docreader.models.document import Document
from docreader.parser.base_parser import BaseParser

logger = logging.getLogger(__name__)


class LargeDocxTextParser(BaseParser):
    """Low-memory DOCX text extraction for oversized documents.

    Only WordprocessingML text parts are opened. Embedded media is deliberately
    skipped because decoding every image from a very large DOCX can exhaust the
    parser container before any searchable text is produced.
    """

    _TEXT_PARTS = ("word/document.xml",)

    def parse_into_text(self, content: bytes) -> Document:
        paragraphs: list[str] = []
        with zipfile.ZipFile(io.BytesIO(content)) as archive:
            names = set(archive.namelist())
            parts = list(self._TEXT_PARTS)
            parts.extend(
                sorted(
                    name
                    for name in names
                    if name.startswith("word/header") or name.startswith("word/footer")
                )
            )
            for part in parts:
                if part not in names:
                    continue
                paragraphs.extend(self._read_part(archive, part))

        text = "\n".join(line for line in paragraphs if line)
        logger.info(
            "Large DOCX text-only extraction completed: file=%s, chars=%d",
            self.file_name,
            len(text),
        )
        return Document(
            content=text,
            metadata={
                "large_document_mode": "text_only",
                "images_skipped": "true",
            },
        )

    @staticmethod
    def _read_part(archive: zipfile.ZipFile, part: str) -> list[str]:
        lines: list[str] = []
        current: list[str] = []
        with archive.open(part) as xml_file:
            for event, element in ElementTree.iterparse(xml_file, events=("end",)):
                tag = element.tag.rsplit("}", 1)[-1]
                if tag == "t" and element.text:
                    current.append(element.text)
                elif tag == "tab":
                    current.append("\t")
                elif tag in {"br", "cr"}:
                    current.append("\n")
                elif tag == "tc":
                    current.append("\t")
                elif tag == "p":
                    line = "".join(current).strip()
                    if line:
                        lines.append(line)
                    current.clear()
                element.clear()
        if current:
            line = "".join(current).strip()
            if line:
                lines.append(line)
        return lines
