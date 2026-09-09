#!/usr/bin/env python3
"""Verify administrator metadata editing with isolated Linux media and NFO files.

The shared browser runner owns database/HBA/process cleanup. This scenario adds
only run-owned media, metadata assertions, and a catalog-preserving restart.
"""

import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import signal
import sys


FIXTURE_MARKER = "goby-metadata-browser-fixtures-v1"
MOVIE_NFO = """<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Automatic movie</title>
  <originaltitle>Original automatic</originaltitle>
  <plot>Automatic overview one</plot>
  <year>2024</year>
  <premiered>2024-01-02T16:30:45.123456789Z</premiered>
  <rating>7.5</rating>
  <mpaa>PG</mpaa>
  <genre>Drama</genre>
  <tag>Initial</tag>
  <studio>Initial Studio</studio>
  <uniqueid type="tmdb">100</uniqueid>
  <actor><name>Initial Actor</name><role>Lead</role><order>0</order></actor>
</movie>
"""
EPISODE_NFO = """<?xml version="1.0" encoding="UTF-8"?>
<episodedetails>
  <title>Automatic episode</title>
  <plot>Automatic episode overview</plot>
  <season>1</season>
  <episode>1</episode>
</episodedetails>
"""
MUTABLE_NFO = """<?xml version="1.0" encoding="UTF-8"?>
<episodedetails>
  <title>Reclassifiable episode</title>
  <season>1</season>
  <episode>1</episode>
</episodedetails>
"""


def load_core(path):
    # The shared runner is imported read-only; importing it does not execute its
    # CLI or emit bytecode into a prepared source snapshot.
    sys.dont_write_bytecode = True
    specification = importlib.util.spec_from_file_location("goby_browser_verification", path)
    if specification is None or specification.loader is None:
        raise RuntimeError("The shared browser verifier could not be loaded.")
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


