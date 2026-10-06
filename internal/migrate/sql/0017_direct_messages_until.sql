-- compat: breaking
ALTER TABLE web_user ADD COLUMN direct_messages_until timestamptz;
