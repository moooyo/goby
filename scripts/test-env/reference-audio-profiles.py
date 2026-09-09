#!/usr/bin/env python3
"""Capture bounded Audio DeviceProfile PlaybackInfo contracts from Emby 4.9.5.

Only the existing isolated official reference and immutable owned audio sources
are used. New audio-profile-m4d records live in a separate marked tmpfs tree.
Credentials, raw responses, and exact wire data stay private. Returned URLs are
followed only within the reference origin and never placed in process argv.
"""

from __future__ import annotations

import argparse
import copy
import importlib.util
import json
import os
from pathlib import Path
import sys
import time
from urllib.parse import parse_qs, urlencode, urlsplit


sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location("reference_audio", Path(__file__).with_name("reference-audio.py"))
AUDIO = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(AUDIO)
BASE = AUDIO.BASE
OLD_AUDIO = AUDIO.ROOT
OLD_DEFAULTS = OLD_AUDIO.with_name("audio-m4c-defaults")
ROOT = OLD_AUDIO.with_name("audio-profile-m4d")
PRIVATE = ROOT / "private"
RAW, WIRE, EXPORT, PROBES = PRIVATE / "raw", PRIVATE / "wire", ROOT / "export", PRIVATE / "probe"
BASELINE = PRIVATE / "baseline.json"
CONTEXT = PRIVATE / "context.json"
PREFIX = "audio-profile-m4d-"
MARKER = "goby-audio-profile-m4d-owned-v1"
DEVICE = "goby-audio-profile-m4d-recorder"

# Reuse the bounded transport, sanitizer, and audio decoder with independent
# destinations. The original source directory remains read-only and unchanged.
for key, value in {"ROOT": ROOT, "PRIVATE": PRIVATE, "RAW": RAW, "WIRE": WIRE, "EXPORT": EXPORT,
                   "PROBES": PROBES, "BASELINE": BASELINE, "CONTEXT": CONTEXT, "PREFIX": PREFIX,
                   "MARKER": MARKER, "DEVICE": DEVICE, "MAX_TOTAL": 8 * 1024 * 1024}.items():
    setattr(AUDIO, key, value)


def check(condition: bool, label: str) -> None:
    AUDIO.check(condition, label)


def transcoding(protocol="http", container="mp3", codec="mp3", channels="2") -> dict:
    result = {"Type": "Audio", "Context": "Streaming", "Container": container,
              "AudioCodec": codec, "MaxAudioChannels": channels}
    if protocol is not None:
        result["Protocol"] = protocol
    if protocol == "hls":
        result.update({"SegmentLength": 3, "MinSegments": 1})
    return result


def profile(outputs=None, *, direct=None, conditions=None, bitrate=128000) -> dict:
    result = {"Name": "Goby audio profile reference", "MaxStreamingBitrate": bitrate,
              "DirectPlayProfiles": direct or [], "TranscodingProfiles": outputs if outputs is not None else [transcoding()]}
    if conditions is not None:
        result["CodecProfiles"] = [{"Type": "Audio", "Codec": "mp3", "Container": "mp3", "Conditions": conditions}]
    return result


def condition(kind: str, name: str, value: str) -> dict:
    return {"Condition": kind, "Property": name, "Value": value, "IsRequired": True}


