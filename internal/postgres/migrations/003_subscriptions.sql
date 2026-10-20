CREATE TABLE subscriptions (
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    channel_id bigint NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    active boolean NOT NULL DEFAULT true,
    expires_at timestamptz,
    PRIMARY KEY (user_id, channel_id)
);
