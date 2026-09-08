#!/usr/bin/env python3
"""Check indexed artwork and persistent entities on the authorized Linux host.

Run beside nfo-smoke.py after schema 5 is deployed. Existing synthetic credentials
stay in memory. One marked fixture owns the copied movie, NFO, and generated PNG;
the source movie, reference sidecars, reference images, and services are untouched.
"""

from __future__ import annotations

import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import sys
from urllib.parse import quote, urlencode
import xml.etree.ElementTree as ET
import zlib


spec = importlib.util.spec_from_file_location("goby_nfo_smoke", Path(__file__).with_name("nfo-smoke.py"))
core = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = core
spec.loader.exec_module(core)
check = core.check
POSTER = "poster.png"
NEXT_POSTER = "poster.next"
KINDS = {
    "genre": ("Genres", "GenreItems", "GenreIds", "Genre"),
    "tag": ("Tags", "TagItems", "TagIds", "Tag"),
    "studio": ("Studios", "Studios", "StudioIds", "Studio"),
    "person": ("Persons", "People", "PersonIds", "Person"),
}


def image_request(method: str, path: str, *, headers=None, expected=200):
    connection = http.client.HTTPConnection("127.0.0.1", 18096, timeout=15)
    try:
        connection.request(method, path, headers=headers or {})
        response = connection.getresponse()
        data = response.read(2 * 1024 * 1024 + 1)
        check(len(data) <= 2 * 1024 * 1024, "Image response exceeded the smoke-check byte limit")
        check(response.status == expected, f"Image {method} returned HTTP {response.status}, expected {expected}")
        return {key.lower(): value for key, value in response.getheaders()}, data
    except (OSError, http.client.HTTPException) as error:
        raise core.SmokeFailure(f"Image {method} failed ({type(error).__name__})") from None
    finally:
        connection.close()


def png(red: int, green: int, blue: int) -> bytes:
    def chunk(kind: bytes, data: bytes) -> bytes:
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)
    pixels = (b"\x00" + bytes((red, green, blue)) * 96) * 64
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 96, 64, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(pixels)) + chunk(b"IEND", b""))


def decode_image(data: bytes, width: int, height: int, codec: str) -> None:
    configured = os.environ.get("GOBY_FFPROBE")
    candidates = list(Path("/opt/goby-toolchains").glob("ffmpeg-*/bin/ffprobe"))
    if configured:
        binary = configured
    elif candidates:
        binary = str(max(candidates, key=lambda path: tuple(
            int(part) if part.isdigit() else 0 for part in path.parent.parent.name.removeprefix("ffmpeg-").split(".")
        )))
    else:
        binary = shutil.which("ffprobe")
    check(bool(binary), "An existing ffprobe executable is required; no software is installed by this script")
    result = subprocess.run([binary, "-v", "error", "-protocol_whitelist", "pipe", "-threads", "1",
                             "-show_frames", "-show_entries", "stream=codec_name,width,height:frame=width,height",
                             "-of", "json", "-i", "pipe:0"], input=data, capture_output=True, timeout=10)
    check(result.returncode == 0, "ffprobe could not decode the returned image")
    decoded = json.loads(result.stdout)
    streams, frames = decoded.get("streams", []), decoded.get("frames", [])
    check(len(streams) == 1 and streams[0].get("codec_name") == codec and
          (streams[0].get("width"), streams[0].get("height")) == (width, height), "Image format or dimensions did not match")
    check(bool(frames) and all((frame.get("width"), frame.get("height")) == (width, height) for frame in frames),
          "The image did not decode to the expected frame dimensions")


def write_metadata(fixture, names: dict[str, str]) -> None:
    root = ET.Element("movie")
    for tag, text in (("title", "Artwork entity smoke movie"), ("year", "2026"),
                      ("genre", names["genre"]), ("tag", names["tag"]), ("studio", names["studio"])):
        ET.SubElement(root, tag).text = text
    actor = ET.SubElement(root, "actor")
    ET.SubElement(actor, "name").text = names["person"]
    ET.SubElement(actor, "role").text = "Synthetic role"
    path = fixture.owned() / core.NFO_NAME
    path.write_bytes(ET.tostring(root, encoding="utf-8", xml_declaration=True))
    path.chmod(0o644)


def movie(api, library_id: str, media_path: Path) -> dict:
    endpoint = f"/emby/Users/{quote(api.user_id)}/Items"
    query = urlencode({"ParentId": library_id, "Recursive": "true", "IncludeItemTypes": "Movie", "Fields": "Path", "Limit": 10})
    response = api.request("GET", endpoint + "?" + query, emby=True)
    items = response.get("Items", [])
    check(response.get("TotalRecordCount") == 1 and len(items) == 1 and items[0].get("Path") == str(media_path),
          "The isolated movie was not found through the real library API")
    return api.request("GET", endpoint + "/" + quote(items[0]["Id"]), emby=True)


