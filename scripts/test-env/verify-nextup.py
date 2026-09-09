#!/usr/bin/env python3
"""Verify deployed NextUp using owned synthetic media on Linux test-env."""

import importlib.util
import json
import os
from pathlib import Path
import sys
from urllib.parse import quote, urlencode

spec = importlib.util.spec_from_file_location("goby_direct_smoke", Path(__file__).with_name("verify-direct-playback.py"))
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)

ROOT = Path("/opt/goby-fixtures/nextup-long-reference")
RECORD = Path("/opt/goby-test/nextup-goby-verification.json")
NAME = "Goby NextUp verification"


def main():
    os.umask(0o077)
    api, owned = smoke.API(), smoke.OwnedFixture()
    summary = {"status": "failed", "cleanup_errors": []}
    series_id = ""
    sources = {}
    try:
        smoke.check(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"),
                    "Run through SSH on the authorized Linux test host")
        marker = ROOT / ".goby-managed"
        smoke.check(ROOT.resolve(strict=True) == ROOT and marker.is_file() and not marker.is_symlink() and
                    marker.read_text(encoding="utf-8").strip() == "goby-nextup-long-reference-owned-v1",
                    "Owned long-duration reference fixture is unavailable")
        sources = {str(path): smoke.digest(path) for path in ROOT.rglob("*.mp4")}
        smoke.check(len(sources) == 3, "The reference fixture must contain exactly three synthetic episodes")
        owned.open()
        api.admin_login(smoke.credentials())
        api.viewer_login(owned.state)
        listed = api.request("GET", "/admin/v1/libraries", admin=True, label="NextUp library lookup")["Items"]
        matching = [entry for entry in listed if entry.get("Name") == NAME or str(ROOT) in entry.get("Paths", [])]
        if RECORD.exists():
            record = smoke.bounded_json(RECORD)
            smoke.check(record.get("owner") == "goby-nextup-verification" and len(matching) == 1 and
                        matching[0].get("Id") == record.get("library_id"), "Owned NextUp library record does not match")
            library = matching[0]
        else:
            smoke.check(not matching, "An unrecorded library already uses this fixture or name")
            library = api.request("POST", "/admin/v1/libraries", admin=True, expected=(201,),
                                  label="NextUp library creation", body={"Name": NAME, "CollectionType": "tvshows",
                                  "Paths": [str(ROOT)], "Scan": False})["Library"]
            descriptor = os.open(RECORD, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
                json.dump({"owner": "goby-nextup-verification", "library_id": library["Id"]}, handle)
        smoke.check(library.get("Name") == NAME and library.get("Paths") == [str(ROOT)] and
                    library.get("CollectionType") == "tvshows", "Owned NextUp library configuration changed")
        job = api.request("POST", "/admin/v1/libraries/" + quote(library["Id"]) + "/scan", admin=True,
                          expected=(202,), label="NextUp scan")["Job"]
        job = api.wait_job(job["Id"])
        smoke.check(job["Status"] == "completed" and job["Scanned"] == 3 and not job.get("Error"),
                    "NextUp source scan did not finish cleanly")
        query = urlencode({"ParentId": library["Id"], "Recursive": "true", "IncludeItemTypes": "Series"})
        result = api.request("GET", "/emby/Users/" + quote(api.user_id) + "/Items?" + query,
                             emby=True, label="NextUp series lookup")
        smoke.check(result.get("TotalRecordCount") == 1, "NextUp fixture must contain one series")
        series_id = result["Items"][0]["Id"]
        episodes = api.request("GET", "/emby/Shows/" + quote(series_id) + "/Episodes",
                               emby=True, label="NextUp episode lookup")["Items"]
        smoke.check(len(episodes) == 3 and all(episode.get("RunTimeTicks") == 600 * smoke.TICKS for episode in episodes),
                    "NextUp episodes must have actual ten-minute probe durations")
        first, middle, last = [episode["Id"] for episode in episodes]

        def flag(method, identifier):
            return api.request(method, "/emby/Users/" + quote(api.user_id) + "/PlayedItems/" + quote(identifier),
                               emby=True, label="Owned NextUp watched state")

        def next_up(expected, total, **parameters):
            parameters["UserId"] = api.user_id
            result = api.request("GET", "/sHoWs/nExTuP?" + urlencode(parameters),
                                 emby=True, label="NextUp selection")
            smoke.check(result.get("TotalRecordCount") == total and
                        [item["Id"] for item in result.get("Items", [])] == expected,
                        "NextUp result order, page, or total does not match")

        flag("DELETE", series_id)
        next_up([], 0, SeriesId=series_id)
        flag("POST", middle)
        next_up([last], 1, SeriesId=series_id)
        flag("DELETE", middle)
        flag("POST", first)
        next_up([middle, last], 2, SeriesId=series_id)
        next_up([last], 2, SeriesId=series_id, StartIndex=1, Limit=1)
        next_up([], 2, SeriesId=series_id, Limit=0)
        next_up([middle, last], 2, SeriesId=series_id, ParentId=library["Id"])
        # This global expectation verifies the documented Goby policy. The
        # isolated reference's global positive-selection rule remains unresolved.
        next_up([middle], 1, ParentId=library["Id"])
        summary.update({"status": "passed", "episodes": 3, "episode_seconds": 600,
                        "series_cursor_skips_earlier_gap": True, "remaining_sequence": True,
                        "pagination_and_zero_limit": True, "global_goby_policy": True})
    except Exception as error:
        summary["error"] = str(error) if isinstance(error, smoke.VerificationFailure) else type(error).__name__
    finally:
        if api.token and series_id:
            try:
                api.request("DELETE", "/emby/Users/" + quote(api.user_id) + "/PlayedItems/" + quote(series_id),
                            emby=True, label="NextUp user state restoration")
                summary["owned_state_restored"] = True
            except Exception:
                summary["cleanup_errors"].append("Owned NextUp state restoration failed")
        if api.token:
            try:
                api.request("POST", "/emby/Sessions/Logout", emby=True, parse=False, label="NextUp viewer logout")
            except Exception:
                summary["cleanup_errors"].append("NextUp viewer logout failed")
        if api.cookie:
            try:
                api.request("DELETE", "/admin/v1/session", admin=True, expected=(204,), parse=False,
                            label="NextUp administrator logout")
            except Exception:
                summary["cleanup_errors"].append("NextUp administrator logout failed")
        try:
            smoke.check(all(smoke.digest(Path(path)) == value for path, value in sources.items()),
                        "Synthetic episode bytes changed")
            summary["source_bytes_unchanged"] = bool(sources)
        except Exception:
            summary["cleanup_errors"].append("Synthetic source preservation check failed")
        if owned.lock is not None:
            owned.lock.close()
        if summary["cleanup_errors"]:
            summary["status"] = "failed"
        print(json.dumps(summary, indent=2, sort_keys=True))
    return 0 if summary["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
