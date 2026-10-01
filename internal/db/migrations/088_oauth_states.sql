-- Server side of OAuth flows that act on a signed-in account, such as connecting
-- Google. Only a hash of the state is stored, so a database read cannot finish a flow.
CREATE TABLE oauth_states (
    state_hash TEXT PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose    TEXT NOT NULL CHECK (purpose IN ('link')),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, purpose)
);
