CREATE TABLE topics (
    id   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);
CREATE TABLE repo_topics (
    repo_id  BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    topic_id BIGINT NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    PRIMARY KEY (repo_id, topic_id)
);
CREATE INDEX idx_repo_topics_topic ON repo_topics(topic_id);
