import pandas as pd

from structured_query.profile import _select_value_counts, profile_frame, to_mschema_context


def test_low_cardinality_keeps_all_real_values():
    frame = pd.DataFrame({"c_001": ["软件测评师", "软件评测师", "软件评测师"]})
    profiles = profile_frame(frame, {"c_001": "证书"}, 20, 5)
    assert {(value.value, value.frequency) for value in profiles[0].values} == {
        ("软件测评师", 1),
        ("软件评测师", 2),
    }
    context = to_mschema_context("kb", "d_1", "人员", profiles)
    assert "c_001:证书" in context
    assert "软件测评师" in context


def test_high_cardinality_sample_keeps_frequent_and_diverse_values():
    counts = pd.Series([9, 8, 7, 6, 5, 4], index=["常用A", "常用B", "中段C", "中段D", "尾部E", "尾部F"])

    selected = _select_value_counts(counts, 4, complete=False)

    assert selected[:2] == [("常用A", 9), ("常用B", 8)]
    assert len(selected) == 4
    assert any(value in {"尾部E", "尾部F"} for value, _ in selected)
