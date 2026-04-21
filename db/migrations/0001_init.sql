-- initial schema based on JPA entities

CREATE TABLE IF NOT EXISTS users (
    login VARCHAR(100) PRIMARY KEY,
    name TEXT NOT NULL,
    avatar_url TEXT,
    github_access_token TEXT
);


CREATE TABLE IF NOT EXISTS refresh_tokens (
    token VARCHAR(36) PRIMARY KEY,
    user_login VARCHAR(100) NOT NULL REFERENCES users(login),
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL,
    revoked BOOLEAN NOT NULL DEFAULT false
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_login);
