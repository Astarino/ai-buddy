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
ALTER TABLE buddy_sessions ADD COLUMN IF NOT EXISTS context_hash text NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS corporate_operations (
    operation_key text PRIMARY KEY,
    user_id text NOT NULL,
    body jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS corporate_operations_user ON corporate_operations(user_id);
CREATE TABLE IF NOT EXISTS buddy_audit (
    id text PRIMARY KEY,
    actor_id text NOT NULL,
    owner_id text NOT NULL,
    event_type text NOT NULL,
    object_id text NOT NULL,
    decision text NOT NULL,
    outcome text NOT NULL,
    correlation_id text NOT NULL,
    policy_version text NOT NULL,
    at timestamptz NOT NULL,
    duration_ms bigint NOT NULL
);
CREATE INDEX IF NOT EXISTS buddy_audit_owner ON buddy_audit(owner_id, at);
CREATE TABLE IF NOT EXISTS buddy_logins (
    state_hash text PRIMARY KEY,
    nonce text NOT NULL,
    verifier text NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS buddy_logins_expiry ON buddy_logins(expires_at);
CREATE TABLE IF NOT EXISTS buddy_principals (
    profile_id text PRIMARY KEY,
    issuer text NOT NULL,
    subject text NOT NULL,
    UNIQUE(issuer,subject)
);
