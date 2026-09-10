from types import SimpleNamespace

from structured_query.table_selector import confirmed_join_limit, select_minimal_tables, select_schema_candidates


def table(name, foreign_keys=None, version_id="v1"):
    return SimpleNamespace(physical_name=name, physical_schema="public", version_id=version_id, profile={"foreign_keys": foreign_keys or []})


def test_defaults_to_one_and_only_expands_on_confirmed_relation():
    people, certs, noise = table("people", [{"referred_schema": "public", "referred_table": "certs"}]), table("certs"), table("noise")
    tables = {"a": people, "b": certs, "c": noise}
    hits = [{"table_id": "a", "score": 1.0}, {"table_id": "b", "score": .9}, {"table_id": "c", "score": .89}]
    assert select_minimal_tables(hits, tables) == [people, certs]
    assert select_minimal_tables([hits[0], hits[2]], tables) == [people]


def test_near_tied_profiles_do_not_expand_without_a_relation():
    detail, summary = table("detail"), table("summary")
    hits = [{"table_id": "summary", "score": 1.0}, {"table_id": "detail", "score": .999}]
    assert select_minimal_tables(hits, {"detail": detail, "summary": summary}) == [summary]


def test_never_selects_more_than_three_tables():
    chain = [table(str(index), [{"referred_table": str(index + 1)}]) for index in range(4)]
    hits = [{"table_id": str(index), "score": 1.0} for index in range(4)]
    assert len(select_minimal_tables(hits, {str(index): value for index, value in enumerate(chain)})) <= 3


def test_schema_candidates_add_only_near_tied_sibling_sheet():
    summary, detail, unrelated = table("summary"), table("detail"), table("other", version_id="v2")
    hits = [
        {"table_id": "summary", "score": 1.0},
        {"table_id": "detail", "score": .96},
        {"table_id": "other", "score": .99},
    ]
    selected = select_schema_candidates(
        hits, {"summary": summary, "detail": detail, "other": unrelated}
    )
    assert selected == [summary, detail]


def test_schema_candidates_keep_one_when_sibling_is_not_ambiguous():
    summary, detail = table("summary"), table("detail")
    hits = [{"table_id": "summary", "score": 1.0}, {"table_id": "detail", "score": .8}]
    assert select_schema_candidates(hits, {"summary": summary, "detail": detail}) == [summary]


def test_mutually_exclusive_schema_candidates_do_not_expand_join_limit():
    assert confirmed_join_limit([table("summary"), table("detail")]) == 1


def test_confirmed_relation_expands_join_limit():
    people = table("people", [{"referred_schema": "public", "referred_table": "certs"}])
    assert confirmed_join_limit([people, table("certs")]) == 2