def create_runner(core, args):
    # These module values are private to this one Python process. Invoking the
    # original script separately retains its M5a names and defaults.
    core.MARKER = "goby-metadata-browser-v1"
    core.RUN_ENV = "GOBY_METADATA_RUN_ID"

    class MetadataRunner(core.Runner):
        browser_spec = "metadata-management.spec.ts"
        browser_timeout_seconds = 360
        screenshot_names = (
            "metadata-items-desktop.png", "metadata-editor-desktop.png",
            "metadata-sources-desktop.png", "metadata-editor-mobile.png", "metadata-inactive-desktop.png",
        )

        def __init__(self, arguments):
            super().__init__(arguments)
            self.database = "goby_m5b_metadata_" + self.run_id
            self.role = "goby_m5b_role_" + self.run_id
            self.pg_app_name = "goby_m5b_control_" + self.run_id
            core.BASE_ENV["PGAPPNAME"] = self.pg_app_name
            self.output = core.EXEC_ROOT / ("goby-metadata-" + self.run_id)
            self.runtime = Path("/dev/shm") / ("goby-metadata-" + self.run_id)
            self.admin_name = "m5b-admin-" + self.run_id
            self.report.update({"scenario": "administrator_metadata", "database": self.database, "role": self.role})
            self.media_files = {}

        def allocate(self):
            super().allocate()
            self.report["verifier_sha256"] = core.file_digest(Path(__file__).resolve())
            self.report["shared_runner_sha256"] = core.file_digest(args.core_runner.resolve())

        def prepare_files(self):
            super().prepare_files()
            # Retain the type-only import target used by the isolated spec.
            (self.browser_work / "src").mkdir(mode=0o700)
            shutil.copyfile(self.args.snapshot / "web/admin/src/api.ts", self.browser_work / "src/api.ts")
            media = self.runtime / "media"
            # The application can inspect these fixtures, but only the root
            # verification driver can replace their sidecars or media bytes.
            os.chown(self.runtime, 0, 0)
            os.chmod(self.runtime, 0o755)
            os.chown(media, 0, 0)
            os.chmod(media, 0o755)
            movies = media / "movies"
            television = media / "tv"
            mixed = media / "mixed"
            episode_directory = television / "Metadata Show" / "Season 1"
            movies.mkdir(mode=0o755)
            mixed.mkdir(mode=0o755)
            episode_directory.mkdir(mode=0o755, parents=True)
            movie = movies / "Automatic.Movie.2024.mp4"
            core.command([
                "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin",
                "-f", "lavfi", "-i", "color=c=blue:s=160x90:r=10:d=1",
                "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=1",
                "-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-preset", "ultrafast",
                "-crf", "28", "-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "32k",
                "-threads", "1", "-filter_threads", "1", "-shortest", "-movflags", "+faststart", movie,
            ], timeout=30)
            core.require(movie.stat().st_size <= 128 * 1024, "The synthetic media fixture exceeded its byte budget.")
            episode = episode_directory / "Episode.S01E01.mp4"
            mutable_episode = mixed / "Mutable.S01E01.mp4"
            mutable_movie = mixed / "Reclassified.Movie.mp4"
            paths = [movie]
            for number in range(1, 26):
                destination = movies / f"Page.Fixture.{number:03d}.mp4"
                shutil.copyfile(movie, destination)
                paths.append(destination)
            shutil.copyfile(movie, episode)
            paths.append(episode)
            shutil.copyfile(movie, mutable_episode)
            paths.append(mutable_episode)
            for path in paths:
                os.chown(path, 0, 0)
                path.chmod(0o444)
                self.media_files[path.relative_to(media).as_posix()] = core.file_digest(path)
            movie_nfo = movie.with_suffix(".nfo")
            episode_nfo = episode.with_suffix(".nfo")
            mutable_nfo = mutable_episode.with_suffix(".nfo")
            for path, content in ((movie_nfo, MOVIE_NFO), (episode_nfo, EPISODE_NFO), (mutable_nfo, MUTABLE_NFO)):
                core.private_write(path, content.encode("utf-8"))
                path.chmod(0o444)
            core.private_write(media / ".goby-metadata-fixtures", FIXTURE_MARKER.encode())
            self.manifest = media / "fixture-manifest.json"
            core.private_write(self.manifest, (json.dumps({
                "Marker": FIXTURE_MARKER, "RunId": self.run_id, "Root": str(media),
                "MoviesPath": str(movies), "TVPath": str(television),
                "MixedPath": str(mixed), "MutableEpisodePath": str(mutable_episode),
                "MutableMoviePath": str(mutable_movie), "MutableNFOPath": str(mutable_nfo),
                "MoviePath": str(movie), "MovieNFOPath": str(movie_nfo),
                "EpisodePath": str(episode), "EpisodeNFOPath": str(episode_nfo),
                "MediaSHA256": self.media_files,
            }, indent=2, sort_keys=True) + "\n").encode())
            self.report["fixtures"] = {
                "movie_count": 26, "episode_count": 2, "media_bytes_each": movie.stat().st_size,
                "all_media_sha256": core.digest(json.dumps(self.media_files, sort_keys=True).encode()),
                "initial_movie_nfo_sha256": core.file_digest(movie_nfo),
                "initial_episode_nfo_sha256": core.file_digest(episode_nfo),
                "root_owned_read_only_to_application": True,
            }

        def browser_environment(self):
            environment = super().browser_environment()
            environment.pop("GOBY_SMOKE_USERS_DISPOSABLE_DATABASE", None)
            environment.pop("GOBY_SMOKE_USERS_DEDICATED_ADMIN", None)
            environment.update({
                "GOBY_SMOKE_METADATA_DISPOSABLE_DATABASE": "1",
                "GOBY_SMOKE_METADATA_DEDICATED_ADMIN": "1",
                "GOBY_SMOKE_METADATA_FIXTURE_MANIFEST": str(self.manifest),
            })
            return environment

        def account_snapshot(self):
            # Hash full stored state, including source values, manual overrides,
            # locks, revisions, and entity associations. The response never
            # publishes credentials or a raw metadata/database dump.
            statement = """SELECT jsonb_build_object(
                'users', (SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
                'libraries', (SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM libraries l),
                'roots', (SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM library_roots r),
                'items', (SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
                'metadata_state', (SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m),
                'entities', (SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM catalog_entities e),
                'associations', (SELECT jsonb_agg(to_jsonb(a) ORDER BY item_id, entity_id, position) FROM item_entities a),
                'userdata', (SELECT jsonb_agg(to_jsonb(d) ORDER BY user_id, item_id) FROM user_item_data d),
                'settings', (SELECT jsonb_agg(to_jsonb(s) ORDER BY key) FROM server_settings s))::text;"""
            value = core.command([
                "runuser", "--user", "postgres", "--", core.PG_BIN / "psql", "-X", "-v",
                "ON_ERROR_STOP=1", "-At", "-h", core.PG_SOCKET, "-p", str(core.PG_PORT), "-d", self.database,
            ], text=statement)
            return core.digest(value.encode())

        def verify_restart(self):
            super().verify_restart()
            media = self.runtime / "media"
            observed = {relative: core.file_digest(media / relative) for relative in self.media_files}
            core.require(observed == self.media_files, "Metadata editing changed original synthetic media bytes.")
            self.report["checks"]["original_media_bytes_unchanged"] = True
            self.report["checks"]["restart_preserved_metadata_and_entities"] = True
            self.report["fixtures"]["final_movie_nfo_sha256"] = core.file_digest(Path(json.loads(self.manifest.read_text())["MovieNFOPath"]))
            self.report["fixtures"]["final_episode_nfo_sha256"] = core.file_digest(Path(json.loads(self.manifest.read_text())["EpisodeNFOPath"]))

    return MetadataRunner(args)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snapshot", type=Path, required=True)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--binary-sha256", required=True)
    parser.add_argument("--assets", type=Path, required=True)
    parser.add_argument("--core-runner", type=Path, default=Path(__file__).with_name("verify-managed-users.py"))
    args = parser.parse_args()
    core = load_core(args.core_runner)
    core.require(re.fullmatch(r"[0-9a-f]{64}", args.binary_sha256) is not None, "Supply the prepared binary SHA-256.")
    def interrupted(signum, frame):
        raise core.VerificationError("The verifier was interrupted; owned resources are being cleaned up.")
    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(signum, interrupted)
    return create_runner(core, args).execute()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        # Exceptions may carry a pathname, SQL, or connection details.
        print(json.dumps({"status": "failed", "failure": "Metadata verifier setup failed: " + type(error).__name__}))
        sys.exit(1)
