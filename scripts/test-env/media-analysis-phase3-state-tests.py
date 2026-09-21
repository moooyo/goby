#!/usr/bin/env python3
"""Remote-only regressions for resumed-state and cache evidence boundaries."""

import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sqlite3
import tempfile
import unittest


spec = importlib.util.spec_from_file_location("phase3_state", Path(__file__).with_name("media-analysis-phase3-state.py"))
state = importlib.util.module_from_spec(spec)
spec.loader.exec_module(state)


class StateEvidenceTests(unittest.TestCase):
    def snapshot(self, userdata, session=None):
        connection = sqlite3.connect(":memory:")
        self.addCleanup(connection.close)
        connection.execute("CREATE TABLE rows(table_name TEXT,row_key TEXT,payload TEXT)")
        connection.execute("INSERT INTO rows VALUES(?,?,?)", ("user_item_data", '["user","item"]', state.canonical(userdata).decode()))
        if session is not None:
            connection.execute("INSERT INTO rows VALUES(?,?,?)", ("play_sessions", '["play_session"]', state.canonical(session).decode()))
        return connection

    def resume(self, *, reused=False, changed=None):
        previous = dict(state.state_defaults("user", "item"), playback_position_ticks=20000000, play_count=4)
        current = dict(previous, play_count=4 if reused else 5)
        session = {"id": "play_session", "user_id": "user", "item_id": "item", "media_source_id": "source",
                   "counted": True, "state": "Stopped", "position_ticks": 20000000}
        if changed:
            session.update(changed)
        before = self.snapshot(previous, dict(session, state="Playing") if reused else None)
        after = self.snapshot(current, session)
        http = [{"method": "POST", "path_sha256": hashlib.sha256(route.encode()).hexdigest(),
                 "http_status": 204, "client_timed_out": False, "client_disconnected": False}
                for route in ("/emby/Sessions/Playing", "/emby/Sessions/Playing/Stopped")]
        data = {"reconnected": True, "user_id": "user", "item_id": "item", "play_session_id": "play_session",
                "persisted_ticks": 20000000, "requested_ticks": 20000000, "current_media_source_id": "source", "http_evidence": http}
        ack = {"kind": "progress", "probe_progress": data,
               "probe_receipt": {"path": "/private/resume.probe.json", "bytes": 1, "sha256": "a" * 64},
               "postimages": [{"table": "user_item_data", "key": ["user", "item"],
                               "expected_fields": {"playback_position_ticks": 20000000, "play_count": current["play_count"]}}]}
        return state.Observer({}), before, after, ack

    def test_external_resume_requires_actual_stopped_counted_session(self):
        for change in ({"state": "Playing"}, {"counted": False}, {"item_id": "other"}, {"position_ticks": 0}):
            with self.subTest(change=change):
                observer, before, after, ack = self.resume(changed=change)
                with self.assertRaises(state.StateError):
                    state.inspect_probe_progress_images(observer, before, after, ack)

    def test_reused_counted_session_does_not_invent_an_increment(self):
        observer, before, after, ack = self.resume(reused=True)
        state.inspect_probe_progress_images(observer, before, after, ack)
        invalid = copy.deepcopy(ack)
        invalid["postimages"][0]["expected_fields"]["play_count"] += 1
        with self.assertRaises(state.StateError):
            state.inspect_probe_progress_images(observer, before, after, invalid)

    def test_new_session_requires_exact_counter_increment_and_complete_http(self):
        observer, before, after, ack = self.resume()
        state.inspect_probe_progress_images(observer, before, after, ack)
        ack["probe_progress"]["http_evidence"][1]["client_timed_out"] = True
        with self.assertRaises(state.StateError):
            state.inspect_probe_progress_images(observer, before, after, ack)

    def test_cache_temporary_allocation_counts_owned_files_and_rejects_symlinks(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".goby-analysis-cache").write_text(json.dumps({"marker": "goby-analysis-cache-v1", "owner": "owner"}))
            temporary = root / "tmp-owner-work"
            temporary.mkdir()
            payload = temporary / "frame.jpeg"
            payload.write_bytes(b"actual temporary bytes")
            observer = state.Observer({"analysis_cache": {"path": str(root), "owner": "owner"}})
            result = observer.cache(None)
            expected = temporary.stat().st_blocks * 512 + payload.stat().st_blocks * 512
            self.assertEqual(result["temporary_bytes"], expected)
            self.assertEqual(result["temporary_inodes"], 2)
            self.assertEqual(result["counts"]["temporary"], 1)
            self.assertEqual(result["counts"]["ready"], 0)
            os.symlink(payload, temporary / "unexpected-link")
            with self.assertRaises(state.StateError):
                observer.cache(None)


if __name__ == "__main__":
    unittest.main()
