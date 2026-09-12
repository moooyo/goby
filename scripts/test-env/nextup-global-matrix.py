#!/usr/bin/env python3
"""Pure, bounded NextUp reference request planning and response state machine.

This module performs no HTTP, filesystem, process, fixture, or clock operation.
An owned recorder supplies verified preparation inputs, durable intent receipts,
the actual response bodies, and monotonic elapsed time. It must persist private
state before and after dispatch and independently enforce process/fixture locks.
The accepted wire shapes come from reference-nextup-fresh.py and the retained
full-detail cleanup receipts. This module is not a client acceptance runner.
"""

from __future__ import annotations

from copy import deepcopy
from dataclasses import dataclass
from datetime import datetime
import hashlib
import json
import math
import re
from urllib.parse import urlencode


ACTORS = ("P", "Q")
EPISODES = ("A1", "A2", "A3", "B1", "B2", "B3")
SUMMARIES = ("A", "AS1", "AS2", "B", "BS1", "BS2")
RUNTIME_TICKS = 6_000_000_000
PARTIAL_TICKS = 1_200_000_000
MAX_REQUESTS = 300
CLEANUP_RESERVE = 80
NORMAL_LIMIT = MAX_REQUESTS - CLEANUP_RESERVE
PROJECTION = "UserData,ParentId"
LIFECYCLES = (("R1", "P", "A1"), ("R2", "P", "B1"), ("R3", "P", "A2"),
              ("R4-B1", "Q", "B1"), ("R4-A1", "Q", "A1"),
              ("R5", "P", "A2"), ("R6", "P", "B1"))
CHANGED_PAIRS = (("P", "A1"), ("P", "A2"), ("P", "B1"), ("Q", "A1"), ("Q", "B1"))


class MatrixError(ValueError):
    """A frozen manifest, request, or observed response failed its guard."""


def require(condition, message):
    if not condition:
        raise MatrixError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True)


def digest(value):
    return hashlib.sha256(canonical(value).encode("utf-8")).hexdigest()


def require_sha(value, label):
    require(isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None,
            label + " must be a lowercase SHA-256 digest.")


def require_id(value, label):
    require(isinstance(value, str) and re.fullmatch(r"[A-Za-z0-9_-]+", value) is not None,
            label + " must be one nonempty public identifier, not a path or URL.")


def integer(value):
    return type(value) is int


def parse_date(value):
    require(isinstance(value, str) and value, "A persisted playback date is required.")
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise MatrixError("The persisted playback date is not an ISO timestamp.") from error
    require(parsed.utcoffset() is not None, "The persisted date must identify a timezone.")
    return parsed


def userdata_fact(body):
    """Retain every field and distinguish absent, null, boolean, and number."""
    require(isinstance(body, dict) and isinstance(body.get("UserData"), dict),
            "A full item detail with a UserData object is required.")
    value = deepcopy(body["UserData"])
    require(type(value.get("Played")) is bool and integer(value.get("PlayCount")) and
            value["PlayCount"] >= 0 and integer(value.get("PlaybackPositionTicks")) and
            0 <= value["PlaybackPositionTicks"] <= RUNTIME_TICKS,
            "The complete UserData played/count/position primitives are unavailable.")
    return {"value": value, "fields": sorted(value),
            "types": {key: type(item).__name__ for key, item in value.items()}}


def zero_state(fact):
    value = fact["value"]
    return (value["Played"] is False and value["PlayCount"] == 0 and
            value["PlaybackPositionTicks"] == 0 and
            ("LastPlayedDate" not in value or value["LastPlayedDate"] is None))


