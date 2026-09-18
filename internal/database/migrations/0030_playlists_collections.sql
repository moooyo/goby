CREATE TABLE media_collections (
    item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('Playlist', 'BoxSet')),
    media_type text NOT NULL DEFAULT '' CHECK (media_type IN ('', 'Audio', 'Video')),
    is_public boolean NOT NULL DEFAULT false,
    is_locked boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX media_collections_owner_idx ON media_collections(owner_id, item_id);

CREATE TABLE media_collection_entries (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    collection_id text NOT NULL REFERENCES media_collections(item_id) ON DELETE CASCADE,
    item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    position integer NOT NULL CHECK (position >= 0),
    UNIQUE (collection_id, position) DEFERRABLE INITIALLY DEFERRED,
    CHECK (collection_id <> item_id)
);

CREATE INDEX media_collection_entries_item_idx ON media_collection_entries(item_id, collection_id);

CREATE TABLE media_collection_shares (
    collection_id text NOT NULL REFERENCES media_collections(item_id) ON DELETE CASCADE,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    can_edit boolean NOT NULL DEFAULT false,
    PRIMARY KEY (collection_id, user_id)
);

CREATE INDEX media_collection_shares_user_idx ON media_collection_shares(user_id, collection_id);

-- Removing an owner removes the corresponding catalog items as well as their
-- collection state. Membership rows never own their referenced media items.
CREATE FUNCTION delete_owned_media_collection_items() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE items SET parent_id = NULL, updated_at = clock_timestamp()
    WHERE parent_id IN (SELECT item_id FROM media_collections WHERE owner_id = OLD.id)
      AND id NOT IN (SELECT item_id FROM media_collections WHERE owner_id = OLD.id);
    DELETE FROM items WHERE id IN (
        SELECT item_id FROM media_collections WHERE owner_id = OLD.id
    );
    RETURN OLD;
END;
$$;

CREATE TRIGGER users_delete_owned_media_collections
BEFORE DELETE ON users
FOR EACH ROW EXECUTE FUNCTION delete_owned_media_collection_items();
