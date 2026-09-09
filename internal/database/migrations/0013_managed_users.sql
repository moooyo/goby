ALTER TABLE users
    ADD COLUMN management_revision bigint NOT NULL DEFAULT 1
    CHECK (management_revision > 0);