def validate_manifest(value):
    """Validate a private, preparation-owned mapping; never discover IDs here."""
    require(isinstance(value, dict), "The preparation manifest must be an object.")
    manifest = deepcopy(value)
    require(manifest.get("schemaVersion") == 1 and manifest.get("target") in ("reference", "goby"),
            "A versioned reference or Goby target binding is required.")
    require_id(manifest.get("runId"), "runId")
    for key in ("preparationReceiptSha256", "coordinationReceiptSha256", "catalogReceiptSha256"):
        require_sha(manifest.get(key), key)
    binding = manifest.get("binding", {})
    require(isinstance(binding, dict) and all(binding.get(key) for key in
            ("processIdentity", "version", "endpoint", "isolationIdentity", "evidenceRoot")),
            "Current process, endpoint, isolation, and fresh evidence-root bindings are required.")
    require(binding.get("fixtureReleased") is True and binding.get("freshEvidenceRoot") is True,
            "Preparation must attest fixture release and a fresh, unused evidence root.")
    if manifest["target"] == "goby":
        for key in ("sourceManifestSha256", "executableSha256", "fixtureObservationSha256"):
            require_sha(binding.get(key), key)
        require(integer(binding.get("schemaVersion")) and binding["schemaVersion"] > 0,
                "Goby requires its current schema binding.")
    require_id(manifest.get("serverId"), "serverId")
    cleanup = manifest.get("cleanupContract", {})
    require(isinstance(cleanup, dict) and cleanup.get("mode") == "episode-delete-played-items" and
            cleanup.get("zeroBaselineRequired") is True and
            cleanup.get("verifiedForBoundTarget") is True,
            "Preparation must bind the verified episode DELETE cleanup contract for this target.")
    require_sha(cleanup.get("receiptSha256"), "cleanupContract.receiptSha256")
    require(manifest.get("lifecycleSeparationSeconds") in (2, 3, 4, 5),
            "Choose a bounded real-time lifecycle separation of two to five seconds.")
    actors = manifest.get("actors", {})
    require(isinstance(actors, dict) and set(actors) == set(ACTORS), "Exactly P and Q are required.")
    for actor in ACTORS:
        row = actors[actor]
        require(isinstance(row, dict), "Each ordinary actor binding must be an object.")
        for key in ("userId", "deviceId", "credentialRef"):
            require_id(row.get(key), actor + "." + key)
        require(isinstance(row.get("username"), str) and row["username"] and
                row.get("newOwnedOrdinaryAccount") is True,
                "Each actor must be a newly owned ordinary account with a public username.")
        require_sha(row.get("policyReceiptSha256"), actor + ".policyReceiptSha256")
    for key in ("userId", "deviceId", "credentialRef", "username"):
        require(actors["P"][key] != actors["Q"][key], "P and Q must have distinct " + key + " values.")
    libraries = manifest.get("libraries", {})
    require(isinstance(libraries, dict) and set(libraries) == {"LA", "LB"},
            "Exactly the two owned TV libraries LA and LB are required.")
    for symbol, library in libraries.items():
        for key in ("libraryId", "viewId", "sourceRootId", "scopeItemId", "policyFolderId"):
            require_id(library.get(key), symbol + "." + key)
        require(library.get("scopeItemId") == library.get("viewId") and
                library.get("owned") is True and library.get("collectionType") == "tvshows",
                "Parent scope must be the verified view item ID of an owned TV library.")
        require_sha(library.get("mediaRootReceiptSha256"), symbol + ".mediaRootReceiptSha256")
    require(all(libraries["LA"][key] != libraries["LB"][key]
                for key in ("libraryId", "viewId", "sourceRootId", "policyFolderId")),
            "The two series must belong to separate TV libraries and roots.")
    for actor in ACTORS:
        require(isinstance(actors[actor].get("allowedFolderIds"), list) and
                len(actors[actor]["allowedFolderIds"]) == 2 and
                set(actors[actor]["allowedFolderIds"]) ==
                {libraries[name]["policyFolderId"] for name in ("LA", "LB")},
                "Both ordinary actors must initially have exactly the two owned library grants.")
    catalog = manifest.get("items", {})
    require(isinstance(catalog, dict) and set(catalog) == set(EPISODES + SUMMARIES),
            "The catalog must contain exactly two series, four seasons, and six episodes.")
    for symbol, row in catalog.items():
        require(isinstance(row, dict), "Each semantic item mapping must be an object.")
        require_id(row.get("id"), symbol)
        require(row.get("library") == "L" + symbol[0], "An item crossed its mapped library boundary.")
    require(len({row["id"] for row in catalog.values()}) == len(catalog),
            "Semantic item IDs must be unique; numeric ordering is not a mapping.")
    for series in ("A", "B"):
        row = catalog[series]
        require(row.get("type") == "Series" and isinstance(row.get("name"), str) and row["name"] and
                row.get("parentId") == libraries["L" + series]["sourceRootId"],
                "Series ownership and parent identity must be established by preparation.")
        for season in (1, 2):
            row = catalog[series + "S" + str(season)]
            require(row.get("type") == "Season" and row.get("parentId") == catalog[series]["id"] and
                    row.get("seriesId") == catalog[series]["id"] and row.get("indexNumber") == season,
                    "The season identity or relation differs from the semantic map.")
        for index, (season, episode) in enumerate(((1, 1), (1, 2), (2, 1)), 1):
            row = catalog[series + str(index)]
            require(row.get("type") == "Episode" and row.get("seriesId") == catalog[series]["id"] and
                    row.get("parentId") == catalog[series + "S" + str(season)]["id"] and
                    row.get("parentIndexNumber") == season and row.get("indexNumber") == episode and
                    row.get("runtimeTicks") == RUNTIME_TICKS and row.get("frameRate") == 30,
                    "The six episodes must map to S01E01, S01E02, S02E01 at 600 seconds and 30 fps.")
            require_sha(row.get("mediaSha256"), series + str(index) + ".mediaSha256")
    require(catalog["A"]["name"] != catalog["B"]["name"], "The two series need distinct stable names.")
    return manifest


@dataclass(frozen=True)
class Step:
    label: str
    stage: str
    actor: str
    kind: str
    item: str = ""
    lifecycle: str = ""
    position: int = 0
    variant: str = ""
    expected_ids: tuple | None = None
    cleanup: bool = False


@dataclass(frozen=True)
class Request:
    label: str
    actor: str
    method: str
    route: str
    body_json: str | None
    login: bool
    cleanup: bool

    @property
    def body(self):
        return json.loads(self.body_json) if self.body_json is not None else None

    def recorder_arguments(self):
        """Use with the actor's existing request(label, method, route, body, login=...)."""
        return (self.label, self.method, self.route, self.body), {"login": self.login}

    def fact(self):
        return {"label": self.label, "actor": self.actor, "method": self.method,
                "route": self.route, "body": self.body, "login": self.login, "cleanup": self.cleanup}


@dataclass(frozen=True)
class WaitRequired:
    seconds: float
    reason: str = "Bounded real time must separate independent playback lifecycles."


def detail(stage, actor, item, kind="detail"):
    return Step(stage + "-" + actor + "-" + kind + "-" + item, stage, actor, kind, item=item)


def query(stage, actor, variant, expected=None):
    return Step(stage + "-" + actor + "-nextup-" + variant, stage, actor, "nextup",
                variant=variant, expected_ids=None if expected is None else tuple(expected))


def core(stage, actor, a, b):
    return ([detail(stage, actor, item) for item in EPISODES] +
            [query(stage, actor, "global"), query(stage, actor, "series-A", a),
             query(stage, actor, "series-B", b)])


def lifecycle(stage, actor, item, position=RUNTIME_TICKS):
    return [Step(stage + "-" + actor + "-" + kind + "-" + item, stage, actor, kind,
                 item=item, lifecycle=stage, position=position)
            for kind in ("before-play", "playback-info", "started", "progress", "stopped", "after-play")]


