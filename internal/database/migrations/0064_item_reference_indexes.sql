-- Item deletion must find all playback/user-state references, including
-- terminal history outside the existing current-source and resume indexes.
CREATE INDEX user_item_data_item_idx ON user_item_data(item_id);
CREATE INDEX play_sessions_item_idx ON play_sessions(item_id);

-- Historical source_item_id is immutable evidence, not the nullable live FK.
-- Detached operations no longer participate in the SET NULL lookup.
CREATE INDEX media_operations_item_idx ON media_operations(item_id)
    WHERE item_id IS NOT NULL;
