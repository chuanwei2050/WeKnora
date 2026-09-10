from structured_query.database import Base


def test_metadata_covers_all_persistent_domain_models():
    assert {
        "sq_tenants",
        "sq_namespaces",
        "sq_data_sources",
        "sq_datasets",
        "sq_dataset_versions",
        "sq_tables",
        "sq_columns",
        "sq_column_values",
        "sq_profiles",
        "sq_jobs",
    }.issubset(Base.metadata.tables)


def test_tenant_namespace_and_profile_constraints_are_declared():
    namespaces = Base.metadata.tables["sq_namespaces"]
    profiles = Base.metadata.tables["sq_profiles"]
    assert {column.name for column in namespaces.primary_key.columns} == {"id"}
    assert profiles.c.version_id.nullable is False
    assert profiles.c.payload.nullable is False


def test_external_source_metadata_stores_references_not_credentials():
    sources = Base.metadata.tables["sq_data_sources"]
    columns = set(sources.columns.keys())
    assert {"dialect", "secret_ref", "catalog", "allowed_schemas", "allowed_tables", "field_policies"} <= columns
    assert not {"dsn", "password", "host", "username"} & columns