def initial_steps():
    rows = []
    for actor in ACTORS:
        rows += [Step("PRE-" + actor + "-login", "PRE", actor, "login"),
                 Step("PRE-" + actor + "-profile", "PRE", actor, "profile"),
                 Step("PRE-" + actor + "-preferences", "PRE", actor, "preferences")]
        rows += [detail("PRE", actor, item, "summary") for item in SUMMARIES]
    rows += core("R0", "P", (), ()) + core("R0", "Q", (), ())
    rows += lifecycle("R1", "P", "A1") + core("R1", "P", ("A2", "A3"), ())
    rows += [query("R1", "Q", "global"), detail("R1", "Q", "A1")]
    rows += lifecycle("R2", "P", "B1") + core("R2", "P", ("A2", "A3"), ("B2", "B3"))
    rows += [query("R2", "Q", "global"), detail("R2", "Q", "B1")]
    rows += lifecycle("R3", "P", "A2") + core("R3", "P", ("A3",), ("B2", "B3"))
    rows += [query("R3", "Q", "global")]
    rows += lifecycle("R4-B1", "Q", "B1") + lifecycle("R4-A1", "Q", "A1")
    rows += core("R4", "Q", ("A2", "A3"), ("B2", "B3"))
    rows += core("R4", "P", ("A3",), ("B2", "B3"))
    return rows


EXTENSION_VARIANTS = ("limit-omitted", "limit-zero", "limit-one", "page-one", "beyond-count",
                      "parent-LA", "parent-LB", "parent-A", "parent-AS1",
                      "combined-A-LA", "combined-A-AS2", "userdata-disabled", "fields-projection")


def extension_steps(actor):
    return ([query("EXT", actor, variant) for variant in EXTENSION_VARIANTS] +
            [query("EXT", "Q" if actor == "P" else "P", "global")])


def positive_steps():
    rows = [detail("R5-RESET", "P", item) for item in EPISODES]
    rows += [Step("R5-RESET-P-delete-" + item, "R5-RESET", "P", "reset", item=item)
             for item in ("A1", "A2", "B1")]
    rows += [detail("R5-ZERO", "P", item, "reset-proof") for item in EPISODES]
    rows += [detail("R5-ZERO", "P", item, "summary-proof") for item in SUMMARIES]
    rows += lifecycle("R5", "P", "A2", PARTIAL_TICKS) + core("R5", "P", ("A2", "A3"), ())
    rows += [query("R5", "Q", "global"), detail("R5", "Q", "A1"), detail("R5", "Q", "B1")]
    rows += lifecycle("R6", "P", "B1", PARTIAL_TICKS)
    rows += core("R6", "P", ("A2", "A3"), ("B1", "B2", "B3"))
    rows += core("R6", "Q", ("A2", "A3"), ("B2", "B3"))
    return rows


def cleanup_templates():
    """Freeze every allowed cleanup route before the first playback dispatch."""
    rows = [Step("CLEAN-" + stage + "-stop", "CLEAN", actor, "cleanup-stop", item=item,
                 lifecycle=stage, cleanup=True) for stage, actor, item in LIFECYCLES]
    rows += [Step("CLEAN-" + actor + "-reconcile-" + item, "CLEAN", actor,
                  "cleanup-reconcile", item=item, cleanup=True) for actor, item in CHANGED_PAIRS]
    rows += [Step("CLEAN-" + actor + "-delete-" + item, "CLEAN", actor,
                  "cleanup-reset", item=item, cleanup=True) for actor, item in CHANGED_PAIRS]
    for actor in ACTORS:
        rows += [Step("CLEAN-" + actor + "-zero-" + item, "CLEAN", actor,
                      "cleanup-proof", item=item, cleanup=True) for item in EPISODES]
        rows += [Step("CLEAN-" + actor + "-summary-" + item, "CLEAN", actor,
                      "cleanup-summary", item=item, cleanup=True) for item in SUMMARIES]
        rows += [Step("CLEAN-" + actor + "-profile", "CLEAN", actor, "profile", cleanup=True),
                 Step("CLEAN-" + actor + "-preferences", "CLEAN", actor, "preferences", cleanup=True),
                 Step("CLEAN-" + actor + "-logout", "CLEAN", actor, "logout", cleanup=True),
                 Step("CLEAN-" + actor + "-token-invalid", "CLEAN", actor, "token-invalid", cleanup=True)]
    return rows


