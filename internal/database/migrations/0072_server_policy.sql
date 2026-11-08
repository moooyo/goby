-- Global access policy is independent of per-account restrictions and listener
-- configuration. Existing deployments retain remote access until configured.
CREATE TABLE server_policy_settings (
    id smallint PRIMARY KEY CHECK (id = 1),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    enable_remote_access boolean NOT NULL DEFAULT true,
    local_network_subnets text[] NOT NULL DEFAULT ARRAY[
        '10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16',
        '169.254.0.0/16', 'fc00::/7', 'fe80::/10'
    ]::text[] CHECK (
        cardinality(local_network_subnets) <= 32
        AND array_position(local_network_subnets, NULL) IS NULL
        AND octet_length(local_network_subnets::text) <= 4096
    ),
    ui_culture text NOT NULL DEFAULT 'zh-CN' CHECK (ui_culture IN ('zh-CN', 'en-US')),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO server_policy_settings (id) VALUES (1);
