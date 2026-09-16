from io import BytesIO
from zipfile import ZipFile

import pandas as pd
import pytest

from structured_query.ingestion import _validate_xlsx_archive, normalize_frame, parse_tabular_file, promote_header_row


def test_normalize_preserves_chinese_and_duplicate_headers():
    frame = pd.DataFrame([["张三", "研发"]], columns=["姓名", "姓名"])
    normalized, mapping = normalize_frame(frame)
    assert list(normalized.columns) == ["c_001", "c_002"]
    assert mapping == {"c_001": "姓名", "c_002": "姓名#2"}


def test_promote_header_when_first_row_is_title_and_columns_are_numeric():
    frame = pd.DataFrame(
        [["姓名", "年龄", "学历"], ["张三", 30, "硕士"], ["李四", 25, "本科"]],
        columns=[0, 1, 2],
    )
    promoted = promote_header_row(frame)
    assert list(promoted.columns) == ["姓名", "年龄", "学历"]
    assert promoted.iloc[0].tolist() == ["张三", 30, "硕士"]

    normalized, mapping = normalize_frame(frame)
    assert mapping == {"c_001": "姓名", "c_002": "年龄", "c_003": "学历"}
    assert normalized.iloc[0].tolist()[:1] == ["张三"]


def test_promote_header_does_not_steal_headerless_data_row():
    """Numeric placeholders + Chinese data must not promote the first person row."""
    frame = pd.DataFrame(
        [["张三", "硕士"], ["李四", "本科"], ["王五", "博士"]],
        columns=[0, 1],
    )
    promoted = promote_header_row(frame)
    assert list(promoted.columns) == [0, 1]
    assert len(promoted) == 3
    assert promoted.iloc[0].tolist() == ["张三", "硕士"]


def test_promote_header_skips_when_year_columns_already_descriptive_enough():
    frame = pd.DataFrame(
        [["产品A", 10], ["产品B", 20]],
        columns=["品名", "2024"],
    )
    promoted = promote_header_row(frame)
    assert list(promoted.columns) == ["品名", "2024"]
    assert len(promoted) == 2


def test_parse_gb18030_csv():
    content = "姓名,学历\n张三,硕士\n".encode("gb18030")
    tables = parse_tabular_file("人员.csv", BytesIO(content))
    assert tables[0].sheet_name == "人员"
    assert tables[0].frame.iloc[0].tolist() == ["张三", "硕士"]


def test_parse_single_column_csv_without_detectable_delimiter():
    table = parse_tabular_file("名单.csv", BytesIO("姓名\n张三\n".encode()))[0]
    assert table.frame.iloc[0].tolist() == ["张三"]


def test_mixed_type_column_falls_back_without_losing_values():
    content = "编号,值\n1,100\n2,文字\n3,2026-01-01\n".encode()
    table = parse_tabular_file("mixed.csv", BytesIO(content))[0]
    assert table.column_mapping == {"c_001": "编号", "c_002": "值"}
    assert table.frame["c_002"].astype(str).tolist() == ["100", "文字", "2026-01-01"]


def test_xlsx_archive_is_rejected_before_expansion_limit():
    stream = BytesIO()
    with ZipFile(stream, "w") as archive:
        archive.writestr("xl/worksheets/sheet1.xml", "x" * 128)

    with pytest.raises(ValueError, match="excel_uncompressed_size_exceeded"):
        _validate_xlsx_archive(stream.getvalue(), max_uncompressed_bytes=64, max_entries=10)


def test_csv_row_limit_stops_during_parse():
    content = "姓名,学历\n张三,硕士\n李四,本科\n".encode()

    with pytest.raises(ValueError, match="too_many_rows"):
        parse_tabular_file("人员.csv", BytesIO(content), max_rows=1)