class Matrix:
    """A single-use response consumer with no transport or automatic recovery."""

    def __init__(self, manifest):
        self.manifest = validate_manifest(manifest)
        self.queue = initial_steps()
        self.count = 0
        self.normal_count = 0
        self.pending = None
        self.mode = "api"
        self.outcome = None
        self.failure = None
        self.completed = []
        self.facts = []
        self.baseline = {actor: {} for actor in ACTORS}
        self.current = {actor: {} for actor in ACTORS}
        self.summaries = {actor: {} for actor in ACTORS}
        self.profiles = {}
        self.preferences = {}
        self.sessions = {}
        self.plays = {}
        self.touched = set()
        self.restored_pairs = set()
        self.reset_proofs = set()
        self.cleanup_proofs = set()
        self.revoked = set()
        self.positive = None
        self.extension_complete = False
        self.r0_r4_complete = False
        self.r5_r6_started = False
        self.ranking = []
        self.activity_dates = {}
        self.last_stop_elapsed = None
        self.last_elapsed = 0.0
        self.cleanup_reconciled = set()
        self.cleanup_started = False
        self.client_discovery = None
        self.frozen_plan = self.plan()
        self.plan_sha256 = digest(self.frozen_plan)
        self._manifest_sha256 = digest(self.manifest)
        self._seal_queue()

    def plan(self):
        branches = {"R0-R4": initial_steps(), "first-positive-extension": extension_steps("P"),
                    "R5-R6-positive-only": positive_steps(), "cleanup-maximum": cleanup_templates()}
        counts = {name: len(rows) for name, rows in branches.items()}
        normal = counts["R0-R4"] + counts["first-positive-extension"] + counts["R5-R6-positive-only"]
        require(normal <= NORMAL_LIMIT and counts["cleanup-maximum"] <= CLEANUP_RESERVE,
                "The frozen matrix would consume the cleanup reserve.")
        return {"schemaVersion": 1, "classification": "planned public protocol matrix; not executed",
                "manifestSha256": digest(self.manifest), "maximumRequests": MAX_REQUESTS,
                "normalLimit": NORMAL_LIMIT, "cleanupReserve": CLEANUP_RESERVE,
                "requestCounts": counts, "maximumNormalRequests": normal,
                "maximumPlannedRequests": normal + counts["cleanup-maximum"],
                "extensionActor": "The first positive global response actor; P or Q only.",
                "beyondOffset": "max(first positive TotalRecordCount, returned item count) + 1",
                "branches": {name: [self._planned_request(step) for step in rows] for name, rows in branches.items()},
                "firstPositiveExtensionForQ": [self._planned_request(step) for step in extension_steps("Q")],
                "undefinedFlags": ["EnableResumable", "EnableRewatching"],
                "globalSelectionAndOrdering": "observed facts only; no prefilled expected IDs"}

    @staticmethod
    def _template(step):
        return dict(step.__dict__)

    def _planned_request(self, step):
        return {**self._template(step), "wire": self._resolve(step, planning=True).fact()}

    def _seal_queue(self):
        self._queue_sha256 = digest([self._template(step) for step in self.queue])

    def _check_frozen_inputs(self):
        require(digest(self.manifest) == self._manifest_sha256 and digest(self.frozen_plan) == self.plan_sha256,
                "The frozen manifest or route plan changed after construction.")
        require(digest([self._template(step) for step in self.queue]) == self._queue_sha256,
                "The frozen request sequence changed outside its response-driven branch transition.")

    def _verify_elapsed(self, elapsed):
        require(isinstance(elapsed, (int, float)) and not isinstance(elapsed, bool) and
                math.isfinite(elapsed) and elapsed >= self.last_elapsed,
                "Recorder monotonic elapsed time must be finite and cannot move backwards.")
        self.last_elapsed = elapsed

    def _advance_branch(self):
        if self.queue or self.mode != "api":
            return
        if not self.r0_r4_complete:
            self.r0_r4_complete = True
            if self.positive is None:
                self.mode = "client-discovery-required"
                self.outcome = "reference_global_positive_unresolved"
                return
            require(self.extension_complete, "The frozen positive-state extension did not finish.")
            self.r5_r6_started = True
            self.queue = positive_steps()
        else:
            self.mode = "api-observed"
            self.outcome = "reference_api_matrix_observed_client_acceptance_pending"

    def prepare_next(self, elapsed_seconds):
        self._check_frozen_inputs()
        self._verify_elapsed(elapsed_seconds)
        require(self.pending is None, "An attempted request must be reconciled; no blind replay is allowed.")
        require(self.failure is None or self.mode == "cleanup", "Normal work is stopped after a failed observation.")
        self._advance_branch()
        self._seal_queue()
        if not self.queue:
            return None
        step = self.queue[0]
        require(self.count < MAX_REQUESTS and (step.cleanup or self.count < NORMAL_LIMIT),
                "Normal work cannot spend the eighty-request cleanup reserve.")
        if step.kind == "playback-info" and self.last_stop_elapsed is not None:
            delay = self.last_stop_elapsed + self.manifest["lifecycleSeparationSeconds"] - elapsed_seconds
            if delay > 0:
                return WaitRequired(delay)
        return self._resolve(step)

    def _nextup_parameters(self, step, planning=False):
        user = self.manifest["actors"][step.actor]["userId"]
        params = {"UserId": user, "Limit": "10", "Fields": PROJECTION, "EnableImages": "false"}
        variant = step.variant
        if variant == "global":
            pass
        elif variant.startswith("series-"):
            params["SeriesId"] = self.manifest["items"][variant[-1]]["id"]
        else:
            require((planning or self.positive is not None) and step.stage == "EXT",
                    "Pagination, ParentId, and projection extensions need a captured positive global result.")
            if variant == "limit-omitted":
                del params["Limit"]
            elif variant in ("limit-zero", "limit-one"):
                params["Limit"] = "0" if variant == "limit-zero" else "1"
            elif variant in ("page-one", "beyond-count"):
                offset = "{firstPositiveBeyondOffset}" if planning else str(self.positive["beyondOffset"])
                params.update(Limit="1", StartIndex="1" if variant == "page-one" else offset)
            elif variant.startswith("parent-"):
                symbol = variant.removeprefix("parent-")
                params["ParentId"] = (self.manifest["libraries"][symbol]["scopeItemId"] if symbol.startswith("L")
                                      else self.manifest["items"][symbol]["id"])
            elif variant.startswith("combined-"):
                params["SeriesId"] = self.manifest["items"]["A"]["id"]
                params["ParentId"] = (self.manifest["libraries"]["LA"]["scopeItemId"] if variant.endswith("LA")
                                      else self.manifest["items"]["AS2"]["id"])
            elif variant == "userdata-disabled":
                params["EnableUserData"] = "false"
            elif variant == "fields-projection":
                params["Fields"] = "ParentId"
            else:
                raise MatrixError("Unknown NextUp variant.")
        return params

    def _resolve(self, step, planning=False):
        user = self.manifest["actors"][step.actor]["userId"]
        prefix = "/emby/Users/" + user
        item = self.manifest["items"].get(step.item, {}).get("id")
        body = None
        method = "GET"
        if step.kind == "login":
            route, method = "/emby/Users/AuthenticateByName", "POST"
        elif step.kind == "profile":
            route = prefix
        elif step.kind == "preferences":
            route = "/emby/usersettings/" + user
        elif step.kind == "nextup":
            route = "/emby/Shows/NextUp?" + urlencode(self._nextup_parameters(step, planning=planning))
        elif step.kind in ("reset", "cleanup-reset"):
            require(planning or (step.actor, step.item) in self.touched, "Only an episode changed by this run may be reset.")
            require(planning or all(play["stopped"] for play in self.plays.values()),
                    "Episode restoration requires every known owned playback session to be stopped.")
            if step.cleanup and not planning:
                require((step.actor, step.item) in self.cleanup_reconciled,
                        "Cleanup needs a fresh full-detail reconciliation before its DELETE.")
            route, method = prefix + "/PlayedItems/" + item, "DELETE"
        elif step.kind == "playback-info":
            require(planning or all(len(self.baseline[actor]) == 6 for actor in ACTORS),
                    "Playback needs all twelve full-detail zero baselines.")
            require(planning or step.label.replace("playback-info", "before-play") in self.completed,
                    "Playback needs its completed full-detail precondition.")
            require(planning or step.stage not in ("R5", "R6") or
                    (self.positive is not None and self.extension_complete and self.r0_r4_complete and
                     self.reset_proofs == set(EPISODES + SUMMARIES)),
                    "R5 and R6 need the positive gate and a full P zero-state restoration proof.")
            route, method = "/emby/Items/" + item + "/PlaybackInfo", "POST"
            body = {"UserId": user, "IsPlayback": True}
        elif step.kind in ("started", "progress", "stopped", "cleanup-stop"):
            play = ({"actor": step.actor, "item": step.item,
                     "lastReportedPosition": "{" + step.lifecycle + ".lastAcknowledgedPositionTicks}"
                                             if step.kind == "cleanup-stop" else step.position,
                     "context": {"ItemId": item, "MediaSourceId": "{" + step.lifecycle + ".MediaSourceId}",
                                 "PlaySessionId": "{" + step.lifecycle + ".PlaySessionId}",
                                 "SessionId": "{" + step.actor + ".SessionId}"}}
                    if planning else self.plays.get(step.lifecycle))
            require(play is not None and play["actor"] == step.actor and play["item"] == step.item,
                    "Playback reports require the exact acknowledged owned negotiation context.")
            context = deepcopy(play["context"])
            method = "POST"
            if step.kind in ("stopped", "cleanup-stop"):
                route = "/emby/Sessions/Playing/Stopped"
                body = {**context, "PositionTicks": play["lastReportedPosition"], "Failed": False, "IsAutomated": False}
            else:
                route = "/emby/Sessions/Playing" + ("/Progress" if step.kind == "progress" else "")
                body = {**context, "RunTimeTicks": RUNTIME_TICKS, "PositionTicks": 0,
                        "CanSeek": True, "IsPaused": False, "IsMuted": False,
                        "PlayMethod": "DirectStream", "PlaybackRate": 1}
                if step.kind == "progress":
                    body.update(PositionTicks=step.position, EventName="TimeUpdate")
        elif step.kind == "logout":
            route, method = "/emby/Sessions/Logout", "POST"
        elif step.kind == "token-invalid":
            route = "/emby/Sessions"
        else:
            require(step.kind in ("detail", "before-play", "after-play", "summary", "summary-proof",
                                  "reset-proof", "cleanup-reconcile", "cleanup-proof", "cleanup-summary"),
                    "Unknown detail request kind.")
            route = prefix + "/Items/" + item
        return Request(step.label, step.actor, method, route,
                       canonical(body) if body is not None else None, step.kind == "login", step.cleanup)

    def authorize(self, request, intent_receipt_sha256, elapsed_seconds, *, actor_token_sha256=None):
        """Reserve an attempt only after the recorder durably journals its exact intent."""
        require_sha(intent_receipt_sha256, "intentReceiptSha256")
        expected = self.prepare_next(elapsed_seconds)
        require(isinstance(expected, Request) and request == expected,
                "Only the exact next frozen request may be dispatched.")
        require(request.actor in self.sessions or request.login, "The request actor lacks a verified owned login.")
        if not request.login:
            require(actor_token_sha256 == self.sessions[request.actor]["tokenSha256"],
                    "The actual request token must match this actor's one acknowledged recorder login.")
        self.count += 1
        self.normal_count += int(not request.cleanup)
        self.pending = {"step": self.queue[0], "request": request,
                "intentReceiptSha256": intent_receipt_sha256, "ordinal": self.count,
                "actorTokenSha256": actor_token_sha256}
        if self.queue[0].kind == "playback-info":
            self.touched.add((self.queue[0].actor, self.queue[0].item))
        return {"ordinal": self.count, "request": request.fact(), "intentReceiptSha256": intent_receipt_sha256}

    def accept(self, status, body, response_timestamp, response_receipt_sha256, elapsed_seconds):
        """Consume actual decoded HTTP facts; requested positions never establish state."""
        self._check_frozen_inputs()
        require(self.pending is not None, "No authorized request is awaiting a response.")
        self._verify_elapsed(elapsed_seconds)
        require_sha(response_receipt_sha256, "responseReceiptSha256")
        parse_date(response_timestamp)
        pending = self.pending
        step, request = pending["step"], pending["request"]
        fact = {"ordinal": pending["ordinal"], "label": step.label, "actor": step.actor,
                "stage": step.stage, "kind": step.kind, "request": request.fact(),
                "status": status, "responseTimestamp": response_timestamp,
                "responseReceiptSha256": response_receipt_sha256, "bodyType": type(body).__name__}
        self.facts.append(fact)
        self.pending = None
        try:
            self._accept_step(step, status, body, fact, elapsed_seconds)
        except (MatrixError, TypeError, KeyError, IndexError) as error:
            self.failure = {"label": step.label, "reason": str(error), "responseRetained": True}
            self.mode = "recovery-required"
            self.outcome = "recovery_required"
            raise MatrixError("The response failed its guard; preserve evidence and reconcile cleanup.") from error
        self.completed.append(step.label)
        self.queue.pop(0)
        if step.stage == "EXT" and not any(row.stage == "EXT" for row in self.queue):
            self.extension_complete = True
        if self.positive is not None and not self.positive["inserted"]:
            self.positive["inserted"] = True
            self.queue[0:0] = extension_steps(self.positive["actor"])
        self._advance_branch()
        if self.mode == "cleanup" and not self.queue:
            require(self.revoked == set(self.sessions), "Exact owned token rejection proofs are incomplete.")
            self.mode = "closed" if self.failure is None else "closed-with-observation-failure"
        self._seal_queue()

    def _accept_step(self, step, status, body, fact, elapsed):
        if step.kind == "login":
            actor = self.manifest["actors"][step.actor]
            require(status == 200 and isinstance(body, dict) and isinstance(body.get("AccessToken"), str) and
                    body["AccessToken"] and body.get("ServerId") == self.manifest["serverId"],
                    "Login did not acknowledge the bound target and an owned token.")
            user, session = body.get("User", {}), body.get("SessionInfo", {})
            require(user.get("Id") == actor["userId"] and user.get("Name") == actor["username"] and
                    user.get("Policy", {}).get("IsAdministrator") is False and
                    session.get("UserId") == actor["userId"] and session.get("DeviceId") == actor["deviceId"],
                    "Login identity, ordinary policy, or recorder device differs.")
            require_id(session.get("Id"), "SessionInfo.Id")
            token_sha = hashlib.sha256(body["AccessToken"].encode()).hexdigest()
            require(all(known["id"] != session["Id"] and known["tokenSha256"] != token_sha
                        for known in self.sessions.values()),
                    "P and Q must own distinct acknowledged sessions and tokens.")
            self.sessions[step.actor] = {"id": session["Id"], "tokenSha256": token_sha}
            fact["identityVerified"] = True
            return
        if step.kind == "logout":
            require(status in (204, 401), "Exact owned recorder logout was not acknowledged.")
            return
        if step.kind == "token-invalid":
            require(status == 401, "The same owned recorder token was not rejected.")
            self.revoked.add(step.actor)
            return
        if step.kind in ("started", "progress", "stopped", "cleanup-stop"):
            require(status == 204, "The owned playback report was not acknowledged.")
            play = self.plays[step.lifecycle]
            if step.kind == "progress":
                play["lastReportedPosition"] = step.position
            elif step.kind in ("stopped", "cleanup-stop"):
                play["stopped"] = True
                play["stateConfirmed"] = False
                self.last_stop_elapsed = elapsed
            return
        if step.kind in ("reset", "cleanup-reset"):
            require(status == 200, "The observed episode DELETE cleanup route was not acknowledged.")
            self.restored_pairs.add((step.actor, step.item))
            return
        require(status == 200, "The required public observation did not return HTTP 200.")
        if step.kind == "playback-info":
            require(isinstance(body, dict) and isinstance(body.get("PlaySessionId"), str) and
                    body["PlaySessionId"] and isinstance(body.get("MediaSources"), list) and len(body["MediaSources"]) == 1,
                    "Exactly one owned media source and an acknowledged play session are required.")
            source = body["MediaSources"][0]
            require(isinstance(source, dict) and isinstance(source.get("Id"), str) and source["Id"] and
                    source.get("RunTimeTicks") == RUNTIME_TICKS,
                    "The negotiated source differs from the authoritative 600-second fixture.")
            require(all(play["context"]["PlaySessionId"] != body["PlaySessionId"] for play in self.plays.values()),
                    "A new lifecycle unexpectedly reused a known play session identifier.")
            self.plays[step.lifecycle] = {"actor": step.actor, "item": step.item, "stopped": False,
                "stateConfirmed": False,
                "lastReportedPosition": 0, "context": {"ItemId": self.manifest["items"][step.item]["id"],
                    "MediaSourceId": source["Id"], "PlaySessionId": body["PlaySessionId"],
                    "SessionId": self.sessions[step.actor]["id"]}}
            fact["negotiatedRuntimeTicks"] = source["RunTimeTicks"]
            return
        if step.kind in ("profile", "preferences"):
            require(isinstance(body, dict), "The complete profile/preferences response is unavailable.")
            value = deepcopy(body)
            if step.kind == "profile":
                require(body.get("Id") == self.manifest["actors"][step.actor]["userId"] and
                        isinstance(body.get("Policy"), dict) and body["Policy"].get("IsAdministrator") is False and
                        isinstance(body.get("Configuration"), dict), "The ordinary profile binding differs.")
                require(body["Policy"].get("EnableAllFolders") is False and
                        isinstance(body["Policy"].get("EnabledFolders"), list) and
                        set(body["Policy"]["EnabledFolders"]) ==
                        set(self.manifest["actors"][step.actor]["allowedFolderIds"]),
                        "The current ordinary account policy must grant exactly the two prepared folders.")
                value = {"Policy": deepcopy(body["Policy"]), "Configuration": deepcopy(body["Configuration"])}
            storage = self.profiles if step.kind == "profile" else self.preferences
            if step.cleanup:
                require(canonical(storage.get(step.actor)) == canonical(value),
                        "The complete owned profile/preferences drifted, including JSON primitive types.")
            else:
                storage[step.actor] = value
            fact["value"] = value
            return
        if step.kind == "nextup":
            self._accept_nextup(step, body, fact)
            return
        self._accept_detail(step, body, fact)

    def _accept_nextup(self, step, body, fact):
        require(isinstance(body, dict) and isinstance(body.get("Items"), list) and
                integer(body.get("TotalRecordCount")) and body["TotalRecordCount"] >= 0,
                "NextUp must retain an Items array and a nonnegative integer count.")
        by_id = {row["id"]: name for name, row in self.manifest["items"].items() if name in EPISODES}
        items = body["Items"]
        require(all(isinstance(row, dict) and row.get("Id") in by_id and row.get("Type") == "Episode" for row in items),
                "NextUp returned a foreign or non-episode item.")
        ids = [by_id[row["Id"]] for row in items]
        if step.stage != "EXT":
            require(all(isinstance(row.get("UserData"), dict) for row in items),
                    "The explicitly requested core UserData object is unavailable.")
        fact.update(variant=step.variant, orderedIds=[row["Id"] for row in items], orderedSymbols=ids,
                    total=body["TotalRecordCount"], body=deepcopy(body))
        if step.expected_ids is not None:
            require(ids == list(step.expected_ids), "The independently captured SeriesId control differs.")
        if step.variant == "global" and step.stage != "EXT" and self.positive is None and items:
            self.positive = {"actor": step.actor, "label": step.label, "stage": step.stage,
                "orderedSymbols": ids, "total": body["TotalRecordCount"],
                "beyondOffset": max(body["TotalRecordCount"], len(items)) + 1,
                "responseReceiptSha256": fact["responseReceiptSha256"], "inserted": False}

    def _accept_detail(self, step, body, fact):
        mapped = self.manifest["items"][step.item]
        require(isinstance(body, dict) and body.get("Id") == mapped["id"] and body.get("Type") == mapped["type"],
                "The full item detail did not identify its mapped item.")
        if step.item not in EPISODES:
            require(isinstance(body.get("UserData"), dict), "A complete series/season UserData object is required.")
            value = deepcopy(body["UserData"])
            fact.update(userData=value, fields=sorted(value))
            if step.kind == "summary":
                self.summaries[step.actor][step.item] = value
            else:
                require(canonical(self.summaries[step.actor].get(step.item)) == canonical(value),
                        "The complete series/season summary baseline was not restored.")
                if step.kind == "summary-proof":
                    self.reset_proofs.add(step.item)
            return
        require(body.get("ParentId") == mapped["parentId"] and body.get("SeriesId") == mapped["seriesId"] and
                body.get("ParentIndexNumber") == mapped["parentIndexNumber"] and
                body.get("IndexNumber") == mapped["indexNumber"] and body.get("RunTimeTicks") == RUNTIME_TICKS,
                "The full episode identity, numbering, relations, or runtime differs.")
        observed = userdata_fact(body)
        fact["userData"] = deepcopy(observed)
        previous = self.current[step.actor].get(step.item)
        baseline = self.baseline[step.actor].get(step.item)
        pair = (step.actor, step.item)
        if step.stage == "R0":
            require(zero_state(observed), "Every initial full detail must prove zero playback history.")
            self.baseline[step.actor][step.item] = deepcopy(observed)
        elif step.kind in ("reset-proof", "cleanup-proof"):
            require(baseline is not None and canonical(observed) == canonical(baseline) and zero_state(observed),
                    "Full UserData including field presence did not match the measured zero baseline.")
            if step.kind == "reset-proof":
                self.reset_proofs.add(step.item)
            else:
                self.cleanup_proofs.add(pair)
        elif step.kind == "after-play":
            value = observed["value"]
            require(self.plays[step.lifecycle]["stopped"], "State proof cannot precede its acknowledged stop.")
            require(value["PlayCount"] > 0, "Reported playback did not persist a positive count.")
            observed_date = parse_date(value.get("LastPlayedDate"))
            if step.position == RUNTIME_TICKS:
                require(value["Played"] is True, "Completion was not established by the full detail.")
            else:
                require(value["Played"] is False and value["PlaybackPositionTicks"] == PARTIAL_TICKS,
                        "The exact partial state was not established by the full detail.")
            earlier = self.activity_dates.get(step.actor)
            if earlier is not None:
                self.ranking.append({"actor": step.actor, "earlierLifecycle": earlier["lifecycle"],
                    "laterLifecycle": step.lifecycle, "earlierDate": earlier["date"],
                    "laterDate": value["LastPlayedDate"],
                    "status": "distinct-persisted-order" if observed_date > parse_date(earlier["date"])
                              else "inconclusive-persisted-dates-not-increasing"})
            self.activity_dates[step.actor] = {"lifecycle": step.lifecycle, "date": value["LastPlayedDate"]}
            self.restored_pairs.discard(pair)
            self.plays[step.lifecycle]["stateConfirmed"] = True
            fact["requestedPositionTicks"] = step.position
            fact["durableStateEstablished"] = True
        elif step.kind == "cleanup-reconcile":
            require(previous is not None and baseline is not None,
                    "Cleanup needs the complete retained episode baseline and prior observation.")
            stopped = [play for play in self.plays.values() if play["actor"] == step.actor and
                       play["item"] == step.item and play["stopped"] and not play["stateConfirmed"]]
            if canonical(observed) != canonical(previous):
                require(stopped, "A changed cleanup observation lacks an unreconciled acknowledged owned stop.")
                playback_fields = {"Played", "PlayCount", "PlaybackPositionTicks", "LastPlayedDate"}
                old, current = previous["value"], observed["value"]
                require(canonical({key: value for key, value in old.items() if key not in playback_fields}) ==
                        canonical({key: value for key, value in current.items() if key not in playback_fields}),
                        "An owned stop does not authorize changing unrelated UserData fields.")
                require(current["PlayCount"] >= old["PlayCount"] and
                        current["PlaybackPositionTicks"] <= RUNTIME_TICKS,
                        "The stopped playback history is outside the bounded owned episode state.")
                if current.get("LastPlayedDate") is not None:
                    current_date = parse_date(current["LastPlayedDate"])
                    if old.get("LastPlayedDate") is not None:
                        require(current_date >= parse_date(old["LastPlayedDate"]),
                                "The acknowledged stop moved the observed playback date backwards.")
                else:
                    require(old.get("LastPlayedDate") is None,
                            "The acknowledged stop removed an existing playback date.")
                fact["ownedStopStateReconciled"] = True
            self.cleanup_reconciled.add(pair)
            for play in stopped:
                play["stateConfirmed"] = True
            # Select the already frozen reset only after the post-stop full detail.
            if canonical(observed) == canonical(baseline):
                self.queue = [row for row in self.queue if not (row.kind == "cleanup-reset" and
                              row.actor == step.actor and row.item == step.item)]
                fact["cleanupResetRequired"] = False
            else:
                fact["cleanupResetRequired"] = True
        else:
            require(previous is not None and canonical(observed) == canonical(previous),
                    "An untouched or precondition episode changed outside its observed owned lifecycle.")
            if step.kind == "before-play":
                require(zero_state(observed), "The planned lifecycle requires this episode's verified zero baseline.")
        self.current[step.actor][step.item] = deepcopy(observed)

    def lost_response(self, reason):
        """Stop without forgetting an attempt whose effects or token ownership are unknown."""
        require(self.pending is not None and isinstance(reason, str) and reason,
                "A lost-response report needs its pending owned intent and a reason.")
        self.failure = {"label": self.pending["step"].label, "reason": reason, "responseLost": True}
        self.mode, self.outcome = "recovery-required", "recovery_required"

    def begin_cleanup(self):
        """Select only the pre-enumerated cleanup subset; never replay a lost mutation."""
        self._check_frozen_inputs()
        require(not self.cleanup_started, "Cleanup is single-use; a failed cleanup needs a separate recovery manifest.")
        require(self.pending is None, "A lost response needs separate retained recovery; cleanup cannot infer its effects.")
        require(self.mode in ("api-observed", "client-discovery-required", "recovery-required"),
                "Cleanup requires a completed API branch or an explicit retained failure.")
        if any(item not in self.current[actor] for actor, item in self.touched):
            raise MatrixError("Incomplete full-detail ownership needs a separate recovery manifest.")
        rows = []
        for step in cleanup_templates():
            if step.actor not in self.sessions:
                continue
            if step.kind == "cleanup-stop" and (step.lifecycle not in self.plays or self.plays[step.lifecycle]["stopped"]):
                continue
            if step.kind in ("cleanup-reset", "cleanup-reconcile") and (step.actor, step.item) not in self.touched:
                continue
            if step.kind == "cleanup-proof" and step.item not in self.baseline[step.actor]:
                continue
            if step.kind == "cleanup-summary" and step.item not in self.summaries[step.actor]:
                continue
            if step.kind == "profile" and step.actor not in self.profiles:
                continue
            if step.kind == "preferences" and step.actor not in self.preferences:
                continue
            rows.append(step)
        require(len(rows) <= CLEANUP_RESERVE and self.count + len(rows) <= MAX_REQUESTS,
                "The remaining exact cleanup route set exceeds its reserved budget.")
        self.queue, self.mode = rows, "cleanup"
        self.cleanup_started = True
        self._seal_queue()
        return len(rows)

    def client_discovery_plan(self):
        require(self.r0_r4_complete and self.positive is None,
                "The unresolved discovery path requires the all-empty R0-R4 outcome.")
        return {"outcome": "reference_global_positive_unresolved", "maximumPlaybackAttempts": 2,
                "maximumSeconds": 1200, "separateManifestRequired": True,
                "recorderAndBrowserMustNotOverlap": True, "actualClientGlobalRequestRequired": True,
                "actualResponseAndVisibleCardsRequired": True, "R5R6Allowed": False,
                "newQueryHypothesisRequiresSeparateFrozenPublicControl": True}

    def record_client_discovery(self, receipt):
        self.client_discovery_plan()
        require(self.mode in ("closed", "closed-with-observation-failure") and self.revoked == set(ACTORS),
                "The API recorder must close and revoke both tokens before a separate client phase.")
        require(isinstance(receipt, dict) and integer(receipt.get("playbackAttempts")) and
                0 <= receipt["playbackAttempts"] <= 2 and integer(receipt.get("elapsedSeconds")) and
                0 <= receipt["elapsedSeconds"] <= 1200 and type(receipt.get("positiveGlobalResponse")) is bool,
                "The original-client discovery receipt exceeds the bounded attempt/time contract.")
        for key in ("manifestSha256", "responseReceiptSha256", "visibleStateReceiptSha256"):
            require_sha(receipt.get(key), key)
        require(receipt.get("actualClientRequest") is True and receipt.get("seriesIdOmitted") is True and
                receipt.get("injectedResponseOrManualReport") is False,
                "Discovery must retain the actual unmodified client global request and response.")
        self.client_discovery = deepcopy(receipt)
        if receipt["positiveGlobalResponse"]:
            self.outcome = "client_positive_requires_separate_frozen_public_control"
        else:
            self.outcome = "reference_global_positive_unresolved"

    def private_state(self):
        """A private journal snapshot, never an unsanitized public evidence export."""
        return {"schemaVersion": 1, "planSha256": self.plan_sha256, "manifestSha256": digest(self.manifest),
                "mode": self.mode, "outcome": self.outcome, "requestCount": self.count,
                "normalRequestCount": self.normal_count, "cleanupReserve": CLEANUP_RESERVE,
                "pending": None if self.pending is None else {
                    "step": self._template(self.pending["step"]), "request": self.pending["request"].fact(),
                    "intentReceiptSha256": self.pending["intentReceiptSha256"], "ordinal": self.pending["ordinal"],
                    "actorTokenSha256": self.pending["actorTokenSha256"]},
                "remainingSteps": [self._template(step) for step in self.queue],
                "completedLabels": list(self.completed), "facts": deepcopy(self.facts),
                "baseline": deepcopy(self.baseline), "current": deepcopy(self.current),
                "summaries": deepcopy(self.summaries), "profiles": deepcopy(self.profiles),
                "preferences": deepcopy(self.preferences), "sessions": deepcopy(self.sessions),
                "plays": deepcopy(self.plays), "touched": sorted(self.touched),
                "positive": deepcopy(self.positive), "ranking": deepcopy(self.ranking),
                "r0R4Complete": self.r0_r4_complete, "r5R6Started": self.r5_r6_started,
                "extensionComplete": self.extension_complete, "failure": deepcopy(self.failure),
                "cleanupStarted": self.cleanup_started,
                "revokedActors": sorted(self.revoked), "clientDiscovery": deepcopy(self.client_discovery),
                "acceptanceClaim": "No original-client acceptance or global selection rule is inferred."}
