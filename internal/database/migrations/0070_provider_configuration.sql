-- Provider overrides are a single authenticated encrypted document. No secret
-- values or administrator supplied application identifiers are stored as JSON.
CREATE TABLE provider_configuration (
    id integer PRIMARY KEY CHECK (id = 1),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    configuration_ciphertext bytea CHECK (
        octet_length(configuration_ciphertext) BETWEEN 34 AND 131104
    ),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO provider_configuration(id) VALUES (1);
