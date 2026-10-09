-- Empty for rows created before this column and for mentions and transfers, which have no title to show.
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS subject_title TEXT NOT NULL DEFAULT '';
