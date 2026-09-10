import pytest

from structured_query.sql_safety import (
    UnsafeSQL,
    normalize_unambiguous_column_names,
    remove_impossible_complete_profile_or_branches,
    validate_read_only_sql,
)


@pytest.mark.parametrize("sql", [
    "SELECT pg_read_file('/etc/passwd') FROM allowed",
    "SELECT load_file('/etc/passwd') FROM allowed",
    "SELECT sleep(10) FROM allowed",
    "SELECT query_to_xml('SELECT * FROM secret', true, true, '') FROM allowed",
])
def test_dangerous_read_only_functions_are_rejected(sql):
    with pytest.raises(UnsafeSQL) as failure:
        validate_read_only_sql(sql, {"allowed"})
    assert failure.value.code == "dangerous_function"


def test_accepts_single_read_only_query():
    result = validate_read_only_sql('SELECT COUNT(*) FROM "d_people"', {"d_people"})
    assert result.tables == {"d_people"}


def test_rejects_unrequested_near_duplicate_or_expansion():
    with pytest.raises(UnsafeSQL, match="ambiguous_synonym_expansion"):
        validate_read_only_sql(
            "SELECT COUNT(*) FROM people WHERE certificate ILIKE '%软件测评师%' OR certificate ILIKE '%软件评测师%'",
            {"people"},
            question_text="软件测评师（软考）有多少人",
        )


def test_allows_near_duplicate_categories_when_user_names_both():
    result = validate_read_only_sql(
        "SELECT COUNT(*) FROM people WHERE certificate ILIKE '%软件测评师%' OR certificate ILIKE '%软件评测师%'",
        {"people"},
        question_text="软件测评师或软件评测师分别多少人",
    )
    assert result.tables == {"people"}


@pytest.mark.parametrize(
    "sql,error",
    [
        ("DELETE FROM d_people", "read_only_required"),
        ("SELECT * FROM d_people; SELECT 1", "multiple_statements"),
        ("SELECT * FROM other", "unauthorized_table"),
        ("SELECT * FROM a JOIN b ON true JOIN c ON true JOIN d ON true", "too_many_tables"),
        ("SELECT * FROM d_people; DELETE FROM d_people", "multiple_statements"),
    ],
)
def test_rejects_unsafe_sql(sql, error):
    with pytest.raises(UnsafeSQL, match=error):
        validate_read_only_sql(sql, {"d_people", "a", "b", "c", "d"})


def test_mysql_dialect_preserves_qualified_authorization():
    result = validate_read_only_sql(
        "SELECT COUNT(*) FROM `hr`.`people`", {"hr.people"}, dialect="mysql"
    )
    assert result.tables == {"hr.people"}
    with pytest.raises(UnsafeSQL, match="unauthorized_table"):
        validate_read_only_sql("SELECT * FROM `other`.`people`", {"hr.people"}, dialect="mysql")


def test_normalizes_only_column_identifiers_not_string_values():
    sql = normalize_unambiguous_column_names(
        "SELECT 姓名 FROM people WHERE 学历 = '学历'", {"姓名": "c_004", "学历": "c_009"}
    )
    assert 'c_004' in sql and 'c_009' in sql
    assert "'学历'" in sql


def test_filter_literals_must_be_supported_by_profile_evidence():
    with pytest.raises(UnsafeSQL, match="unsupported_value_literal"):
        validate_read_only_sql(
            "SELECT * FROM people WHERE certificate ILIKE '%invented combined value%'",
            {"people"},
            supported_values_by_column={"certificate": "real stored value"},
        )


def test_supported_like_fragments_can_be_combined_without_exact_phrase():
    result = validate_read_only_sql(
        "SELECT * FROM people WHERE certificate ILIKE '%professional qualification%' AND certificate ILIKE '%reviewer%'",
        {"people"},
        supported_values_by_column={"certificate": "professional qualification, software reviewer"},
    )
    assert result.tables == {"people"}


def test_conditional_aggregate_literals_are_validated_too():
    with pytest.raises(UnsafeSQL, match="unsupported_value_literal"):
        validate_read_only_sql(
            "SELECT SUM(CASE WHEN certificate ILIKE '%invented%' THEN 1 ELSE 0 END) FROM people",
            {"people"},
            supported_values_by_column={"certificate": "real stored value"},
        )


