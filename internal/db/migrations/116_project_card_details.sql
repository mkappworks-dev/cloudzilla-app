ALTER TABLE project_cards ADD COLUMN title    TEXT NOT NULL DEFAULT '';
ALTER TABLE project_cards ADD COLUMN due_date DATE;

-- Dropped before the backfill: the old CHECK requires a non-empty note on unlinked cards.
ALTER TABLE project_cards DROP CONSTRAINT project_cards_check;

-- A leading blank line would otherwise yield an empty title and violate the new CHECK.
UPDATE project_cards SET note = btrim(note, E' \t\r\n')
WHERE issue_id IS NULL AND pull_id IS NULL;

-- A first line over 120 chars keeps the whole original text as the description so nothing is lost.
UPDATE project_cards SET
    title = COALESCE(NULLIF(LEFT(split_part(note, E'\n', 1), 120), ''), 'Untitled'),
    note  = CASE
        WHEN length(split_part(note, E'\n', 1)) > 120 THEN note
        WHEN position(E'\n' IN note) > 0 THEN substr(note, length(split_part(note, E'\n', 1)) + 2)
        ELSE ''
    END
WHERE issue_id IS NULL AND pull_id IS NULL;

ALTER TABLE project_cards ADD CONSTRAINT project_cards_shape CHECK (
    (issue_id IS NULL OR pull_id IS NULL)
    AND (issue_id IS NOT NULL OR pull_id IS NOT NULL OR title <> '')
);

CREATE TABLE card_assignees (
    card_id BIGINT NOT NULL REFERENCES project_cards(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, user_id)
);

CREATE TABLE card_labels (
    card_id  BIGINT NOT NULL REFERENCES project_cards(id) ON DELETE CASCADE,
    label_id BIGINT NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, label_id)
);
