#!/usr/bin/env python3
"""Verify a normal ProbeVersion 4 upgrade of five existing Linux fixtures.

This module is imported by the audio-profile deployment verifier. It never
creates libraries, accounts, media, or ownership records. Callers must hold the
existing direct-playback fixture lock and keep other state-changing verifiers
idle until verify() finishes. The only mutations are ordinary administrator
scan requests and cancellation of scan jobs returned to this instance.

Database rows and private ownership records stay in memory. Public return
values contain only fixed fixture labels, counts, versions, and aggregate
hashes. The injected database helper must enforce read-only transactions.
"""

from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys
from urllib.parse import quote


FIXTURE_ROOT = Path("/opt/goby-fixtures")
PRIVATE_ROOT = Path("/opt/goby-test")
REPORT = Path(__file__).resolve().parents[2] / "docs/development/m4c-rescan-upgrade.json"
MAX_FILE = 16 * 1024 * 1024
MAX_TOTAL_FILES = 128
MAX_TOTAL_BYTES = 64 * 1024 * 1024
MAX_ITEMS = 256
MAX_USER_ROWS = 4096
ID_PATTERN = re.compile(r"[0-9a-f]{32}\Z")
HASH_PATTERN = re.compile(r"[0-9a-f]{64}\Z")
FIXTURES = {
    "direct": {
        "id": "f7735a851879da1b28459681929029ac",
        "name": "Goby Direct Playback fd68121d1efcad99",
        "root": "direct-playback", "collection": "movies", "media_count": 1,
    },
    "hls": {
        "id": "f9d3068ea791e6fdf1e756351e7938fb",
        "name": "Goby HLS verification",
        "root": "hls-reference", "collection": "movies", "media_count": 1,
    },
    "nextup": {
        "id": "869490513db0bc3bd3ca045f94c0f4b7",
        "name": "Goby NextUp verification",
        "root": "nextup-long-reference", "collection": "tvshows", "media_count": 3,
    },
    "subtitle": {
        "id": "6b292629894c1e52223362072bff8a22",
        "name": "Goby Subtitle verification",
        "root": "subtitle-reference", "collection": "movies", "media_count": 1,
    },
}
AUDIO_OWNER = "goby-audio-m4c-verification-v1"
AUDIO_NAME = "Goby Audio M4c verification"
AUDIO_NAMES = {
    "Reference Audio M4c.mp3", "Reference Audio M4c.flac",
    "Reference Audio M4c.aac", "Reference Audio M4c.wav",
    "Goby Audio M4c Short Tail.flac",
}


def aggregate(value) -> str:
    """Return a deterministic digest without exposing the underlying rows."""
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":"),
                                     ensure_ascii=True).encode("ascii")).hexdigest()


def file_identity(info) -> tuple:
    # Reading source files may update atime; none of these fields may change.
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


