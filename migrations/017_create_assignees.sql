CREATE TABLE issue_assignees (
    issue_id BIGINT REFERENCES issues(id) ON DELETE CASCADE,
    user_id  BIGINT REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY(issue_id, user_id)
);

CREATE TABLE pull_assignees (
    pull_id BIGINT REFERENCES pull_requests(id) ON DELETE CASCADE,
    user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY(pull_id, user_id)
);
