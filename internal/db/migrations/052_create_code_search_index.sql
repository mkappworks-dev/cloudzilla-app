CREATE TABLE code_search_index (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id    BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    ref        TEXT NOT NULL DEFAULT 'HEAD',
    file_path  TEXT NOT NULL,
    content    TEXT NOT NULL,
    tsv        TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED,
    indexed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, file_path)
);
CREATE INDEX idx_code_search_tsv ON code_search_index USING GIN(tsv);
CREATE INDEX idx_code_search_repo ON code_search_index(repo_id);