class UpgradeVerification:
    """Own bounded scan jobs and compare independent pre/post observations."""

    def __init__(self, api, db, smoke, audio_record: dict) -> None:
        self.api = api
        self.db = db
        self.smoke = smoke
        self.audio_record = audio_record
        self.libraries = {}
        self.record_paths = []
        self.pending_jobs = {}
        self.unconfirmed_scans = {}
        self.scan_results = []
        self.before = None
        self.before_files = None
        self.before_records = None
        self.before_root = None
        self.prepared = False
        self.finished = False
        self.verified = False

    def check(self, condition: bool, label: str) -> None:
        self.smoke.check(condition, label)

    def _directory(self, path: Path) -> None:
        info = path.lstat()
        self.check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and
                   info.st_mode & 0o022 == 0 and path.resolve(strict=True) == path,
                   "An upgrade fixture directory has unsafe ownership or resolution")

    def _digest(self, path: Path) -> dict:
        before = path.lstat()
        self.check(stat.S_ISREG(before.st_mode) and before.st_uid == 0 and
                   before.st_nlink == 1 and before.st_mode & 0o022 == 0 and
                   0 <= before.st_size <= MAX_FILE and path.resolve(strict=True) == path,
                   "An upgrade fixture input is not a bounded owned regular file")
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        with os.fdopen(descriptor, "rb") as source:
            self.check(file_identity(os.fstat(source.fileno())) == file_identity(before),
                       "An upgrade fixture input changed before its snapshot")
            digest = hashlib.sha256()
            total = 0
            while True:
                chunk = source.read(131072)
                if not chunk:
                    break
                total += len(chunk)
                self.check(total <= MAX_FILE, "An upgrade fixture input exceeded its snapshot limit")
                digest.update(chunk)
            self.check(total == before.st_size and
                       file_identity(os.fstat(source.fileno())) == file_identity(before) and
                       file_identity(path.lstat()) == file_identity(before),
                       "An upgrade fixture input changed during its snapshot")
        return {"identity": list(file_identity(before)), "bytes": total, "sha256": digest.hexdigest()}

    def _private(self, path: Path) -> dict:
        value = self.smoke.bounded_json(path)
        self.record_paths.append(path)
        return value

    def _marker(self, path: Path, expected: str) -> None:
        snapshot = self._digest(path)
        self.check(snapshot["bytes"] <= 256 and path.read_text(encoding="utf-8").strip() == expected,
                   "An upgrade fixture ownership marker does not match")

    def _report(self) -> None:
        self.check(REPORT.is_file() and not REPORT.is_symlink() and REPORT.stat().st_size <= 65536,
                   "The committed prior upgrade report is unavailable")
        prior = json.loads(REPORT.read_text(encoding="utf-8"))
        self.check(isinstance(prior, dict) and prior.get("owner") == "goby-m4c-rescan-upgrade-v1" and
                   prior.get("status") == "passed" and isinstance(prior.get("libraries"), list) and
                   len(prior["libraries"]) == 4,
                   "The committed prior upgrade report has an unexpected scope")
        expected = {(entry["id"], entry["name"]) for entry in FIXTURES.values()}
        actual = {(entry.get("library_id"), entry.get("name")) for entry in prior["libraries"]}
        self.check(actual == expected and prior.get("after", {}).get("selected_item_count") == 14 and
                   prior.get("after", {}).get("selected_media_count") == 6,
                   "The prior upgrade report does not identify the authorized four libraries")

    def _ownership(self) -> None:
        self._report()
        self._directory(FIXTURE_ROOT)
        self._marker(FIXTURE_ROOT / ".goby-managed", "goby-generated-media-fixtures")
        for value in FIXTURES.values():
            self._directory(FIXTURE_ROOT / value["root"])
        direct_root = FIXTURE_ROOT / "direct-playback"
        marker = self._private(direct_root / ".goby-direct-playback-owned.json")
        direct = self._private(direct_root / ".goby-direct-playback-state.json")
        self.check(marker.get("owner") == "goby-direct-playback-v1" and
                   ID_PATTERN.fullmatch(str(marker.get("nonce", ""))) is not None and
                   direct.get("owner") == marker["owner"] and direct.get("nonce") == marker["nonce"] and
                   direct.get("library_id") == FIXTURES["direct"]["id"] and
                   direct.get("library_name") == FIXTURES["direct"]["name"] ==
                   "Goby Direct Playback " + marker["nonce"][:16] and
                   ID_PATTERN.fullmatch(str(direct.get("user_id", ""))) is not None and
                   HASH_PATTERN.fullmatch(str(direct.get("media_sha256", ""))) is not None and
                   not direct.get("library_creation_pending") and not direct.get("media_pending"),
                   "The direct-playback upgrade ownership record does not match")
        self.smoke.private_file(direct_root / ".goby-direct-playback.lock")
        self.check(self._digest(direct_root / "Goby Direct Playback 600 Seconds.mp4")["sha256"] ==
                   direct["media_sha256"], "The owned direct-playback source differs from its creation hash")

        hls = self._private(PRIVATE_ROOT / "hls-goby-verification.json")
        self.check(hls.get("owner") == "goby-hls-verification-v1" and
                   hls.get("root") == str(FIXTURE_ROOT / "hls-reference") and
                   hls.get("name") == FIXTURES["hls"]["name"] and
                   hls.get("library_id") == FIXTURES["hls"]["id"] and
                   hls.get("viewer_id") == direct["user_id"] and
                   hls.get("fixture_nonce") == direct["nonce"] and not hls.get("creation_pending") and
                   hls.get("source_sha256") == "332bce27f1e97d71d3ef7cffa59d8cf70cbcc51380a6b600e05048107432de4d",
                   "The HLS upgrade ownership record does not match")
        self._marker(FIXTURE_ROOT / "hls-reference/.goby-managed", "goby-hls-normal-owned-v1")
        source = self._digest(FIXTURE_ROOT / "hls-reference/Reference HLS Normal (2026).mp4")
        self.check(source["bytes"] == 967651 and source["sha256"] == hls["source_sha256"],
                   "The owned HLS source differs from its creation hash")

        nextup = self._private(PRIVATE_ROOT / "nextup-goby-verification.json")
        self.check(nextup.get("owner") == "goby-nextup-verification" and
                   nextup.get("library_id") == FIXTURES["nextup"]["id"],
                   "The NextUp upgrade ownership record does not match")
        self._marker(FIXTURE_ROOT / "nextup-long-reference/.goby-managed", "goby-nextup-long-reference-owned-v1")

        subtitle = self._private(PRIVATE_ROOT / "subtitle-goby-verification.json")
        self.check(subtitle.get("owner") == "goby-subtitle-verification-v1" and
                   subtitle.get("root") == str(FIXTURE_ROOT / "subtitle-reference") and
                   subtitle.get("name") == FIXTURES["subtitle"]["name"] and
                   subtitle.get("library_id") == FIXTURES["subtitle"]["id"] and
                   subtitle.get("viewer_id") == direct["user_id"] and not subtitle.get("creation_pending"),
                   "The subtitle upgrade ownership record does not match")
        self._marker(FIXTURE_ROOT / "subtitle-reference/.goby-managed", "goby-subtitle-reference-owned-v1")

        audio = self._private(PRIVATE_ROOT / "audio-m4c-goby-verification.json")
        self.check(audio == self.audio_record and audio.get("owner") == AUDIO_OWNER and
                   audio.get("root") == str(FIXTURE_ROOT / "audio-m4c") and
                   audio.get("viewer_id") == direct["user_id"] and
                   audio.get("fixture_nonce") == direct["nonce"] and
                   ID_PATTERN.fullmatch(str(audio.get("library_id", ""))) is not None and
                   audio["library_id"] not in {entry["id"] for entry in FIXTURES.values()} and
                   not audio.get("creation_pending") and isinstance(audio.get("media_hashes"), dict) and
                   set(audio["media_hashes"]) == AUDIO_NAMES and
                   all(HASH_PATTERN.fullmatch(str(value)) is not None for value in audio["media_hashes"].values()),
                   "The audio upgrade ownership record does not match the caller's verified record")
        self._directory(FIXTURE_ROOT / "audio-m4c")
        audio_marker = self._private(FIXTURE_ROOT / "audio-m4c/.goby-managed")
        self.check(audio_marker == {"owner": AUDIO_OWNER, "fixture_nonce": direct["nonce"]},
                   "The audio upgrade fixture marker does not match")
        for name, digest in audio["media_hashes"].items():
            self.check(self._digest(FIXTURE_ROOT / "audio-m4c" / name)["sha256"] == digest,
                       "An owned audio source differs from its creation hash")
        self.libraries = {key: dict(value) for key, value in FIXTURES.items()}
        self.libraries["audio"] = {"id": audio["library_id"], "name": AUDIO_NAME,
                                   "root": "audio-m4c", "collection": "music", "media_count": 5}

    def _api_libraries(self) -> None:
        listed = self.api.request("GET", "/admin/v1/libraries", admin=True,
                                  label="Upgrade fixture administrator ownership lookup")
        self.check(isinstance(listed, dict) and isinstance(listed.get("Items"), list),
                   "The administrator library listing has an unexpected shape")
        for value in self.libraries.values():
            root = str(FIXTURE_ROOT / value["root"])
            matches = [entry for entry in listed["Items"] if entry.get("Id") == value["id"] or
                       entry.get("Name") == value["name"] or root in entry.get("Paths", [])]
            self.check(len(matches) == 1 and matches[0].get("Id") == value["id"] and
                       matches[0].get("Name") == value["name"] and matches[0].get("Paths") == [root] and
                       matches[0].get("CollectionType") == value["collection"],
                       "An administrator library differs from its exact upgrade ownership record")

    def _file_snapshot(self) -> dict:
        files, directories = {}, {}
        total = 0
        for key, value in self.libraries.items():
            root = FIXTURE_ROOT / value["root"]
            pending = [root]
            while pending:
                directory = pending.pop()
                self._directory(directory)
                relative = key + "/" + directory.relative_to(root).as_posix()
                directories[relative] = list(file_identity(directory.lstat()))
                self.check(len(directories) <= MAX_TOTAL_FILES, "Upgrade directory snapshot limit exceeded")
                for child in sorted(directory.iterdir()):
                    info = child.lstat()
                    if stat.S_ISDIR(info.st_mode):
                        pending.append(child)
                        continue
                    entry = self._digest(child)
                    relative = key + "/" + child.relative_to(root).as_posix()
                    files[relative] = entry
                    total += entry["bytes"]
                    self.check(len(files) <= MAX_TOTAL_FILES and total <= MAX_TOTAL_BYTES,
                               "Upgrade fixture snapshot exceeded its bounded scope")
        names = set(files)
        self.check({name.removeprefix("direct/") for name in names if name.startswith("direct/")} == {
                       ".goby-direct-playback-owned.json", ".goby-direct-playback-state.json",
                       ".goby-direct-playback.lock", "Goby Direct Playback 600 Seconds.mp4"},
                   "Unexpected files exist in the owned direct-playback upgrade fixture")
        self.check({name.removeprefix("hls/") for name in names if name.startswith("hls/")} == {
                       ".goby-managed", "Reference HLS Normal (2026).mp4"},
                   "Unexpected files exist in the owned HLS upgrade fixture")
        nextup = {name.removeprefix("nextup/") for name in names if name.startswith("nextup/")}
        self.check(len(nextup) == 4 and ".goby-managed" in nextup and
                   all(name.endswith(".mp4") for name in nextup - {".goby-managed"}),
                   "The owned NextUp upgrade fixture does not contain exactly three episodes")
        title = "Reference Subtitle M3c (2026)"
        subtitle_names = {".goby-managed", *(title + "/" + title + extension
                                            for extension in (".mp4", ".en.srt", ".en.forced.vtt"))}
        self.check({name.removeprefix("subtitle/") for name in names if name.startswith("subtitle/")} == subtitle_names,
                   "Unexpected files exist in the owned subtitle upgrade fixture")
        self.check({name.removeprefix("audio/") for name in names if name.startswith("audio/")} ==
                   AUDIO_NAMES | {".goby-managed"},
                   "Unexpected files exist in the owned audio upgrade fixture")
        return {"files": files, "directories": directories, "bytes": total}

    def _record_snapshot(self) -> dict:
        result = {}
        for index, path in enumerate(self.record_paths):
            self.smoke.private_file(path)
            result[str(index)] = self._digest(path)
        return result

    def _database_snapshot(self) -> dict:
        # IDs are strict generated hexadecimal values, never SQL fragments.
        ids = [entry["id"] for entry in self.libraries.values()]
        self.check(len(ids) == 5 and len(set(ids)) == 5 and
                   all(ID_PATTERN.fullmatch(value) is not None for value in ids),
                   "Upgrade database selectors are outside the authorized scope")
        selectors = ",".join("'" + value + "'" for value in ids)
        query = """
WITH selected AS MATERIALIZED (
    SELECT i.* FROM items i WHERE i.library_id IN (SELECTOR)
), selected_ids AS MATERIALIZED (SELECT id FROM selected)
SELECT jsonb_build_object(
    'item_count', (SELECT count(*) FROM selected),
    'user_row_count', (SELECT count(*) FROM user_item_data),
    'selected_user_row_count', (SELECT count(*) FROM user_item_data WHERE item_id IN (SELECT id FROM selected_ids)),
    'items', COALESCE((SELECT jsonb_agg(to_jsonb(i) - 'media' - 'updated_at' - 'probed_at' ORDER BY i.id COLLATE "C")
        FROM (SELECT * FROM selected ORDER BY id COLLATE "C" LIMIT 257) i), '[]'::jsonb),
    'media', COALESCE((SELECT jsonb_agg(jsonb_build_object('id', i.id, 'library_id', i.library_id,
        'path', i.path, 'type', i.type, 'probe_version', i.media -> 'ProbeVersion') ORDER BY i.id COLLATE "C")
        FROM (SELECT * FROM selected WHERE media IS NOT NULL ORDER BY id COLLATE "C" LIMIT 257) i), '[]'::jsonb),
    'libraries', COALESCE((SELECT jsonb_agg(to_jsonb(l) - 'last_scan_at' ORDER BY l.id COLLATE "C")
        FROM libraries l WHERE l.id IN (SELECTOR)), '[]'::jsonb),
    'roots', COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id COLLATE "C")
        FROM library_roots r WHERE r.library_id IN (SELECTOR)), '[]'::jsonb),
    'user_data', COALESCE((SELECT jsonb_agg(to_jsonb(d) ORDER BY d.user_id COLLATE "C", d.item_id COLLATE "C")
        FROM (SELECT * FROM user_item_data ORDER BY user_id COLLATE "C", item_id COLLATE "C" LIMIT 4097) d), '[]'::jsonb),
    'item_entities', COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY e.item_id COLLATE "C", e.entity_id, e.position)
        FROM item_entities e WHERE e.item_id IN (SELECT id FROM selected_ids)), '[]'::jsonb),
    'entities', COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY e.id) FROM catalog_entities e
        WHERE e.id IN (SELECT entity_id FROM item_entities WHERE item_id IN (SELECT id FROM selected_ids))), '[]'::jsonb),
    'images', COALESCE((SELECT jsonb_agg(to_jsonb(i) ORDER BY i.item_id COLLATE "C", i.image_type COLLATE "C", i.image_index)
        FROM item_images i WHERE i.item_id IN (SELECT id FROM selected_ids)), '[]'::jsonb),
    'subtitles', COALESCE((SELECT jsonb_agg(to_jsonb(s) ORDER BY s.item_id COLLATE "C", s.stream_index)
        FROM item_subtitles s WHERE s.item_id IN (SELECT id FROM selected_ids)), '[]'::jsonb)
);
""".replace("SELECTOR", selectors)
        snapshot = self.db.read(query, "Read-only complete upgrade catalog and user-state snapshot")
        self.check(isinstance(snapshot, dict) and 0 < snapshot.get("item_count", 0) <= MAX_ITEMS and
                   snapshot["item_count"] == len(snapshot.get("items", [])) and
                   0 <= snapshot.get("user_row_count", -1) <= MAX_USER_ROWS and
                   snapshot["user_row_count"] == len(snapshot.get("user_data", [])),
                   "Upgrade database snapshot exceeded its bounds or omitted rows")
        self.check(len(snapshot["libraries"]) == len(snapshot["roots"]) == 5,
                   "Upgrade libraries no longer have exactly one recorded root each")
        for key, value in self.libraries.items():
            roots = [entry for entry in snapshot["roots"] if entry["library_id"] == value["id"]]
            self.check(len(roots) == 1 and roots[0]["path"] == str(FIXTURE_ROOT / value["root"]) and
                       roots[0]["allowed_path"] == str(FIXTURE_ROOT) and roots[0]["relative_path"] == value["root"],
                       "A database library root differs from its authorized fixture path")
            media = [entry for entry in snapshot["media"] if entry["library_id"] == value["id"]]
            self.check(len(media) == value["media_count"] and
                       all(isinstance(entry["probe_version"], int) and not isinstance(entry["probe_version"], bool) and
                           1 <= entry["probe_version"] <= 4 for entry in media),
                       "An upgrade library has an unexpected media count or probe version")
            for entry in media:
                path = Path(entry["path"])
                root = FIXTURE_ROOT / value["root"]
                self.check(path != root and path.is_relative_to(root) and path.resolve(strict=True) == path,
                           "A catalog media path escapes its exact fixture root")
                self.check(path.is_file() and entry["type"] == ("Audio" if key == "audio" else
                           "Episode" if key == "nextup" else "Movie"),
                           "An upgrade media item has an unexpected source type")
        self.check(sum(item["library_id"] != self.libraries["audio"]["id"] for item in snapshot["items"]) == 14,
                   "The four previously verified libraries changed their item population")
        return snapshot

    @staticmethod
    def _stable(snapshot: dict) -> dict:
        return {key: value for key, value in snapshot.items() if key != "media"}

    def _summary(self, snapshot: dict, files: dict) -> dict:
        versions = {}
        for entry in snapshot["media"]:
            version = str(entry["probe_version"])
            versions[version] = versions.get(version, 0) + 1
        return {"library_count": len(self.libraries), "item_count": snapshot["item_count"],
                "media_count": len(snapshot["media"]), "probe_versions": versions,
                "all_user_state_rows": snapshot["user_row_count"],
                "selected_user_state_rows": snapshot["selected_user_row_count"],
                "fixture_file_count": len(files["files"]), "fixture_file_bytes": files["bytes"],
                "metadata_sha256": aggregate({key: value for key, value in self._stable(snapshot).items()
                                              if key not in {"user_data", "user_row_count", "selected_user_row_count"}}),
                "all_user_state_sha256": aggregate(snapshot["user_data"]),
                "fixture_snapshot_sha256": aggregate(files)}

    def prepare(self) -> dict:
        """Verify existing ownership and capture a complete immutable baseline."""
        self.check(not self.prepared and not self.finished and self.before is None,
                   "Upgrade preparation cannot be repeated on the same verifier")
        self.check(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")),
                   "Run upgrade verification only through SSH on the authorized Linux test host")
        self._ownership()
        self._api_libraries()
        self.before_root = self._digest(FIXTURE_ROOT / ".goby-managed")
        self.before_records = self._record_snapshot()
        self.before_files = self._file_snapshot()
        self.before = self._database_snapshot()
        self.prepared = True
        return self._summary(self.before, self.before_files)

    def run(self) -> list[dict]:
        """Request and await exactly one normal scan per authorized library."""
        self.check(self.prepared and not self.finished and not self.scan_results and
                   not self.pending_jobs and not self.unconfirmed_scans,
                   "Upgrade scans require a fresh prepared baseline")
        for key, value in self.libraries.items():
            self._api_libraries()
            existing = self._listed_library_jobs(value["id"], "Upgrade scan pre-request job baseline")
            self.check(not any(job["Status"] in {"pending", "running"} for job in existing),
                       "An authorized upgrade library already has an active scan")
            previous_ids = {job["Id"] for job in existing}
            # The server may commit a scan before HTTP fails or its response is
            # decoded. Keep this fence until a valid new job is acknowledged.
            self.unconfirmed_scans[value["id"]] = previous_ids
            response = self.api.request("POST", "/admin/v1/libraries/" + quote(value["id"], safe="") + "/scan",
                                        admin=True, expected=(202,), label="Owned normal ProbeVersion upgrade scan")
            self.check(isinstance(response, dict) and isinstance(response.get("Job"), dict),
                       "The upgrade scan response does not contain an acknowledged job")
            job = response["Job"]
            job_id = job.get("Id", "")
            self.check(isinstance(job_id, str) and ID_PATTERN.fullmatch(job_id) is not None and
                       job_id not in previous_ids and job.get("LibraryId") == value["id"] and
                       job.get("Status") in {"pending", "running", "completed", "cancelled", "failed", "interrupted"},
                       "The upgrade scan response is not bound to its requested library")
            self.pending_jobs[job_id] = value["id"]
            del self.unconfirmed_scans[value["id"]]
            result = self.api.wait_job(job_id, timeout=90)
            self.check(result.get("Id") == job_id and result.get("LibraryId") == value["id"] and
                       result.get("Status") == "completed" and not result.get("Error") and
                       result.get("Scanned") == value["media_count"] and result.get("Added") == 0 and
                       isinstance(result.get("Updated"), int) and 0 <= result["Updated"] <= value["media_count"],
                       "An owned normal upgrade scan did not complete without catalog additions")
            del self.pending_jobs[job_id]
            self.scan_results.append({"fixture": key, "status": "completed", "scanned": result["Scanned"],
                                      "added": 0, "updated": result["Updated"]})
        self.finished = True
        return [dict(value) for value in self.scan_results]

    def _preservation_observation(self) -> tuple[dict, dict]:
        """Read current state and compare it with the original prepared baseline."""
        self.check(self.prepared and not self.pending_jobs and not self.unconfirmed_scans,
                   "Upgrade preservation requires a prepared baseline and no pending or unconfirmed scans")
        self._api_libraries()
        # A pre-request idle check may have failed before this instance sent
        # anything. Never treat empty local tracking as proof that such an
        # independently owned scan has stopped.
        for value in self.libraries.values():
            listed = self._listed_library_jobs(value["id"], "Upgrade preservation scan-idle observation")
            self.check(not any(job["Status"] in {"pending", "running"} for job in listed),
                       "An upgrade fixture still has an active scan during preservation observation")
        after = self._database_snapshot()
        files = self._file_snapshot()
        self.check(self._stable(after) == self._stable(self.before),
                   "A normal probe upgrade changed stable catalog metadata or user state")
        before_media = [{key: value for key, value in entry.items() if key != "probe_version"}
                        for entry in self.before["media"]]
        after_media = [{key: value for key, value in entry.items() if key != "probe_version"}
                       for entry in after["media"]]
        self.check(before_media == after_media, "A normal probe upgrade changed media IDs or source paths")
        self.check(files == self.before_files and self._record_snapshot() == self.before_records and
                   self._digest(FIXTURE_ROOT / ".goby-managed") == self.before_root,
                   "A normal probe upgrade changed fixture inputs or private ownership records")
        return after, files

    def _preservation_summary(self) -> dict:
        return {"metadata_preserved": True, "all_user_state_preserved": True,
                "fixture_files_preserved": True, "ownership_records_preserved": True,
                "ownership_record_count": len(self.before_records),
                "ownership_records_sha256": aggregate(self.before_records)}

    def verify(self) -> dict:
        """Recheck current probes and all stable inputs after a completed upgrade.

        Repeated calls make fresh read-only observations against the same
        original baseline, including after subsequent media verification.
        """
        self.check(self.prepared and self.finished and not self.pending_jobs and not self.unconfirmed_scans,
                   "Upgrade postconditions require all five owned scans to finish")
        after, files = self._preservation_observation()
        self.check(all(entry["probe_version"] == 4 for entry in after["media"]),
                   "A normally rescanned source did not reach ProbeVersion 4")
        self.verified = True
        return {"status": "passed", "before": self._summary(self.before, self.before_files),
                "after": self._summary(after, files), "scans": [dict(value) for value in self.scan_results],
                **self._preservation_summary(),
                "all_media_probe_version": 4}

    def audit_preservation(self) -> dict:
        """Audit preserved state even after a partial or cancelled upgrade.

        This result describes preservation only. It does not certify that the
        upgrade scan sequence completed or that every probe reached version 4.
        Call cancel_pending() first when an earlier scan failed or timed out.
        """
        after, files = self._preservation_observation()
        observed = self._summary(after, files)
        return {"status": "preserved", "before": self._summary(self.before, self.before_files),
                "observed": observed, "observed_probe_versions": observed["probe_versions"],
                "completed_scan_count": len(self.scan_results), "expected_scan_count": len(self.libraries),
                **self._preservation_summary()}

    def _listed_library_jobs(self, library_id: str, label: str) -> list[dict]:
        listed = self.api.request("GET", "/admin/v1/jobs", admin=True, label=label)
        self.check(isinstance(listed, dict) and isinstance(listed.get("Items"), list),
                   "The upgrade scan job listing has an unexpected shape")
        self.check(all(isinstance(entry, dict) for entry in listed["Items"]),
                   "The upgrade scan job listing contains an invalid entry")
        matches = [entry for entry in listed["Items"] if entry.get("LibraryId") == library_id]
        self.check(all(isinstance(entry.get("Id"), str) and ID_PATTERN.fullmatch(entry["Id"]) is not None and
                       entry.get("Status") in {"pending", "running", "completed", "cancelled", "failed", "interrupted"}
                       for entry in matches) and len({entry["Id"] for entry in matches}) == len(matches),
                   "An upgrade library job has an invalid identity or status")
        return matches

    def cancel_pending(self) -> list[str]:
        """Cancel acknowledged jobs and only observe unconfirmed scan requests.

        A unique new job after an unconfirmed HTTP request is a candidate, not
        proof of ownership. Observe it to a terminal state without cancelling
        it. Ambiguous or missing candidates retain the audit fence.
        """
        errors = []
        for job_id, library_id in list(self.pending_jobs.items()):
            try:
                listed = self.api.request("GET", "/admin/v1/jobs", admin=True,
                                          label="Owned upgrade scan cleanup lookup")["Items"]
                matches = [entry for entry in listed if entry.get("Id") == job_id]
                self.check(len(matches) == 1 and matches[0].get("LibraryId") == library_id,
                           "The owned upgrade cleanup job cannot be identified safely")
                self.check(matches[0].get("Status") in {
                               "pending", "running", "cancelled", "completed", "failed", "interrupted"},
                           "The owned upgrade cleanup job has an unknown status")
                if matches[0].get("Status") in {"pending", "running"}:
                    self.api.request("POST", "/admin/v1/jobs/" + quote(job_id, safe="") + "/cancel", admin=True,
                                     expected=(202,), label="Owned upgrade scan cancellation")
                    result = self.api.wait_job(job_id, timeout=30)
                    self.check(result.get("Id") == job_id and result.get("LibraryId") == library_id and
                               result.get("Status") in {"cancelled", "completed", "failed", "interrupted"},
                               "An owned upgrade scan did not reach a terminal cleanup state")
                del self.pending_jobs[job_id]
            except Exception:
                errors.append("An owned upgrade scan could not be confirmed terminal during cleanup")
        for library_id, previous_ids in list(self.unconfirmed_scans.items()):
            try:
                listed = self._listed_library_jobs(library_id, "Unconfirmed upgrade scan observation lookup")
                candidates = [entry for entry in listed if entry["Id"] not in previous_ids]
                self.check(len(candidates) == 1,
                           "An unconfirmed upgrade scan has no unique new observation candidate")
                job_id = candidates[0]["Id"]
                result = self.api.wait_job(job_id, timeout=90)
                self.check(result.get("Id") == job_id and result.get("LibraryId") == library_id and
                           result.get("Status") in {"cancelled", "completed", "failed", "interrupted"},
                           "An unconfirmed upgrade scan candidate did not reach an observed terminal state")
                del self.unconfirmed_scans[library_id]
            except Exception:
                errors.append("An unconfirmed upgrade scan could not be observed unambiguously to a terminal state")
        return errors
