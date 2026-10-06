-- Who suspended the account, and why, live in the audit log: an FK here would
-- need its own ON DELETE rule or a ghost reassignment.
ALTER TABLE users ADD COLUMN suspended_at TIMESTAMPTZ;
