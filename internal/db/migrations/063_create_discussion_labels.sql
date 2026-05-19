CREATE TABLE discussion_labels (
    discussion_id BIGINT REFERENCES discussions(id) ON DELETE CASCADE,
    label_id      BIGINT REFERENCES labels(id) ON DELETE CASCADE,
    PRIMARY KEY(discussion_id, label_id)
);
