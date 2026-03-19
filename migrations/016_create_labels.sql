CREATE TABLE labels (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    name        TEXT   NOT NULL,
    color       TEXT   NOT NULL DEFAULT '#e5e5e5',
    description TEXT   NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, name)
);

CREATE TABLE issue_labels (
    issue_id BIGINT REFERENCES issues(id) ON DELETE CASCADE,
    label_id BIGINT REFERENCES labels(id) ON DELETE CASCADE,
    PRIMARY KEY(issue_id, label_id)
);

CREATE TABLE pull_labels (
    pull_id  BIGINT REFERENCES pull_requests(id) ON DELETE CASCADE,
    label_id BIGINT REFERENCES labels(id) ON DELETE CASCADE,
    PRIMARY KEY(pull_id, label_id)
);
