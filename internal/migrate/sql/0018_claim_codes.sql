-- compat: compatible
CREATE TABLE pwikit_claim_code (
    user_id    bigint      PRIMARY KEY REFERENCES web_user (id) ON DELETE CASCADE,
    code_hash  text        NOT NULL,
    sent_at    timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    attempts   integer     NOT NULL DEFAULT 0
);
