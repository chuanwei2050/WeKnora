from dataclasses import dataclass
from csv import Error as CSVError
from io import BytesIO
from pathlib import Path
from typing import BinaryIO
from zipfile import BadZipFile, ZipFile

import pandas as pd
from charset_normalizer import from_bytes
from python_calamine import CalamineWorkbook


SUPPORTED_EXTENSIONS = {".csv", ".xls", ".xlsx"}


@dataclass(frozen=True)
class ParsedTable:
    sheet_name: str
    frame: pd.DataFrame
    column_mapping: dict[str, str]


def physical_column_name(ordinal: int) -> str:
    return f"c_{ordinal:03d}"


def normalize_frame(frame: pd.DataFrame) -> tuple[pd.DataFrame, dict[str, str]]:
    normalized = frame.copy()
    mapping: dict[str, str] = {}
    physical_names: list[str] = []
    seen_original: dict[str, int] = {}
    for ordinal, raw in enumerate(frame.columns, start=1):
        original = str(raw).strip() or f"未命名列{ordinal}"
        occurrence = seen_original.get(original, 0) + 1
        seen_original[original] = occurrence
        display = original if occurrence == 1 else f"{original}#{occurrence}"
        physical = physical_column_name(ordinal)
        mapping[physical] = display
        physical_names.append(physical)
    normalized.columns = physical_names
    normalized = normalized.dropna(how="all").reset_index(drop=True)
    return normalized, mapping


def _read_csv(content: bytes, max_rows: int | None, max_columns: int | None) -> pd.DataFrame:
    detected = from_bytes(content[: min(len(content), 256_000)]).best()
    encodings = ["utf-8-sig", "gb18030"]
    if detected is not None and detected.encoding:
        encodings.append(detected.encoding)
    last_error: Exception | None = None
    for encoding in dict.fromkeys(encodings):
        try:
            try:
                frame = pd.read_csv(
                    BytesIO(content), encoding=encoding, sep=None, engine="python",
                    nrows=None if max_rows is None else max_rows + 1,
                )
            except CSVError:
                # Sniffer cannot infer a delimiter for valid one-column CSV files.
                frame = pd.read_csv(
                    BytesIO(content), encoding=encoding, sep=",",
                    nrows=None if max_rows is None else max_rows + 1,
                )
            if max_rows is not None and len(frame) > max_rows:
                raise ValueError("too_many_rows")
            if max_columns is not None and len(frame.columns) > max_columns:
                raise ValueError("too_many_columns")
            return frame
        except (UnicodeDecodeError, pd.errors.ParserError) as error:
            last_error = error
    raise ValueError("unable_to_parse_csv") from last_error


def _validate_xlsx_archive(content: bytes, max_uncompressed_bytes: int, max_entries: int) -> None:
    try:
        with ZipFile(BytesIO(content)) as archive:
            entries = archive.infolist()
            if len(entries) > max_entries:
                raise ValueError("too_many_archive_entries")
            if sum(entry.file_size for entry in entries) > max_uncompressed_bytes:
                raise ValueError("excel_uncompressed_size_exceeded")
    except BadZipFile as error:
        raise ValueError("invalid_xlsx_archive") from error


def parse_tabular_file(
    file_name: str,
    stream: BinaryIO,
    *,
    max_sheets: int | None = None,
    max_rows: int | None = None,
    max_columns: int | None = None,
    max_excel_uncompressed_bytes: int = 512 * 1024 * 1024,
    max_excel_archive_entries: int = 10_000,
) -> list[ParsedTable]:
    suffix = Path(file_name).suffix.lower()
    if suffix not in SUPPORTED_EXTENSIONS:
        raise ValueError("unsupported_file_type")
    content = stream.read()
    if suffix == ".csv":
        frames = {Path(file_name).stem or "Sheet1": _read_csv(content, max_rows, max_columns)}
    else:
        if suffix == ".xlsx":
            _validate_xlsx_archive(content, max_excel_uncompressed_bytes, max_excel_archive_entries)
        metadata = CalamineWorkbook.from_filelike(BytesIO(content))
        try:
            if max_sheets is not None and len(metadata.sheet_names) > max_sheets:
                raise ValueError("too_many_sheets")
            for index in range(len(metadata.sheet_names)):
                sheet = metadata.get_sheet_by_index(index)
                # Height includes the header row consumed by pandas.
                if max_rows is not None and max(0, sheet.height - 1) > max_rows:
                    raise ValueError("too_many_rows")
                if max_columns is not None and sheet.width > max_columns:
                    raise ValueError("too_many_columns")
        finally:
            metadata.close()
        workbook = pd.ExcelFile(BytesIO(content), engine="calamine")
        frames = {sheet_name: workbook.parse(sheet_name) for sheet_name in workbook.sheet_names}
    parsed: list[ParsedTable] = []
    for sheet_name, frame in frames.items():
        normalized, mapping = normalize_frame(frame)
        if not normalized.empty and len(normalized.columns) > 0:
            parsed.append(ParsedTable(str(sheet_name), normalized, mapping))
    if not parsed:
        raise ValueError("no_tabular_data")
    return parsed
