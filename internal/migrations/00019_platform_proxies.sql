-- +goose Up
-- +goose StatementBegin
CREATE TABLE platform_proxies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    proxy_type text NOT NULL CHECK (proxy_type IN ('http', 'socks5')),
    proxy_ciphertext bytea NOT NULL,
    proxy_key_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (name)
);

CREATE TABLE platform_proxy_providers (
    provider_key text PRIMARY KEY,
    proxy_id uuid NOT NULL REFERENCES platform_proxies(id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE platform_proxy_providers;
DROP TABLE platform_proxies;
-- +goose StatementEnd
