CREATE TABLE IF NOT EXISTS buddy_states (
    user_id text PRIMARY KEY,
    body jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS buddy_sessions (
    token_hash text PRIMARY KEY,
    user_id text NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS corporate_operations (
    operation_key text PRIMARY KEY,
    user_id text NOT NULL,
    body jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS corporate_operations_user ON corporate_operations(user_id);