class Recorder(AUDIO.Recorder):
    def clean_text(self, value: str) -> str:
        return super().clean_text(value).replace("/reference-audio-private", "/reference-audio-profile-private")

    def audit_export(self, original, exported, key="") -> None:
        if isinstance(original, str) and str(AUDIO.SOURCES) in original:
            check(exported == self.clean_text(original), "Immutable source path sanitization changed unrelated text")
            return
        super().audit_export(original, exported, key)

    def setup(self) -> None:
        AUDIO.preconditions(preparing=True)
        for directory, marker in ((OLD_AUDIO, "goby-audio-m4c-owned-v1"), (OLD_DEFAULTS, "goby-audio-m4c-defaults-owned-v1")):
            check(directory.resolve(strict=True) == directory and (directory / ".goby-managed").read_text().strip() == marker,
                  "Existing audio reference ownership marker does not match")
        folders = (BASE.PRIVATE / "raw", BASE.EXPORT, OLD_AUDIO / "private/raw", OLD_AUDIO / "export",
                   OLD_DEFAULTS / "private/raw", OLD_DEFAULTS / "export")
        old_records = {str(path): AUDIO.digest(path) for folder in folders for path in folder.glob("*.json")}
        check(len(old_records) == 1220, "Expected the preserved 610 preceding raw/export reference pairs")
        facts = json.loads((OLD_AUDIO / "private/sources.json").read_text())
        check(len(facts) == 4 and all(AUDIO.digest(Path(source["path"])) == source["sha256"] for source in facts),
              "An existing immutable audio source changed")
        context = json.loads((OLD_AUDIO / "private/context.json").read_text())
        ROOT.mkdir(mode=0o700)
        for folder in (PRIVATE, RAW, WIRE, EXPORT, PROBES):
            folder.mkdir(mode=0o700)
        AUDIO.private_write(ROOT / ".goby-managed", MARKER + "\n", exclusive=True)
        goby_pid = int(AUDIO.subprocess.check_output(["systemctl", "show", "goby-foundation-test.service", "-p", "MainPID", "--value"], timeout=5))
        AUDIO.private_write(BASELINE, json.dumps({"records": old_records, "sources": facts, "gobyPID": goby_pid}, indent=2), exclusive=True)
        AUDIO.private_write(CONTEXT, json.dumps(context, indent=2), exclusive=True)
        old_user = BASE.read_credentials(OLD_AUDIO / "private/user-credentials.env")
        BASE.save_credentials({key: old_user[key] for key in ("REFERENCE_USERNAME", "REFERENCE_PASSWORD")}, AUDIO.credentials("user"))
        self.use("user")
        login = self.request("user-login", "POST", "/emby/Users/AuthenticateByName",
                             body={"Username": self.credentials["REFERENCE_USERNAME"], "Pw": self.credentials["REFERENCE_PASSWORD"]})
        check(login["status"] == 200 and self.credentials["REFERENCE_USER_ID"] == old_user["REFERENCE_USER_ID"],
              "Audio profile login did not resolve the existing owned ordinary account")
        public = self.request("system-info", "GET", "/emby/System/Info/Public", headers={})
        check(public["status"] == 200 and public["parsed"].get("Version") == "4.9.5.0", "Unexpected official reference server version")
        self.write("runtime-before", {"kind": "remote-runtime-observation", "namespaceMatchesReference": True,
                                      "sourceHashes": {source["kind"]: source["sha256"] for source in facts}, **self.runtime()})

    def playback_info(self, name: str, source_kind: str, *, method="POST", device_profile=None,
                      request_values=None, minimal=False, fetch=True, cleanup=True, prefer_direct=False) -> dict:
        context = json.loads(CONTEXT.read_text())
        item = context["items"][source_kind]
        path = f"/emby/Items/{item['Id']}/PlaybackInfo"
        if method == "GET":
            body = None
            path += "?" + urlencode({"UserId": self.credentials["REFERENCE_USER_ID"], **(request_values or {})})
        elif minimal:
            body = {"UserId": self.credentials["REFERENCE_USER_ID"]}
            body.update(request_values or {})
        else:
            body = {"UserId": self.credentials["REFERENCE_USER_ID"], "MediaSourceId": item["MediaSourceId"],
                    "IsPlayback": True, "EnableDirectPlay": False, "EnableDirectStream": False,
                    "EnableTranscoding": True, "AllowAudioStreamCopy": False, "MaxStreamingBitrate": 128000,
                    "DeviceProfile": copy.deepcopy(device_profile or profile())}
            body.update(request_values or {})
        response = self.request(name + "-info", method, path, body=body, authenticated=True)
        value = response["parsed"] if isinstance(response["parsed"], dict) else {}
        sources = value.get("MediaSources", [])
        source = sources[0] if len(sources) == 1 and isinstance(sources[0], dict) else {}
        fields = ("Container", "SupportsDirectPlay", "SupportsDirectStream", "SupportsTranscoding", "TranscodingContainer",
                  "TranscodingSubProtocol", "DefaultAudioStreamIndex", "DefaultSubtitleStreamIndex", "AddApiKeyToDirectStreamUrl", "RunTimeTicks")
        source_fields = {key: source[key] for key in fields if key in source}
        observed = {"kind": "audio-profile-observation", "case": name, "sourceKind": source_kind, "method": method,
                    "playbackInfoStatus": response["status"], "sourceFields": source_fields,
                    "omittedSourceFields": [key for key in fields if key not in source],
                    "playSessionIdPresent": bool(value.get("PlaySessionId")),
                    "errorCode": value.get("ErrorCode"),
                    "hasTranscodingUrl": bool(source.get("TranscodingUrl")), "hasDirectStreamUrl": bool(source.get("DirectStreamUrl")),
                    "nativeAudioStreams": [{key: stream[key] for key in ("Index", "Codec", "Channels", "SampleRate", "BitDepth") if key in stream}
                                           for stream in source.get("MediaStreams", []) if stream.get("Type") == "Audio"]}
        current = (request_values or {}).get("CurrentPlaySessionId")
        if current is not None:
            observed["currentPlaySessionIdReused"] = value.get("PlaySessionId") == current
        ids = {value["PlaySessionId"]} if value.get("PlaySessionId") else set()
        offered = ["DirectStreamUrl", "TranscodingUrl"] if prefer_direct else ["TranscodingUrl", "DirectStreamUrl"]
        field = next((key for key in offered if source.get(key)), None)
        try:
            if fetch and response["status"] == 200 and field is not None:
                target = self.local(path, source[field])
                query = parse_qs(urlsplit(target).query, keep_blank_values=True)
                ids.update(query.get("PlaySessionId", []))
                observed.update({"selectedUrlField": field, "urlHasApiKey": bool(query.get("api_key")),
                                 "urlParameterNames": sorted(query),
                                 "urlAudioParameters": {key: value for key, value in query.items() if key in
                                     {"AudioCodec", "AudioBitrate", "AudioBitRate", "AudioSampleRate", "AudioChannels", "MaxAudioChannels",
                                      "audiochannels", "audiosamplerate", "AudioStreamIndex", "allowAudioStreamCopy", "TranscodeReasons",
                                      "TranscodingMaxAudioChannels", "StartTimeTicks", "Static", "Container", "SegmentLength", "MinSegments"}}})
                head = self.request(name + "-media-head", "HEAD", target, headers={},
                                    note="Uses the returned same-origin delivery URL without adding a token or altering its stream parameters.")
                fetched = self.request(name + "-media-get", "GET", target, headers={})
                observed.update({"mediaHeadStatus": head["status"], "initialMediaGetStatus": fetched["status"]})
                if fetched["status"] == 500:
                    time.sleep(0.2)
                    fetched = self.request(name + "-media-retry", "GET", target, headers={},
                                           note="One identical-URL retry after 200 ms; the first HTTP 500 is retained independently.")
                    observed["retryMediaGetStatus"] = fetched["status"]
                observed["delivery"] = self.follow(name, fetched, ids)
            self.write(name + "-observation", observed)
            AUDIO.private_write(PRIVATE / (name + "-context.json"), json.dumps({"playSessionIds": sorted(ids), "observation": observed}, indent=2), exclusive=True)
            return {"play_id": value.get("PlaySessionId", ""), "ids": ids, "observation": observed}
        finally:
            if cleanup and ids:
                self.cleanup(name, ids)

    def capture(self) -> None:
        AUDIO.preconditions()
        self.use("user")
        incomplete = WIRE / (PREFIX + "flac-get-minimal-info.b64")
        if incomplete.exists() and not (RAW / (PREFIX + "flac-get-minimal-info.json")).exists():
            wire = AUDIO.base64.b64decode(incomplete.read_text(), validate=True)
            self.write("flac-get-minimal-incomplete", {"kind": "incomplete-http-observation",
                       "note": "The first GET response body was persisted before source-path sanitizer auditing failed. Headers and status were not persisted. The request is not repeated.",
                       "wireFile": incomplete.name, "responseBody": json.loads(wire),
                       "observation": {"wireBytes": len(wire), "wireSha256": AUDIO.hashlib.sha256(wire).hexdigest()}})
        else:
            self.playback_info("flac-get-minimal", "flac", method="GET")
        self.playback_info("mp3-get-minimal", "mp3", method="GET")
        self.playback_info("flac-post-minimal", "flac", minimal=True)
        baseline = self.playback_info("flac-http-mp3", "flac", cleanup=False)
        ids = set(baseline["ids"])
        try:
            if baseline["play_id"]:
                reused = self.playback_info("flac-http-mp3-start-reuse", "flac", cleanup=False,
                            request_values={"CurrentPlaySessionId": baseline["play_id"], "AudioStreamIndex": 0, "StartTimeTicks": 20000000})
                ids.update(reused["ids"])
            else:
                self.write("reuse-prerequisite-unavailable", {"kind": "audio-profile-observation", "note": "The core profile did not issue a PlaySessionId; no invented reuse ID was tested."})
        finally:
            self.cleanup("core-and-reuse", ids)
        for name, output in (("flac-protocol-omitted", transcoding(None)), ("flac-protocol-empty", transcoding("")),
                             ("flac-http-aac", transcoding("http", "aac", "aac")),
                             ("flac-http-m4a", transcoding("http", "m4a", "aac"))):
            self.playback_info(name, "flac", device_profile=profile([output]))
        for name, context in (("flac-context-omitted", None), ("flac-context-empty", ""), ("flac-context-static", "Static")):
            output = transcoding()
            if context is None:
                del output["Context"]
            else:
                output["Context"] = context
            self.playback_info(name, "flac", device_profile=profile([output]))
        self.playback_info("mp3-direct-match", "mp3", prefer_direct=True,
                           device_profile=profile(direct=[{"Type": "Audio", "Container": "mp3", "AudioCodec": "mp3"}], bitrate=2000000),
                           request_values={"EnableDirectPlay": True, "EnableDirectStream": True, "EnableTranscoding": True,
                                           "AllowAudioStreamCopy": True, "MaxStreamingBitrate": 2000000})
        self.playback_info("flac-direct-stream-only", "flac", request_values={"EnableDirectPlay": False, "EnableDirectStream": True, "EnableTranscoding": False})
        self.playback_info("flac-all-disabled", "flac", fetch=False,
                           request_values={"EnableDirectPlay": False, "EnableDirectStream": False, "EnableTranscoding": False})
        self.playback_info("flac-max-channels-one", "flac", device_profile=profile([transcoding(channels="1")]))
        self.playback_info("flac-codec-exact", "flac", device_profile=profile(conditions=[
            condition("Equals", "AudioChannels", "1"), condition("Equals", "AudioSampleRate", "44100")]))
        self.playback_info("flac-codec-limits", "flac", device_profile=profile(conditions=[
            condition("LessThanEqual", "AudioChannels", "1"), condition("LessThanEqual", "AudioSampleRate", "48000"),
            condition("LessThanEqual", "AudioBitrate", "96000")]))
        hls_profile = transcoding("hls", "ts", "aac")
        http_profile = transcoding()
        self.playback_info("flac-hls-first", "flac", device_profile=profile([hls_profile, http_profile]))
        self.playback_info("flac-http-first", "flac", device_profile=profile([http_profile, hls_profile]))
        self.write("runtime-after-capture", {"kind": "remote-runtime-observation", **self.runtime()})

    def summarize(self) -> None:
        AUDIO.preconditions()
        self.use("user")
        cases = []
        allowed = {"audiocodec", "audiobitrate", "audiosamplerate", "audiochannels", "maxaudiochannels", "audiostreamindex",
                   "transcodingmaxaudiochannels", "starttimeticks", "static", "container", "segmentlength", "minsegments",
                   "allowaudiostreamcopy", "transcodereasons"}
        for path in sorted(RAW.glob(PREFIX + "*-info.json")):
            raw = json.loads(path.read_text())
            value = raw["response"]["body"]
            source = value.get("MediaSources", [{}])[0]
            case = path.stem.removeprefix(PREFIX).removesuffix("-info")
            urls = {}
            for key in ("TranscodingUrl", "DirectStreamUrl"):
                if not source.get(key):
                    continue
                url = urlsplit(source[key])
                query = parse_qs(url.query, keep_blank_values=True)
                urls[key] = {"endpointLeaf": url.path.rsplit("/", 1)[-1], "hasApiKey": bool(query.get("api_key")),
                             "parameterNames": sorted(query), "audioParameters": {key: item for key, item in query.items() if key.lower() in allowed}}
            cases.append({"case": case, "urls": urls})
        self.write("url-contract-summary", {"kind": "derived-url-contract-observation",
                   "note": "Derived from captured PlaybackInfo responses, retaining original query-key casing. This performs no HTTP requests.", "cases": cases})

    def audit(self) -> None:
        AUDIO.preconditions()
        baseline = json.loads(BASELINE.read_text())
        check(all(AUDIO.digest(Path(path)) == expected for path, expected in baseline["records"].items()), "A preceding reference record changed")
        check(all(AUDIO.digest(Path(source["path"])) == source["sha256"] for source in baseline["sources"]), "An immutable audio source changed")
        total, http_count = 0, 0
        records = sorted(RAW.glob(PREFIX + "*.json"))
        for path in records:
            raw = json.loads(path.read_text())
            exported = json.loads((EXPORT / path.name).read_text())
            self.audit_export(raw, exported)
            check(exported == self.sanitize(raw) and not any(secret in (EXPORT / path.name).read_text() for secret in self.secret_values),
                  "Audio profile export differs from safe deterministic sanitization")
            if raw.get("kind") == "incomplete-http-observation":
                wire = AUDIO.base64.b64decode((WIRE / raw["wireFile"]).read_text(), validate=True)
                check(json.loads(wire) == raw["responseBody"] and len(wire) == raw["observation"]["wireBytes"] and
                      AUDIO.hashlib.sha256(wire).hexdigest() == raw["observation"]["wireSha256"], "Incomplete response body changed")
                total += len(wire)
                continue
            if "request" not in raw:
                continue
            wire = AUDIO.base64.b64decode((WIRE / (path.stem + ".b64")).read_text(), validate=True)
            response = raw["response"]
            headers = {key.lower(): value for key, value in response["headers"]}
            check(exported["response"]["headers"] == self.sanitize(response["headers"]), "Audio profile response headers changed")
            if raw["request"]["method"] != "HEAD" and "content-length" in headers:
                check(int(headers["content-length"]) == len(wire), "Audio profile Content-Length differs from wire bytes")
            if response["bodyType"] == "text":
                check(response["body"].encode() == wire, "Audio profile text bytes changed")
            elif response["bodyType"] == "json":
                check(json.loads(wire) == response["body"], "Audio profile JSON response changed")
            else:
                check(AUDIO.base64.b64decode(response["body"], validate=True) == wire and
                      not any(secret.encode() in wire for secret in self.secret_values), "Audio profile binary bytes changed or contain credentials")
            check(AUDIO.hashlib.sha256(wire).hexdigest() == raw["observation"]["wireSha256"], "Audio profile wire hash changed")
            total += len(wire)
            http_count += 1
        check(total <= AUDIO.MAX_TOTAL, "Audio profile wire evidence exceeded its budget")
        goby_pid = int(AUDIO.subprocess.check_output(["systemctl", "show", "goby-foundation-test.service", "-p", "MainPID", "--value"], timeout=5))
        summary = {"status": "passed", "records": len(records), "httpRecords": http_count, "wireBytes": total,
                   "preservedOldRecordFiles": len(baseline["records"]), "preservedSources": len(baseline["sources"]),
                   "gobyPIDBefore": baseline["gobyPID"], "gobyPIDAfter": goby_pid, "gobyPIDUnchanged": goby_pid == baseline["gobyPID"]}
        AUDIO.private_write(PRIVATE / "audit-summary.json", json.dumps(summary, indent=2))
        print(json.dumps(summary, indent=2, sort_keys=True))

    def finish(self) -> None:
        self.use("user")
        self.request("final-device-cleanup", "DELETE", "/emby/Videos/ActiveEncodings?" + urlencode({"DeviceId": DEVICE}), authenticated=True,
                     note="Targets only the independent audio-profile recorder device.")
        time.sleep(0.5)
        self.write("runtime-final", {"kind": "remote-runtime-observation", **self.runtime()})
        self.request("user-logout", "POST", "/emby/Sessions/Logout", authenticated=True)
        self.audit()
        check(PROBES.resolve(strict=True).parent == PRIVATE and (ROOT / ".goby-managed").read_text().strip() == MARKER,
              "Audio profile probe cleanup ownership does not match")
        for path in PROBES.iterdir():
            check(path.is_file() and not path.is_symlink() and path.name.startswith(PREFIX), "Unexpected audio profile probe input")
        for path in PROBES.iterdir():
            path.unlink()
        PROBES.rmdir()
        print("Removed only owned audio-profile probe inputs; private raw, wire, and credentials remain in marked tmpfs.")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("stage", choices=("setup", "capture", "summarize", "audit", "finish"))
    stage = parser.parse_args().stage
    try:
        getattr(Recorder(), stage)()
    except Exception as error:
        print(json.dumps({"status": "failed", "stage": stage, "errorType": type(error).__name__}))
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