def test_unsupported_literal_exposes_only_bounded_profile_repair_evidence():
    with pytest.raises(UnsafeSQL) as failure:
        validate_read_only_sql(
            "SELECT * FROM people WHERE certificate ILIKE '%software reviewer exam%'",
            {"people"},
            supported_values_by_column={"certificate": "official qualification software reviewer, number 123"},
        )
    assert failure.value.code == "unsupported_value_literal"
    assert "softwarereviewer" in failure.value.repair_hint
    assert "supported_overlap=softwarereviewer" in failure.value.repair_hint
    assert len(failure.value.repair_hint) < 200


def test_removes_only_impossible_or_branch_for_complete_profile():
    sql = remove_impossible_complete_profile_or_branches(
        "SELECT * FROM people WHERE certificate ILIKE '%invented qualifier%' OR certificate ILIKE '%stored value%'",
        {"certificate": "stored value, another value"},
        {"certificate"},
    )
    assert "invented qualifier" not in sql
    assert "stored value" in sql


def test_does_not_simplify_sampled_profile_or_branch():
    original = "SELECT * FROM people WHERE certificate ILIKE '%possibly unsampled%' OR certificate ILIKE '%stored value%'"
    sql = remove_impossible_complete_profile_or_branches(
        original, {"certificate": "stored value"}, set()
    )
    assert "possibly unsampled" in sql


def test_repair_hint_keeps_bounded_evidence_for_multiple_invalid_alternatives():
    with pytest.raises(UnsafeSQL) as failure:
        validate_read_only_sql(
            "SELECT * FROM people WHERE certificate ILIKE '%reviewer exam%' OR certificate ILIKE '%reviewer training%'",
            {"people"},
            supported_values_by_column={
                "certificate": "official qualification software reviewer"
            },
        )
    assert failure.value.repair_hint.count("column=certificate") == 2
    assert len(failure.value.repair_hint) < 384


def test_qualified_profile_evidence_does_not_cross_same_named_columns():
    with pytest.raises(UnsafeSQL, match="unsupported_value_literal"):
        validate_read_only_sql(
            "SELECT * FROM people p JOIN training t ON p.id=t.person_id "
            "WHERE p.certificate ILIKE '%training-only%'",
            {"people", "training"},
            supported_values_by_column={
                "people.certificate": "official qualification",
                "training.certificate": "training-only",
            },
        )


def test_unqualified_same_named_column_is_not_validated_against_arbitrary_table():
    result = validate_read_only_sql(
        "SELECT certificate FROM people JOIN training ON people.id=training.person_id",
        {"people", "training"},
        supported_values_by_column={
            "people.certificate": "official qualification",
            "training.certificate": "training-only",
        },
    )
    assert result.tables == {"people", "training"}


def test_incomplete_profile_can_use_bounded_runtime_literal_probe():
    probes = []
    result = validate_read_only_sql(
        "SELECT * FROM people WHERE certificate ILIKE '%rare certificate%'",
        {"people"},
        supported_values_by_column={"people.certificate": "frequent certificate"},
        probeable_columns={"people.certificate"},
        literal_support_probe=lambda column, fragment: probes.append((column, fragment)) or True,
    )
    assert result.tables == {"people"}
    assert probes == [("people.certificate", "rarecertificate")]


def test_complete_profile_never_uses_runtime_probe():
    with pytest.raises(UnsafeSQL, match="unsupported_value_literal"):
        validate_read_only_sql(
            "SELECT * FROM people WHERE certificate ILIKE '%invented%'",
            {"people"},
            supported_values_by_column={"people.certificate": "known"},
            probeable_columns=set(),
            literal_support_probe=lambda *_: (_ for _ in ()).throw(AssertionError("must not probe")),
        )


def test_stale_profile_rechecks_even_previously_supported_literal():
    with pytest.raises(UnsafeSQL, match="unsupported_value_literal"):
        validate_read_only_sql(
            "SELECT * FROM people WHERE certificate ILIKE '%formerly known%'",
            {"people"},
            supported_values_by_column={"people.certificate": "formerly known"},
            probeable_columns={"people.certificate"},
            verify_columns={"people.certificate"},
            literal_support_probe=lambda *_: False,
        )
