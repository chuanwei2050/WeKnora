from structured_query.candidate_search import diversify_value_hits, fuse_ranked_hits, lexical_evidence_is_sufficient, lexical_terms, rank_tables_with_profile_evidence, refine_value_hits_for_tables


def test_lexical_terms_are_generic_and_bounded():
    terms = lexical_terms("数科事业部硕士学历人数", limit=8)
    assert len(terms) <= 8 and "数科事业部硕士学历人数" in terms


def test_lexical_terms_keep_recall_across_a_multi_condition_question():
    terms = lexical_terms("甲条件与乙条件以及丙条件分别多少", limit=64)
    assert "甲条" in terms
    assert "乙条" in terms
    assert "丙条" in terms


def test_48_term_budget_keeps_all_bigrams_of_an_ordinary_question():
    question = "请重新核对并列出持有系统集成项目管理工程师证书的人员"
    terms = lexical_terms(question, limit=48)
    assert "系统" in terms
    assert "证书" in terms
    assert "人员" in terms


def test_rank_fusion_is_auditable_and_rewards_two_channels():
    shared = {"kind": "table", "table_id": "a", "column_id": "", "text": "人员"}
    result = fuse_ranked_hits([shared, {**shared, "table_id": "b"}], [shared], 2)
    assert result[0]["table_id"] == "a"
    assert result[0]["sources"] == ["lexical", "vector"]


def test_table_fusion_uses_profile_identity_not_backend_rendering():
    vector = [{"kind": "table", "table_id": "a", "column_id": "", "text": "M-Schema"}]
    lexical = [{"kind": "table", "table_id": "a", "column_id": "", "text": "table columns"}]
    result = fuse_ranked_hits(vector, lexical, 5)
    assert len(result) == 1
    assert result[0]["sources"] == ["lexical", "vector"]


def test_weak_generic_lexical_hit_keeps_vector_fallback_enabled():
    assert not lexical_evidence_is_sufficient(
        [{"title_score": 0, "score": 1}], [{"score": 1}], []
    )
    assert lexical_evidence_is_sufficient(
        [{"title_score": 3, "score": 1}], [{"score": 1}], []
    )


def test_column_evidence_reranks_owning_table():
    tables = [{"kind": "table", "table_id": "summary", "score": .2}, {"kind": "table", "table_id": "detail", "score": .19}]
    columns = [{"kind": "column", "table_id": "detail", "score": .1}]
    assert rank_tables_with_profile_evidence(tables, columns, [])[0]["table_id"] == "detail"


def test_real_value_can_recall_a_table_without_a_table_name_match():
    values = [{"kind": "value", "table_id": "detail", "column_id": "certificate", "score": .1}]
    ranked = rank_tables_with_profile_evidence([], [], values)
    assert ranked[0]["table_id"] == "detail"


def test_value_evidence_is_bounded_per_column():
    hits = [
        {"column_id": "dominant", "score": 1 - index / 100} for index in range(10)
    ] + [{"column_id": "other", "score": .1}]
    selected = diversify_value_hits(hits, limit=10, per_column=5)
    assert len([hit for hit in selected if hit["column_id"] == "dominant"]) == 5
    assert any(hit["column_id"] == "other" for hit in selected)


def test_target_table_refinement_restores_values_lost_by_global_ranking():
    global_hits = [
        {"kind": "value", "table_id": "other", "column_id": "certificate", "text": "irrelevant", "score": 1.0}
    ]
    targeted_hits = [
        {"kind": "value", "table_id": "target", "column_id": "certificate", "text": "stored spelling", "score": 2.0}
    ]

    result = refine_value_hits_for_tables(global_hits, targeted_hits, {"target"})

    assert [hit["text"] for hit in result] == ["stored spelling"]
