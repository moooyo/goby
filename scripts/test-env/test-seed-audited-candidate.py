#!/usr/bin/env python3
"""Pure seed guards; no files, sockets, databases, or candidate processes are used."""
import copy
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import Mock, patch

SPEC = importlib.util.spec_from_file_location("candidate_seed", Path(__file__).with_name("seed-audited-candidate.py"))
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)


class SeedGuards(unittest.TestCase):
    def value(self):
        return {"runId": "audited-seed-test", "output": str(M.R / "seed-pure-guard"), "candidateManifest": M.MANIFEST,
                "runtimeInspection": M.INSPECTION, "runtimeHelper": {}, "budgets": copy.deepcopy(M.BUDGETS)}

    def io(self):
        io = M.CandidateIO(self.value(), {}, {})
        io.pin = Mock()
        io.candidate = {"publicUrl": "http://127.0.0.1:28496"}
        io.port = 28498
        return io

    def test_existing_output_rejected_before_new_receipt_or_candidate_access(self):
        io = self.io()
        with patch.object(M.os.path, "lexists", return_value=True), patch.object(M, "descriptor") as read, \
             patch.object(M, "write_json_once") as write, patch.object(M.os, "mkdir") as mkdir:
            with self.assertRaises(ValueError):
                io.open()
            read.assert_not_called()
            write.assert_not_called()
            mkdir.assert_not_called()

    def test_failed_mutation_is_dispatched_once_and_retains_unknown_responsibility(self):
        io = self.io()
        connection = Mock()
        connection.request.side_effect = TimeoutError("synthetic transport timeout")
        with patch.object(M, "write_json_once", return_value={"path": "/private/intent", "sha256": "a" * 64}) as persist, \
             patch.object(M.http.client, "HTTPConnection", return_value=connection):
            with self.assertRaises(TimeoutError):
                io.request("create-user", "POST", "/admin/v1/users", {"Name": "synthetic"})
            self.assertEqual(connection.request.call_count, 1)
            self.assertEqual(persist.call_count, 1)
            self.assertEqual(io.requests, {"normal": 1, "cleanup": 0})
            self.assertEqual(io.request_states[0]["outcome"], "unknown")
            connection.close.assert_called_once()

    def test_normal_exhaustion_cannot_consume_cleanup_reserve(self):
        io = self.io()
        io.requests["normal"] = M.BUDGETS["maximumRequests"] - M.BUDGETS["cleanupRequests"]
        with patch.object(M.http.client, "HTTPConnection") as connection, patch.object(M, "write_json_once") as write:
            with self.assertRaises(ValueError):
                io.request("normal-after-cap", "GET", "/healthz")
            connection.assert_not_called()
            write.assert_not_called()
            self.assertEqual(io.requests["cleanup"], 0)

    def test_fixed_length_premature_eof_is_not_a_complete_response(self):
        response = Mock(status=200, length=8)
        response.isclosed.return_value = True
        self.assertFalse(M.response_complete(response, "GET", b"{}", 1024))
        response.length = 0
        self.assertTrue(M.response_complete(response, "GET", b"{}", 1024))
        response.isclosed.return_value = False
        self.assertFalse(M.response_complete(response, "GET", b"{}", 1024))
        self.assertFalse(M.response_complete(response, "HEAD", b"unexpected", 1024))

    def test_known_native_cookie_survives_incomplete_login_body_for_exact_cleanup(self):
        response = {"status": 200, "complete": False, "raw": b"{", "headers": [("Set-Cookie", "goby_session=" + "a" * 32 + "; Path=/admin; HttpOnly")]}
        auth = M.response_auth(response, "native")
        self.assertEqual(auth["token"], "a" * 32)
        self.assertEqual(auth["csrf"], M.sha(("goby:admin:csrf:" + "a" * 32).encode()))
        with self.assertRaises(ValueError):
            M.response_auth(response, "emby")

    def test_q_policy_requires_explicit_empty_folders_without_disabling_playback(self):
        user = {"IsAdministrator": False, "IsDisabled": False, "Policy": {**M.PLAYBACK, "EnableAllFolders": False, "EnabledFolders": []}}
        M.check_policy(user, False)
        for patch_policy in ({"EnableAllFolders": True}, {"EnabledFolders": ["a" * 32]}, {"EnableMediaPlayback": False}):
            changed = copy.deepcopy(user)
            changed["Policy"].update(patch_policy)
            with self.assertRaises(ValueError):
                M.check_policy(changed, False)

    def test_scan_completion_requires_native_case_exact_job_and_no_warning(self):
        job = {"Id": "a" * 32, "LibraryId": "b" * 32, "ForceProbe": False, "Status": "completed", "Error": ""}
        self.assertTrue(M.completed_job(job, "b" * 32, "a" * 32))
        for changed in ({"Status": "Completed"}, {"Status": "failed"}, {"Error": "probe incomplete"}, {"LibraryId": "c" * 32}):
            with self.assertRaises(ValueError):
                M.completed_job({**job, **changed}, "b" * 32, "a" * 32)

    def test_actual_english_subtitle_alias_is_preserved_and_other_languages_rejected(self):
        for codec, index in (("srt", 2), ("vtt", 3)):
            row = {"Codec": codec, "Index": index, "IsExternal": True, "Path": str(M.C / "data/media/Movies" / ("M3e Client Movie.en." + codec))}
            for language in ("en", "eng"):
                result = M.mapped_subtitle({**row, "Language": language}, codec)
                self.assertEqual(result["language"], language)
                self.assertEqual(result["index"], index)
            for language in ("", "English", "fr", "zh", None):
                with self.assertRaises(ValueError):
                    M.mapped_subtitle({**row, "Language": language}, codec)


if __name__ == "__main__":
    unittest.main()