def embedded_ids(item: dict, names: dict[str, str]) -> dict[str, str]:
    ids = {}
    for kind, (_, field, _, _) in KINDS.items():
        entries = [entry for entry in item.get(field, []) if entry.get("Name") == names[kind]]
        check(len(entries) == 1, "Embedded " + kind + " reference is missing or duplicated")
        value = entries[0].get("Id")
        if kind == "person":
            check(isinstance(value, str) and value.isdecimal() and int(value) > 0, "Embedded person ID must be a positive decimal string")
            check(entries[0].get("Type") == "Actor" and entries[0].get("Role") == "Synthetic role", "Person credit metadata did not match")
        else:
            check(type(value) is int and value > 0, "Embedded " + kind + " ID must be a positive JSON number")
        ids[kind] = str(value)
    check("Tags" not in item, "Tag projection must use TagItems rather than an invented Tags field")
    return ids


def entity_contracts(api, library_id: str, item: dict, names: dict[str, str]) -> dict[str, str]:
    ids = embedded_ids(item, names)
    endpoint = f"/emby/Users/{quote(api.user_id)}/Items"
    scope = {"UserId": api.user_id, "ParentId": library_id, "IncludeItemTypes": "Movie", "Limit": 10}
    for kind, (route, _, filter_name, type_name) in KINDS.items():
        listed = api.request("GET", "/emby/" + route + "?" + urlencode(scope), emby=True)
        entries = listed.get("Items", [])
        check(listed.get("TotalRecordCount") == 1 and len(entries) == 1, kind + " list must return the isolated entity envelope")
        check(entries[0].get("Name") == names[kind] and entries[0].get("Id") == ids[kind] and
              isinstance(entries[0].get("Id"), str), kind + " list ID does not match its embedded reference")
        detail = api.request("GET", endpoint + "/" + ids[kind], emby=True)
        check(detail.get("Id") == ids[kind] and detail.get("Name") == names[kind] and detail.get("Type") == type_name,
              kind + " generic-ID detail did not match")
        if kind != "tag":
            named = api.request("GET", "/emby/" + route + "/" + quote(names[kind], safe="") + "?" + urlencode({"UserId": api.user_id}), emby=True)
            check(named.get("Id") == ids[kind] and named.get("Name") == names[kind], kind + " by-name detail did not match")
        filtered = api.request("GET", endpoint + "?" + urlencode({"ParentId": library_id, "Recursive": "true",
                               "IncludeItemTypes": "Movie", filter_name: ids[kind], "Limit": 10}), emby=True)
        check(filtered.get("TotalRecordCount") == 1 and [entry.get("Id") for entry in filtered.get("Items", [])] == [item["Id"]],
              kind + " ID filter did not return the source movie")
    return ids


def scan(api, library_id: str):
    started = api.request("POST", f"/admin/v1/libraries/{quote(library_id)}/scan", admin=True, expected=(202,))["Job"]
    job = api.wait_job(started["Id"])
    check(job.get("Status") == "completed" and not job.get("Error") and job.get("Scanned") == 1,
          "The isolated media and image scan did not complete cleanly")
    return job


