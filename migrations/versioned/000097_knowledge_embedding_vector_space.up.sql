ALTER TABLE knowledges
    ADD COLUMN embedding_compatibility_id VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN embedding_dimension INTEGER NOT NULL DEFAULT 0;

UPDATE knowledges AS knowledge
SET embedding_compatibility_id = COALESCE(
        model.parameters -> 'embedding_parameters' ->> 'compatibility_id',
        ''
    ),
    embedding_dimension = COALESCE(
        NULLIF(model.parameters -> 'embedding_parameters' ->> 'dimension', '')::INTEGER,
        0
    )
FROM models AS model
WHERE model.id = knowledge.embedding_model_id;
