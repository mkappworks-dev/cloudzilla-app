CREATE TABLE projects (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE project_columns (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE project_cards (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    column_id BIGINT NOT NULL REFERENCES project_columns(id) ON DELETE CASCADE,
    issue_id  BIGINT REFERENCES issues(id) ON DELETE CASCADE,
    pull_id   BIGINT REFERENCES pull_requests(id) ON DELETE CASCADE,
    note      TEXT NOT NULL DEFAULT '',
    position  INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (issue_id IS NOT NULL AND pull_id IS NULL AND note = '')
        OR (pull_id IS NOT NULL AND issue_id IS NULL AND note = '')
        OR (issue_id IS NULL AND pull_id IS NULL AND note <> '')
    )
);

CREATE INDEX idx_project_cards_column ON project_cards(column_id, position);
CREATE INDEX idx_project_cards_issue  ON project_cards(issue_id);
CREATE INDEX idx_project_cards_pull   ON project_cards(pull_id);
