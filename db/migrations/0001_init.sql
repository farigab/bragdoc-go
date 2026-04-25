CREATE TABLE IF NOT EXISTS users (
    login               TEXT    PRIMARY KEY,
    name                TEXT    NOT NULL,
    avatar_url          TEXT,
    github_access_token TEXT
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    token      TEXT    PRIMARY KEY,
    user_login TEXT    NOT NULL REFERENCES users(login),
    expires_at TEXT    NOT NULL,   -- stored as RFC3339 / ISO-8601
    created_at TEXT    NOT NULL,   -- stored as RFC3339 / ISO-8601
    revoked    INTEGER NOT NULL DEFAULT 0  -- 0 = false, 1 = true
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_login);