def main() -> int:
    os.umask(0o077)
    summary = {"status": "failed", "assertions": [], "cleanup_errors": []}
    api, fixture, source = core.API(), None, None
    source_hash, library_id = "", ""
    attempted = False
    stage = "preconditions"
    try:
        check(sys.platform == "linux" and os.geteuid() == 0, "Run only as root on the authorized Linux test host")
        check(not core.FIXTURE_ROOT.is_symlink() and core.FIXTURE_ROOT.resolve(strict=True) == core.FIXTURE_ROOT and
              (core.FIXTURE_ROOT / ".goby-managed").read_text().strip() == "goby-generated-media-fixtures", "Fixture root ownership did not match")
        values = core.credentials()
        source = Path(values["GOBY_SMOKE_MEDIA_FILE"]).resolve(strict=True)
        check(source.is_relative_to(core.FIXTURE_ROOT) and source.is_file() and source.suffix.lower() == ".mp4" and
              0 < source.stat().st_size <= 5 * 1024 * 1024, "The source must be a small owned synthetic MP4")
        source_hash = core.digest(source)
        summary["runtime_uid"] = core.runtime_uid()
        api.request("GET", "/readyz", parse=False)
        api.login(values)
        fixture = core.OwnedFixture(source, source_hash, prefix="artwork-entities-smoke-",
                                    marker_name=".goby-artwork-entities-smoke-owned.json", extra_files=(POSTER, NEXT_POSTER))
        fixture.prepare()
        media_path = fixture.owned() / core.MEDIA_NAME
        names = {kind: "Smoke " + kind + " " + fixture.directory.name for kind in KINDS}
        write_metadata(fixture, names)
        poster = fixture.owned() / POSTER
        original = png(23, 110, 130)
        poster.write_bytes(original)
        poster.chmod(0o644)
        source_tag = hashlib.sha256(original).hexdigest()
        stage = "initial catalog scan"
        attempted = True
        created = api.request("POST", "/admin/v1/libraries", admin=True, expected=(201,), body={
            "Name": fixture.directory.name, "CollectionType": "movies", "Paths": [str(fixture.directory)], "Scan": False,
        })
        library_id = created["Library"]["Id"]
        scan(api, library_id)
        item = movie(api, library_id, media_path)
        probe = core.probe_view(item)
        stage = "persistent entity routes and filters"
        ids = entity_contracts(api, library_id, item, names)
        summary["entity_ids"] = ids
        summary["assertions"].append({"stage": stage, "lists": 4, "generic_id_details": 4, "by_name_details": 3, "id_filters": 4})
        summary["by_name_scope"] = "Genres, Studios, and Persons have name routes; Tags is verified through its list and generic ID detail."
        stage = "public indexed artwork and protected enumeration"
        check(item.get("ImageTags", {}).get("Primary") == source_tag == core.digest(poster), "ImageTags.Primary is not the actual indexed source SHA-256")
        image_path = f"/emby/Items/{quote(item['Id'])}/Images/Primary/0"
        image_list = f"/emby/Items/{quote(item['Id'])}/Images"
        for bad in (False, True):
            headers = {"X-Emby-Token": "invalid-artwork-smoke-token"} if bad else {}
            for method in ("GET", "HEAD"):
                response, body = image_request(method, image_path, headers=headers)
                check(response.get("content-type", "").split(";")[0] == "image/png" and
                      response.get("etag") == '"' + source_tag + '"' and response.get("content-length") == str(len(original)),
                      "Public original image response metadata did not match")
                check(body == (original if method == "GET" else b""), "Public original image bytes or HEAD body did not match")
            image_request("GET", image_list, headers=headers, expected=401)
        images = api.request("GET", image_list, emby=True)
        check(isinstance(images, list) and len(images) == 1 and images[0].get("ImageType") == "Primary" and
              (images[0].get("Width"), images[0].get("Height")) == (96, 64), "Authenticated image enumeration did not match the indexed poster")
        decode_image(original, 96, 64, "png")
        summary["assertions"].append({"stage": stage, "public_get_head_without_or_with_bad_token": 200, "unauthorized_image_list": 401, "source_tag_matches_sha256": True})
        stage = "image transform and conditional cache"
        variant_path = image_path + "?" + urlencode({"MaxWidth": 48, "MaxHeight": 48, "Format": "jpg", "Quality": 71})
        variant_headers, variant = image_request("GET", variant_path)
        check(variant_headers.get("content-type", "").split(";")[0] == "image/jpeg" and
              bool(variant_headers.get("etag")) and variant_headers["etag"] != '"' + source_tag + '"', "JPEG transform headers did not match")
        decode_image(variant, 48, 32, "mjpeg")
        for method in ("GET", "HEAD"):
            response, body = image_request(method, variant_path, headers={"If-None-Match": variant_headers["etag"]}, expected=304)
            check(not body and response.get("etag") == variant_headers["etag"] and "cache-control" not in response and
                  "content-length" not in response, "Conditional image response did not preserve the 304 contract")
        tagged_path = image_path + "?" + urlencode({"Tag": source_tag})
        response, body = image_request("GET", tagged_path)
        check(body == original and response.get("cache-control") == "public, max-age=31536000" and
              bool(response.get("expires")) and bool(response.get("last-modified")), "Matching Tag did not enable the source-version cache policy")
        summary["assertions"].append({"stage": stage, "decoded_jpeg_dimensions": [48, 32], "conditional_get_head": 304, "matching_tag_max_age": 31536000})
        stage = "unscanned source replacement"
        replacement = png(177, 72, 96)
        replacement_path = fixture.owned() / NEXT_POSTER
        replacement_path.write_bytes(replacement)
        replacement_path.chmod(0o644)
        os.replace(replacement_path, poster)
        for method, path in (("GET", variant_path), ("HEAD", image_path)):
            response, _ = image_request(method, path, headers={"If-None-Match": "*"}, expected=503)
            check("etag" not in response, "An unscanned replacement reused a cached ETag")
        summary["assertions"].append({"stage": stage, "cached_get_and_head": 503})
        stage = "rescanned artwork and stable entity IDs"
        scan(api, library_id)
        updated = movie(api, library_id, media_path)
        new_tag = hashlib.sha256(replacement).hexdigest()
        check(updated["Id"] == item["Id"] and embedded_ids(updated, names) == ids and core.probe_view(updated) == probe,
              "Artwork rescan changed the movie, entity IDs, or media probe fields")
        check(new_tag != source_tag and updated.get("ImageTags", {}).get("Primary") == new_tag == core.digest(poster), "Rescan did not publish the replacement source SHA")
        response, body = image_request("GET", image_path)
        check(body == replacement and response.get("etag") == '"' + new_tag + '"', "Rescan did not replace the original image bytes and ETag")
        response, body = image_request("GET", tagged_path)
        check(body == replacement and response.get("cache-control") == "public", "An old Tag incorrectly advertised versioned cache freshness")
        response, body = image_request("GET", image_path + "?" + urlencode({"Tag": new_tag}))
        check(body == replacement and response.get("cache-control") == "public, max-age=31536000", "The replacement Tag cache policy did not match")
        response, new_variant = image_request("GET", variant_path)
        check(new_variant != variant and response.get("etag") != variant_headers["etag"], "The cached transform did not change after the source was rescanned")
        decode_image(new_variant, 48, 32, "mjpeg")
        summary["assertions"].append({"stage": stage, "ids_stable": True, "probe_fields_stable": True, "source_and_variant_bytes_updated": True})
        summary["poster_sha256_before"], summary["poster_sha256_after"] = source_tag, new_tag
        stage = "catalog deletion invalidates cached images"
        api.request("DELETE", f"/admin/v1/libraries/{quote(library_id)}", admin=True, expected=(204,), parse=False)
        library_id, attempted = "", False
        for method, path in (("GET", image_path), ("HEAD", variant_path)):
            image_request(method, path, headers={"If-None-Match": "*"}, expected=404)
        api.request("GET", image_list, emby=True, expected=(404,), parse=False)
        check(core.digest(media_path) == source_hash and core.digest(poster) == new_tag, "Native library deletion modified fixture files")
        api.request("GET", "/readyz", parse=False)
        summary["assertions"].append({"stage": stage, "cached_original_and_variant": 404, "media_and_poster_preserved": True, "ready_status": 200})
        summary["status"] = "passed"
    except Exception as error:
        summary["failed_stage"] = stage
        summary["error"] = str(error) if isinstance(error, core.SmokeFailure) else type(error).__name__
    finally:
        unknown = False
        if attempted and not library_id and fixture and fixture.directory:
            try:
                matches = [entry for entry in api.request("GET", "/admin/v1/libraries", admin=True)["Items"]
                           if entry.get("Name") == fixture.directory.name and entry.get("Paths") == [str(fixture.directory)]]
                check(len(matches) <= 1, "Ambiguous owned catalog creation")
                library_id = matches[0]["Id"] if matches else ""
            except Exception:
                unknown = True
                summary["cleanup_errors"].append("Owned library creation result is unknown; fixture retained")
        if library_id:
            try:
                for job in api.request("GET", "/admin/v1/jobs", admin=True)["Items"]:
                    if job.get("LibraryId") == library_id and job.get("Status") in {"pending", "running"}:
                        api.request("POST", f"/admin/v1/jobs/{quote(job['Id'])}/cancel", admin=True, expected=(202,))
                        api.wait_job(job["Id"], timeout=10)
                api.request("DELETE", f"/admin/v1/libraries/{quote(library_id)}", admin=True, expected=(204, 404), parse=False)
                library_id = ""
            except Exception:
                summary["cleanup_errors"].append("Owned catalog cleanup failed; fixture retained")
        if fixture and fixture.directory and not library_id and not unknown:
            try:
                fixture.cleanup()
                summary["owned_directory_removed"] = True
            except Exception:
                summary["cleanup_errors"].append("Owned fixture cleanup refused or failed")
        for mode, method, path, expected in (("emby", "POST", "/emby/Sessions/Logout", (200,)), ("admin", "DELETE", "/admin/v1/session", (204,))):
            if (mode == "emby" and api.token) or (mode == "admin" and api.cookie):
                try:
                    api.request(method, path, **{mode: True}, expected=expected, parse=False)
                except Exception:
                    summary["cleanup_errors"].append("The smoke-check " + mode + " session could not be revoked")
        if source and source_hash:
            try:
                summary["source_sha256_before"], summary["source_sha256_after"] = source_hash, core.digest(source)
                check(summary["source_sha256_after"] == source_hash, "Source movie changed")
                summary["source_media_unchanged"] = True
            except Exception:
                summary["cleanup_errors"].append("The source movie hash was not preserved or could not be verified")
        if summary["cleanup_errors"]:
            summary["status"] = "failed"
        print(json.dumps(summary, indent=2, sort_keys=True))
    return 0 if summary["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
