#!/usr/bin/env python3
"""Prepare and operate one private Goby Docker Compose installation."""

import argparse
import hashlib
import http.client
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import stat
import subprocess
import sys
import time
from urllib.parse import parse_qsl, quote, unquote, urlencode, urlsplit, urlunsplit


IMAGE_ID = re.compile(r"sha256:[0-9a-f]{64}\Z")
DIGEST = re.compile(r"[0-9a-f]{64}\Z")
FILE_NAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}\Z")
ENV_KEY = re.compile(r"[A-Z][A-Z0-9_]*\Z")
MAX_CONFIG_BYTES = 1024 * 1024


class OperationError(Exception):
    """An operator-facing fixed code and message, without raw process output."""

    def __init__(self, code, message):
        super().__init__(message)
        self.code = code
        self.message = message


def need(condition, code, message):
    if not condition:
        raise OperationError(code, message)


def emit(value):
    print(json.dumps(value, sort_keys=True), flush=True)


def clean_text(value):
    return isinstance(value, str) and not any(ord(char) < 32 or ord(char) == 127 for char in value)


def path_text(value):
    text = os.fspath(value)
    need(clean_text(text) and "\\" not in text, "path_invalid",
         "Paths must contain no control characters or backslashes.")
    return Path(text).absolute()


def dotenv_quote(value):
    need(clean_text(value) and "\\" not in value, "environment_value_invalid",
         "Compose values must contain no control characters or backslashes.")
    # Compose single-quoted dotenv values preserve literal dollar signs.
    return "'" + value.replace("'", "\\'") + "'"


def raw_environment(content):
    result = {}
    for line in content.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        key, separator, value = line.partition("=")
        need(separator and ENV_KEY.fullmatch(key) and clean_text(value),
             "environment_format_invalid", "Application settings must use one KEY=value per line.")
        need(key not in result, "environment_duplicate", "Application settings contain a duplicate key.")
        result[key] = value
    return result


def read_file(path, maximum=MAX_CONFIG_BYTES, private=False):
    try:
        before = path.lstat()
        need(stat.S_ISREG(before.st_mode) and before.st_size <= maximum,
             "input_file_invalid", "An input must be a bounded regular file, not a symbolic link.")
        if private:
            need(before.st_mode & 0o077 == 0, "secret_file_permissions",
                 "Database and application secret files must have mode 0600 or stricter.")
        data = path.read_bytes()
        need(len(data) <= maximum, "input_file_invalid", "An input exceeds its size limit.")
        return data
    except OperationError:
        raise
    except OSError:
        raise OperationError("input_file_unreadable", "An input file is missing or unreadable.") from None


def read_json(path):
    try:
        return json.loads(read_file(path).decode("utf-8"))
    except (UnicodeError, ValueError):
        raise OperationError("json_invalid", "An installation or release JSON file is invalid.") from None


def file_hash(path):
    digest = hashlib.sha256()
    size = 0
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
            size += len(block)
    return size, digest.hexdigest()


def database_url(value, with_ca=False):
    need(clean_text(value) and value.strip() == value and len(value) <= 16384,
         "database_url_invalid", "The database URL must be a single nonempty PostgreSQL URL.")
    try:
        parsed = urlsplit(value)
        host = parsed.hostname
        port = parsed.port
        pairs = parse_qsl(parsed.query, keep_blank_values=True)
    except ValueError:
        raise OperationError("database_url_invalid", "The database URL format is invalid.") from None
    need(parsed.scheme in ("postgres", "postgresql") and host and parsed.username
         and parsed.path not in ("", "/") and not parsed.fragment
         and (port is None or 0 < port < 65536),
         "database_url_invalid", "Provide an external PostgreSQL URL with a role, host and database.")
    hostname = unquote(host).rstrip(".").lower()
    loopback = hostname in ("localhost", "localhost.localdomain") or hostname.endswith(".localhost")
    try:
        address = ipaddress.ip_address(hostname)
        loopback = loopback or address.is_loopback or address.is_unspecified
        if isinstance(address, ipaddress.IPv6Address) and address.ipv4_mapped:
            loopback = loopback or address.ipv4_mapped.is_loopback or address.ipv4_mapped.is_unspecified
    except ValueError:
        pass
    need(not loopback, "database_host_is_container_loopback",
         "Use a database host reachable from Docker; localhost and loopback refer to the container.")
    need(not any(key.lower() in {"host", "hostaddr", "port", "user", "password", "dbname", "database"}
                 for key, _ in pairs), "database_url_override",
         "Put the database host, role and name in the URL authority/path, not query overrides.")
    if with_ca:
        pairs = [(key, value) for key, value in pairs if key.lower() != "sslrootcert"]
        pairs.append(("sslrootcert", "/etc/goby/postgres-ca.crt"))
        parsed = parsed._replace(query=urlencode(pairs))
    return urlunsplit(parsed)


def database_url_file(path, with_ca=False):
    try:
        text = read_file(path, maximum=16386, private=True).decode("utf-8")
    except UnicodeError:
        raise OperationError("database_url_invalid", "The database URL file must contain UTF-8 text.") from None
    if text.endswith("\n"):
        text = text[:-1]
        if text.endswith("\r"):
            text = text[:-1]
    return database_url(text, with_ca)


def public_url(value):
    need(clean_text(value), "public_url_invalid", "The public URL contains invalid characters.")
    try:
        parsed = urlsplit(value)
        valid_port = parsed.port is None or 0 < parsed.port < 65536
    except ValueError:
        raise OperationError("public_url_invalid", "The public URL format is invalid.") from None
    need(parsed.scheme in ("http", "https") and parsed.hostname and valid_port
         and parsed.username is None and parsed.password is None
         and parsed.path in ("", "/") and not parsed.query and not parsed.fragment,
         "public_url_invalid", "Use an HTTP or HTTPS origin without credentials, query or path prefix.")
    return value.rstrip("/"), parsed.scheme == "https"


def new_directory(value, media, release):
    requested = path_text(value)
    need(not requested.exists() and not requested.is_symlink(), "installation_already_exists",
         "The installation directory already exists. Select a new directory; existing data is never overwritten.")
    need(requested.parent.is_dir(), "installation_parent_missing",
         "Create the installation's parent directory before preparing this installation.")
    destination = requested.parent.resolve() / requested.name
    need(destination != Path("/") and all(not destination.is_relative_to(source)
         and not source.is_relative_to(destination) for source in (media, release)),
         "installation_path_overlap", "The installation, media and release directories must not overlap.")
    return destination


def command_environment():
    # Shell variables must not silently override this installation's dotenv file.
    return {key: value for key, value in os.environ.items()
            if not key.startswith(("GOBY_", "COMPOSE_"))}


def command(arguments, timeout=30, check=True, cwd=None):
    try:
        result = subprocess.run(arguments, cwd=cwd, env=command_environment(),
                                stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, timeout=timeout, check=False)
    except FileNotFoundError:
        raise OperationError("docker_command_missing", "Install Docker Engine and its Compose plugin first.") from None
    except subprocess.TimeoutExpired:
        raise OperationError("docker_command_timeout", "The Docker operation timed out. Inspect this installation's status.") from None
    except OSError:
        raise OperationError("docker_command_failed", "The Docker command could not be executed.") from None
    if check:
        need(result.returncode == 0, "docker_command_failed",
             "Docker could not complete this operation. Check its daemon, Compose version and installation settings.")
    return result


def inspect_image(image_id, user="10001:10001"):
    result = command(["docker", "image", "inspect", "--format",
                      '{{json .}}', image_id], check=False)
    if result.returncode:
        return None
    try:
        value = json.loads(result.stdout)
    except ValueError:
        raise OperationError("image_inspection_invalid", "Docker returned an invalid image inspection.") from None
    need(value.get("Id") == image_id and value.get("Os") == "linux"
         and value.get("Architecture") == "amd64"
         and value.get("Config", {}).get("User") == user,
         "image_profile_mismatch", "The image must match the recorded Linux amd64 identity and service UID/GID.")
    return value


def release_profile(release, profile):
    catalog_path = release / "current-release.json"
    if not catalog_path.exists():
        catalog_path = Path(__file__).resolve().parent / "current-release.json"
    catalog = read_json(catalog_path)
    need(isinstance(catalog, dict) and catalog.get("kind") == "goby-docker-current-release"
         and type(catalog.get("version")) is int and catalog["version"] == 1,
         "release_catalog_invalid", "Use a supported current-release.json catalog.")
    application = catalog.get("application", {})
    need(isinstance(application, dict) and re.fullmatch(r"[0-9a-f]{40}", application.get("sourceRevision", ""))
         and DIGEST.fullmatch(application.get("sha256", "")),
         "release_application_invalid", "The catalog must identify the application source and binary digest.")
    profiles = catalog.get("profiles", {})
    selected = profiles.get(profile) if isinstance(profiles, dict) else None
    need(isinstance(selected, dict) and IMAGE_ID.fullmatch(selected.get("imageId", "")),
         "release_profile_missing", "The requested software or AMD profile is absent or invalid.")
    archive = selected.get("archive", {})
    need(isinstance(archive, dict) and FILE_NAME.fullmatch(archive.get("name", ""))
         and type(archive.get("bytes")) is int and archive["bytes"] > 0
         and DIGEST.fullmatch(archive.get("sha256", "")),
         "release_archive_invalid", "The catalog must include the archive filename, size and SHA-256.")
    names = selected.get("companions")
    need(isinstance(names, list) and len(names) <= 40 and all(isinstance(name, str)
         and FILE_NAME.fullmatch(name) for name in names) and len(names) == len(set(names)),
         "release_companions_invalid", "The catalog companion filenames are invalid.")
    required = {"compose.yaml", "goby.env.example"}
    if profile == "amd":
        required.update(("compose.amd.yaml", "seccomp.amd.json"))
    need(required.issubset(names), "release_companions_missing", "The catalog lacks required deployment companions.")
    hashes = selected.get("companionHashes")
    need(isinstance(hashes, dict), "release_companions_invalid",
         "The companion hash inventory is invalid.")
    companion_data = {}
    for name in names:
        path = catalog_path.parent / name
        if not path.exists():
            path = release / name
        data = read_file(path)
        need(isinstance(hashes.get(name), str) and DIGEST.fullmatch(hashes[name])
             and hashlib.sha256(data).hexdigest() == hashes[name],
             "companion_hash_mismatch", "A deployment companion differs from the current release catalog.")
        companion_data[name] = data
    return catalog, selected, companion_data


def release_player(catalog, companions):
    player = catalog.get("player")
    need(isinstance(player, dict) and IMAGE_ID.fullmatch(player.get("imageId", "")),
         "release_player_missing", "The selected release does not identify an independently built player image.")
    archive = player.get("archive", {})
    need(isinstance(archive, dict) and FILE_NAME.fullmatch(archive.get("name", ""))
         and type(archive.get("bytes")) is int and archive["bytes"] > 0
         and DIGEST.fullmatch(archive.get("sha256", "")),
         "release_player_archive_invalid", "The player catalog requires an archive filename, size and SHA-256.")
    need("compose.player.yaml" in companions, "player_companion_missing",
         "The selected profile lacks its matching compose.player.yaml companion.")
    return player


def load_release_image(release, profile, user="10001:10001"):
    archive = release / profile["archive"]["name"]
    need(archive.is_file() and not archive.is_symlink(), "archive_missing",
         "A selected image archive is missing from its release directory.")
    size, digest = file_hash(archive)
    need(size == profile["archive"]["bytes"] and digest == profile["archive"]["sha256"],
         "archive_hash_mismatch", "An image archive does not match the current release size and SHA-256.")
    image_id = profile["imageId"]
    if inspect_image(image_id, user) is None:
        command(["docker", "image", "load", "--input", str(archive)], timeout=600)
        need(inspect_image(image_id, user) is not None, "image_missing_after_load",
             "The archive did not provide its recorded image ID.")
    return image_id


def amd_device(value):
    need(value is not None, "amd_render_node_required", "The AMD profile requires --render-node for a real AMD render device.")
    node = path_text(value)
    need(re.fullmatch(r"/dev/dri/renderD[0-9]+", str(node)), "amd_render_node_invalid",
         "Select a render node such as /dev/dri/renderD128.")
    try:
        info = node.lstat()
        vendor = Path("/sys/dev/char") / f"{os.major(info.st_rdev)}:{os.minor(info.st_rdev)}" / "device/vendor"
        need(stat.S_ISCHR(info.st_mode) and vendor.read_text().strip().lower() == "0x1002",
             "amd_render_node_invalid", "The selected node must be an accessible AMD render character device.")
    except OSError:
        raise OperationError("amd_render_node_unavailable", "The selected AMD render node or its sysfs identity is unavailable.") from None
    return str(node), str(info.st_gid)


def write_new(path, data, mode=0o600):
    if isinstance(data, str):
        data = data.encode("utf-8")
    with path.open("xb") as target:
        os.chmod(path, mode)
        target.write(data)


def prepare(args):
    need(os.geteuid() == 0, "root_required", "Run prepare with sudo/root to create private state owned by UID/GID 10001.")
    release = path_text(args.release_dir).resolve(strict=True)
    media = path_text(args.media_dir).resolve(strict=True)
    need(release.is_dir() and media.is_dir(), "directory_invalid", "Release and media inputs must be existing directories.")
    destination = new_directory(args.directory, media, release)
    need(1 <= args.host_port <= 65535, "host_port_invalid", "The host port must be between 1 and 65535.")
    need(args.with_player or (args.player_host_port is None and args.player_release_dir is None),
         "player_option_without_service", "Select --with-player before specifying player options.")
    player_port = args.player_host_port if args.player_host_port is not None else 8080
    need(not args.with_player or (1 <= player_port <= 65535 and player_port != args.host_port),
         "player_host_port_invalid", "The player port must be between 1 and 65535 and differ from the backend port.")
    origin, secure_cookie = public_url(args.public_url)
    ca = read_file(path_text(args.database_ca_file), maximum=256 * 1024) if args.database_ca_file else None
    if ca is not None:
        need(b"-----BEGIN CERTIFICATE-----" in ca, "database_ca_invalid", "The PostgreSQL CA file must contain a PEM certificate.")
    database = database_url_file(path_text(args.database_url_file), with_ca=ca is not None)
    recovery = (database_url_file(path_text(args.recovery_database_url_file), with_ca=ca is not None)
                if args.recovery_database_url_file else None)
    catalog, profile, companions = release_profile(release, args.profile)
    need(not args.writable_subtitles or "compose.subtitles.yaml" in companions,
         "subtitle_companion_missing", "Writable subtitles require the matching compose.subtitles.yaml companion.")
    need(not args.writable_media or "compose.background-previews.yaml" in companions,
         "media_write_companion_missing", "Writable media requires the matching compose.background-previews.yaml companion.")
    player = release_player(catalog, companions) if args.with_player else None
    player_release = (path_text(args.player_release_dir).resolve(strict=True)
                      if args.player_release_dir else release)
    need(player_release.is_dir(), "directory_invalid", "The player release directory must exist.")
    need(not args.with_player or (not destination.is_relative_to(player_release)
         and not player_release.is_relative_to(destination)), "installation_path_overlap",
         "The installation and player release directories must not overlap.")
    render = amd_device(args.render_node) if args.profile == "amd" else None
    need(args.profile == "amd" or args.render_node is None, "render_node_profile_mismatch",
         "Select the AMD profile when specifying a render node.")
    command(["docker", "info", "--format", "{{.OSType}}"])
    image_id = load_release_image(release, profile)
    if player:
        load_release_image(player_release, player, "101:101")
    try:
        app_env = raw_environment(companions["goby.env.example"].decode("utf-8"))
    except UnicodeError:
        raise OperationError("environment_format_invalid", "The application environment example is not UTF-8.") from None
    token = secrets.token_urlsafe(32)
    app_env.update(GOBY_DATABASE_URL=database, GOBY_PUBLIC_URL=origin,
                   GOBY_SETUP_TOKEN=token, GOBY_COOKIE_SECURE=str(secure_cookie).lower())
    if recovery is not None:
        app_env["GOBY_RECOVERY_DATABASE_URL"] = recovery
    project = "goby-" + hashlib.sha256(str(destination).encode()).hexdigest()[:16]
    compose_files = ["compose.yaml"]
    deployment = {"GOBY_OCI_IMAGE": image_id, "GOBY_CONFIG_FILE": str(destination / "goby.env"),
                  "GOBY_HOST_PORT": str(args.host_port), "GOBY_STATE_DIR": str(destination / "state"),
                  "GOBY_CACHE_DIR": str(destination / "cache"), "GOBY_LOG_DIR_HOST": str(destination / "logs"),
                  "GOBY_MEDIA_DIR": str(media), "GOBY_CPU_LIMIT": "2.0", "GOBY_MEMORY_LIMIT": "2g", "GOBY_PID_LIMIT": "256"}
    if render:
        deployment.update(GOBY_AMD_RENDER_NODE=render[0], GOBY_AMD_RENDER_GID=render[1],
                          GOBY_AMD_SECCOMP_PROFILE=str(destination / "seccomp.amd.json"))
        compose_files.append("compose.amd.yaml")
    if args.writable_subtitles:
        compose_files.append("compose.subtitles.yaml")
    if args.writable_media:
        compose_files.append("compose.background-previews.yaml")
    if player:
        deployment.update(GOBY_PLAYER_IMAGE=player["imageId"], GOBY_PLAYER_HOST_PORT=str(player_port),
                          GOBY_PLAYER_API_UPSTREAM="http://goby:8096")
        compose_files.append("compose.player.yaml")
    if ca is not None:
        compose_files.append("compose.install.yaml")
    # The destination is created only after all input and image checks succeed.
    # A later failure retains this new private directory for operator inspection.
    destination.mkdir(mode=0o700)
    os.chmod(destination, 0o700)
    for name in ("state", "cache", "logs"):
        path = destination / name
        path.mkdir(mode=0o700)
        os.chown(path, 10001, 10001)
        os.chmod(path, 0o700)
    for name, data in companions.items():
        if name not in ("goby-docker.py", "current-release.json", "installation.json", "goby.env", "deployment.env", "setup-token.txt"):
            write_new(destination / name, data, 0o644)
    write_new(destination / "goby-docker.py", Path(__file__).read_bytes(), 0o755)
    write_new(destination / "current-release.json", json.dumps(catalog, indent=2) + "\n", 0o644)
    write_new(destination / "goby.env", "".join(f"{key}={value}\n" for key, value in app_env.items()))
    write_new(destination / "deployment.env", "".join(f"{key}={dotenv_quote(value)}\n" for key, value in deployment.items()))
    write_new(destination / "setup-token.txt", token + "\n")
    if ca is not None:
        write_new(destination / "pg-ca.crt", ca, 0o444)
        override = {"services": {"goby": {"volumes": [{"type": "bind",
                    "source": str(destination / "pg-ca.crt").replace("$", "$$"),
                    "target": "/etc/goby/postgres-ca.crt", "read_only": True,
                    "bind": {"create_host_path": False}}]}}}
        write_new(destination / "compose.install.yaml", json.dumps(override, indent=2) + "\n", 0o644)
    metadata = {"kind": "goby-docker-installation", "version": 1, "directory": str(destination),
                "profile": args.profile, "project_name": project, "compose_files": compose_files,
                "image_id": image_id, "host_port": args.host_port, "public_url": origin,
                "application": catalog["application"], "writable_subtitles": args.writable_subtitles,
                "writable_media": args.writable_media or args.writable_subtitles}
    if player:
        metadata["player"] = {"image_id": player["imageId"], "host_port": player_port}
    write_new(destination / "installation.json", json.dumps(metadata, indent=2) + "\n")
    emit({"status": "prepared", "directory": str(destination), "profile": args.profile,
          "imageId": image_id, "services": selected_services(metadata),
          "playerImageId": player["imageId"] if player else None,
          "setupTokenFile": str(destination / "setup-token.txt"),
          "message": "Run check, then start. Read the setup token privately from the indicated file."})


def installation(value):
    directory = path_text(value).resolve(strict=True)
    metadata = read_json(directory / "installation.json")
    need(isinstance(metadata, dict) and metadata.get("kind") == "goby-docker-installation"
         and metadata.get("version") == 1 and metadata.get("directory") == str(directory)
         and metadata.get("profile") in ("software", "amd")
         and re.fullmatch(r"goby-[0-9a-f]{16}", metadata.get("project_name", ""))
         and IMAGE_ID.fullmatch(metadata.get("image_id", ""))
         and type(metadata.get("host_port")) is int and 1 <= metadata["host_port"] <= 65535,
         "installation_invalid", "This directory lacks a valid Goby Docker installation record.")
    allowed = {"compose.yaml", "compose.amd.yaml", "compose.subtitles.yaml", "compose.install.yaml",
               "compose.background-previews.yaml", "compose.player.yaml"}
    files = metadata.get("compose_files")
    need(isinstance(files, list) and files and files[0] == "compose.yaml"
         and all(name in allowed for name in files) and len(files) == len(set(files)),
         "installation_invalid", "The installation Compose file list is invalid.")
    player = metadata.get("player")
    need((player is not None) == ("compose.player.yaml" in files)
         and (player is None or (isinstance(player, dict) and IMAGE_ID.fullmatch(player.get("image_id", ""))
              and type(player.get("host_port")) is int and 1 <= player["host_port"] <= 65535
              and player["host_port"] != metadata["host_port"])),
         "installation_invalid", "The optional player record and Compose selection must agree.")
    for name in files:
        read_file(directory / name)
    read_file(directory / "deployment.env", private=True)
    read_file(directory / "goby.env", private=True)
    return directory, metadata


def selected_services(metadata):
    return ["goby", "player"] if metadata.get("player") is not None else ["goby"]


def writable_media(metadata):
    return metadata.get("writable_media", metadata.get("writable_subtitles", False)) is True


def compose(directory, metadata, arguments, **kwargs):
    invocation = ["docker", "compose", "--project-directory", str(directory),
                  "--project-name", metadata["project_name"], "--env-file", str(directory / "deployment.env")]
    for name in metadata["compose_files"]:
        invocation.extend(("-f", str(directory / name)))
    return command(invocation + arguments, cwd=directory, **kwargs)


def configured_value(directory, key):
    environment = raw_environment(read_file(directory / "deployment.env", private=True).decode("utf-8"))
    value = environment.get(key, "")
    if len(value) > 1 and value[0] == value[-1] and value[0] in ("'", '"'):
        value = value[1:-1]
    return value


def configured_image(directory, service="goby"):
    value = configured_value(directory, "GOBY_PLAYER_IMAGE" if service == "player" else "GOBY_OCI_IMAGE")
    need(IMAGE_ID.fullmatch(value), "configured_image_invalid",
         "Each selected service image must contain an explicit immutable image ID from the selected release receipt.")
    return value


def configured_port(directory, service="goby"):
    value = configured_value(directory, "GOBY_PLAYER_HOST_PORT" if service == "player" else "GOBY_HOST_PORT")
    need(value.isascii() and value.isdecimal() and 1 <= int(value) <= 65535,
         "configured_port_invalid", "Each selected service must have an explicit port between 1 and 65535.")
    return int(value)


def published_loopback_port(details, expected, service="goby"):
    ports = details.get("Ports")
    bindings = ports.get("8080/tcp" if service == "player" else "8096/tcp") if isinstance(ports, dict) else None
    need(isinstance(bindings, list) and len(bindings) == 1 and isinstance(bindings[0], dict)
         and bindings[0].get("HostIp") == "127.0.0.1" and bindings[0].get("HostPort") == str(expected),
         "container_port_mismatch", "The container's loopback port differs from deployment.env. Check the selected installation and recreate it before diagnosing readiness.")
    return expected


CHECK_SCRIPT = r'''test "$(id -u):$(id -g)" = "10001:10001" || { echo IDENTITY_MISMATCH; exit 1; }
for directory in /var/lib/goby /var/cache/goby /var/log/goby; do
  case "$directory" in
    /var/lib/goby) label=STATE_NOT_WRITABLE ;;
    /var/cache/goby) label=CACHE_NOT_WRITABLE ;;
    /var/log/goby) label=LOGS_NOT_WRITABLE ;;
  esac
  probe="$directory/.goby-docker-check-$1"
  (set -C; printf check > "$probe") 2>/dev/null || { echo "$label"; exit 1; }
  trap 'rm -f -- "$probe"' EXIT HUP INT TERM
  test "$(cat "$probe")" = check || { echo "$label"; exit 1; }
  rm -f -- "$probe" || { echo "$label"; exit 1; }
  trap - EXIT HUP INT TERM
done
test -r /media && test -x /media || { echo MEDIA_NOT_READABLE; exit 1; }
sample="$(find /media -maxdepth 4 -type f -print -quit 2>/dev/null)" || { echo MEDIA_NOT_READABLE; exit 1; }
if test -n "$sample"; then
  dd if="$sample" of=/dev/null bs=1 count=1 status=none 2>/dev/null || { echo MEDIA_NOT_READABLE; exit 1; }
  echo MEDIA_SAMPLE_READABLE
else
  echo MEDIA_DIRECTORY_READABLE_NO_SAMPLE
fi
if test "$2" = writable; then
  directory=/media
  if test -n "$sample"; then directory="$(dirname "$sample")"; fi
  probe="$directory/.goby-docker-check-$1"
  (set -C; printf check > "$probe") 2>/dev/null || { echo MEDIA_NOT_WRITABLE; exit 1; }
  trap 'rm -f -- "$probe"' EXIT HUP INT TERM
  test "$(cat "$probe")" = check || { echo MEDIA_NOT_WRITABLE; exit 1; }
  rm -f -- "$probe" || { echo MEDIA_NOT_WRITABLE; exit 1; }
  trap - EXIT HUP INT TERM
  echo MEDIA_SAMPLE_PARENT_WRITABLE
fi
if test "$3" = amd; then
  test -n "$GOBY_HW_DEVICE" && test -c "$GOBY_HW_DEVICE" && test -r "$GOBY_HW_DEVICE" && test -w "$GOBY_HW_DEVICE" || { echo AMD_DEVICE_NOT_ACCESSIBLE; exit 1; }
  echo AMD_DEVICE_ACCESSIBLE
fi
echo CHECK_PASSED
'''


PLAYER_CHECK_SCRIPT = r'''test "$(id -u):$(id -g)" = "101:101" || { echo PLAYER_IDENTITY_MISMATCH; exit 1; }
test -r /usr/share/nginx/html/index.html && test -r /etc/nginx/templates/nginx.conf.template || { echo PLAYER_ASSETS_MISSING; exit 1; }
probe="/tmp/.goby-player-check-$1"
(set -C; printf check > "$probe") 2>/dev/null || { echo PLAYER_TMP_NOT_WRITABLE; exit 1; }
trap 'rm -f -- "$probe"' EXIT HUP INT TERM
test "$(cat "$probe")" = check || { echo PLAYER_TMP_NOT_WRITABLE; exit 1; }
rm -f -- "$probe" || { echo PLAYER_TMP_NOT_WRITABLE; exit 1; }
trap - EXIT HUP INT TERM
echo PLAYER_CHECK_PASSED
'''


def container_check(directory, metadata, service, script, extra):
    name = metadata["project_name"] + "-check-" + service + "-" + secrets.token_hex(5)
    try:
        result = compose(directory, metadata, ["run", "--rm", "--no-deps", "--pull", "never", "--name", name,
                         "--entrypoint", "sh", service, "-eu", "-c", script, "goby-check", secrets.token_hex(8), *extra],
                         timeout=45, check=False)
    finally:
        # This unique one-shot container belongs only to this invocation.
        command(["docker", "container", "rm", "--force", name], timeout=15, check=False)
    return result


def check_installation(directory, metadata):
    for service in selected_services(metadata):
        need(inspect_image(configured_image(directory, service), "101:101" if service == "player" else "10001:10001") is not None,
             "configured_image_missing", "Load each configured image archive before checking or starting this installation.")
    if metadata.get("player"):
        need(configured_port(directory) != configured_port(directory, "player"), "player_host_port_invalid",
             "The player and backend host ports must differ.")
        need(configured_value(directory, "GOBY_PLAYER_API_UPSTREAM") in ("", "http://goby:8096"),
             "player_upstream_invalid", "The managed player must proxy this installation's goby service at http://goby:8096.")
    if metadata["profile"] == "amd":
        render, group = amd_device(configured_value(directory, "GOBY_AMD_RENDER_NODE"))
        need(group == configured_value(directory, "GOBY_AMD_RENDER_GID"), "amd_render_group_changed",
             "The selected render device group changed. Update GOBY_AMD_RENDER_GID before checking or starting.")
    result = compose(directory, metadata, ["config", "--quiet"], check=False)
    need(result.returncode == 0, "compose_configuration_invalid",
         "Compose configuration is invalid. Check installation files and Compose support for env_file.format: raw.")
    result = container_check(directory, metadata, "goby", CHECK_SCRIPT,
                             ["writable" if writable_media(metadata) else "readonly", metadata["profile"]])
    lines = result.stdout.decode("utf-8", errors="replace").splitlines()
    labels = {"IDENTITY_MISMATCH": ("container_identity_mismatch", "The check must run as UID/GID 10001."),
              "STATE_NOT_WRITABLE": ("state_not_writable", "UID 10001 cannot write the installation state directory."),
              "CACHE_NOT_WRITABLE": ("cache_not_writable", "UID 10001 cannot write the installation cache directory."),
              "LOGS_NOT_WRITABLE": ("logs_not_writable", "UID 10001 cannot write the installation log directory."),
              "MEDIA_NOT_READABLE": ("media_not_readable", "UID 10001 cannot traverse or read the sampled media. Adjust host access without changing the container user."),
              "MEDIA_NOT_WRITABLE": ("media_not_writable", "UID 10001 cannot create a sidecar probe in the sampled source directory. Grant targeted host access; the helper never changes media ownership."),
              "AMD_DEVICE_NOT_ACCESSIBLE": ("amd_device_not_accessible", "UID 10001 cannot read and write the selected AMD render device. Check its device mapping and supplemental group.")}
    for label, (code, message) in labels.items():
        if label in lines:
            raise OperationError(code, message)
    need(result.returncode == 0 and "CHECK_PASSED" in lines, "container_check_failed",
         "The temporary identity and mount check failed. Check Docker device/mount access and installation files.")
    if metadata.get("player"):
        player_result = container_check(directory, metadata, "player", PLAYER_CHECK_SCRIPT, [])
        player_lines = player_result.stdout.decode("utf-8", errors="replace").splitlines()
        need(player_result.returncode == 0 and "PLAYER_CHECK_PASSED" in player_lines, "player_check_failed",
             "The player UID, bundled assets or temporary storage check failed.")
    return {"status": "checked", "databaseAuthenticationChecked": False,
            "services": selected_services(metadata), "amdDeviceAccessChecked": "AMD_DEVICE_ACCESSIBLE" in lines,
            "mediaSampleRead": "MEDIA_SAMPLE_READABLE" in lines,
            "mediaWriteSampleChecked": "MEDIA_SAMPLE_PARENT_WRITABLE" in lines,
            "message": "Selected images, service identities and sampled mount checks passed. Generation remains opt-in. Database authentication and player proxy readiness are checked at startup."}


def readiness(port):
    connection = http.client.HTTPConnection("127.0.0.1", port, timeout=3)
    try:
        connection.request("GET", "/readyz", headers={"Connection": "close"})
        response = connection.getresponse()
        body = response.read(16385)
        if len(body) > 16384:
            return "invalid_response", None
        try:
            value = json.loads(body)
        except (ValueError, UnicodeError):
            return "invalid_response", None
        if response.status == 200 and isinstance(value, dict) and value.get("Status") == "ready":
            return "ready", None
        error = value.get("Error", {}) if isinstance(value, dict) else {}
        return "not_ready", error.get("Code") if isinstance(error, dict) else None
    except (OSError, http.client.HTTPException):
        return "unreachable", None
    finally:
        connection.close()


def player_readiness(port, backend_port):
    # The player health route alone cannot establish its API proxy readiness.
    def response(endpoint, path):
        connection = http.client.HTTPConnection("127.0.0.1", endpoint, timeout=3)
        try:
            connection.request("GET", path, headers={"Connection": "close"})
            answer = connection.getresponse()
            body = answer.read(16385)
            if answer.status != 200 or len(body) > 16384:
                return None
            return body
        except (OSError, http.client.HTTPException):
            return None
        finally:
            connection.close()
    if response(port, "/healthz") != b"ok\n":
        return "unreachable", None
    try:
        backend = json.loads(response(backend_port, "/emby/System/Info/Public") or b"null")
        proxied = json.loads(response(port, "/emby/System/Info/Public") or b"null")
    except (ValueError, UnicodeError):
        return "not_ready", "player_proxy_not_ready"
    if (isinstance(backend, dict) and isinstance(proxied, dict)
            and isinstance(backend.get("Id"), str) and backend["Id"]
            and backend["Id"] == proxied.get("Id")):
        return "ready", None
    return "not_ready", "player_proxy_not_ready"


def classify_status(state, observed=None, error_code=None):
    if not isinstance(error_code, str):
        error_code = None
    current = state.get("Status")
    if current is None:
        return {"status": "not_created", "safeErrorCode": "not_created", "message": "Run check, then start to create this installation's container."}
    if current == "exited":
        stopped = state.get("ExitCode") == 0 and not state.get("OOMKilled", False)
        return {"status": "stopped" if stopped else "exited", "safeErrorCode": "stopped" if stopped else "container_exited",
                "exitCode": state.get("ExitCode"), "oomKilled": bool(state.get("OOMKilled")),
                "message": "Start the installation when ready." if stopped else "Read the redacted logs, correct the cause, then start again."}
    if current in ("created", "restarting"):
        return {"status": "starting", "safeErrorCode": "container_starting", "message": "The container has not reached running state yet."}
    if current != "running":
        return {"status": "not_ready", "safeErrorCode": "container_not_running", "message": "Inspect the container state before starting this installation."}
    if observed == "ready":
        return {"status": "ready", "safeErrorCode": None, "message": "Goby is ready."}
    reasons = {"not_ready": ("database_not_ready", "PostgreSQL is unavailable. Check the database service, URL, role and TLS settings."),
               "catalog_not_ready": ("catalog_not_ready", "The catalog session is unavailable. Repair PostgreSQL connectivity, then stop and start Goby."),
               "tasks_not_ready": ("tasks_not_ready", "The task scheduler is unavailable. Inspect logs before restarting Goby."),
               "diagnostics_not_ready": ("diagnostics_not_ready", "Diagnostic storage is unavailable. Check the log directory access and free space.")}
    if isinstance(error_code, str) and error_code.startswith("transcoding_"):
        code, message = "transcode_not_ready", "The conversion engine is unavailable. Check cache access, free space and media-tool logs."
    else:
        code, message = reasons.get(error_code, ("application_not_ready", "The application is not ready. Inspect this installation's redacted logs."))
    if observed == "unreachable":
        return {"status": "starting", "safeErrorCode": "readiness_unreachable", "message": "The container is running but its loopback readiness endpoint is not reachable yet. Inspect logs if this persists."}
    return {"status": "not_ready", "safeErrorCode": code, "message": message}


def terminal_diagnostic(raw):
    reasons = {
        "database_lease_unavailable": ("database_connection_lost", "Goby stopped after losing its PostgreSQL catalog connection. Restore database connectivity, then start this installation."),
        "database_lease_busy": ("database_in_use", "Another Goby process owns this catalog. Check the selected database and existing installation before starting again."),
        "permission_denied": ("startup_permission_denied", "Startup encountered a permission failure. Run check and review the installation's path permissions."),
        "no_space": ("storage_full", "Goby stopped after running out of storage. Free space on the installation's data volumes before starting again."),
        "address_in_use": ("listener_address_in_use", "The application listener address is already in use. Check this installation's listen configuration."),
    }
    for line in reversed(raw[-65536:].decode("utf-8", errors="replace").splitlines()):
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if not isinstance(event, dict):
            continue
        if event.get("event") == "server.starting":
            break
        if event.get("event") == "server.stopped":
            value = event.get("error_class")
            return reasons.get(value) if isinstance(value, str) else None
    return None


def service_status(directory, metadata, service):
    result = compose(directory, metadata, ["ps", "--all", "--quiet", service])
    ids = result.stdout.decode("ascii", errors="replace").split()
    need(len(ids) <= 1 and all(re.fullmatch(r"[0-9a-f]{12,64}", item) for item in ids),
         "container_inventory_invalid", "Expected at most one container per selected service.")
    if not ids:
        return classify_status({})
    result = command(["docker", "container", "inspect", "--format",
                      '{"State":{{json .State}},"Image":{{json .Image}},"Ports":{{json .NetworkSettings.Ports}}}', ids[0]])
    try:
        details = json.loads(result.stdout)
    except ValueError:
        raise OperationError("container_inventory_invalid", "Docker returned an invalid container state.") from None
    need(isinstance(details, dict) and isinstance(details.get("State"), dict),
         "container_inventory_invalid", "Docker returned an invalid container state.")
    need(details.get("Image") == configured_image(directory, service), "container_image_mismatch",
         "The existing container uses a different image from deployment.env. Start after completing the selected update or rollback steps.")
    state = details.get("State", {})
    if state.get("Status") == "running":
        port = published_loopback_port(details, configured_port(directory, service), service)
        if service == "player":
            observation, code = player_readiness(port, configured_port(directory))
        else:
            observation, code = readiness(port)
    else:
        observation, code = None, None
    answer = classify_status(state, observation, code)
    if service == "player":
        if code == "player_proxy_not_ready":
            answer.update(status="not_ready", safeErrorCode=code,
                          message="The player is serving HTTP but its API proxy is not reaching this installation's backend.")
        elif answer["status"] == "ready":
            answer["message"] = "The player and its backend API proxy are ready."
    if service == "goby" and answer["status"] == "exited" and not state.get("OOMKilled", False):
        started = state.get("StartedAt")
        if isinstance(started, str) and re.fullmatch(r"[0-9TZ:.+-]{10,64}", started):
            logs = command(["docker", "logs", "--since", started, "--tail", "80", ids[0]], timeout=10, check=False)
            if logs.returncode == 0:
                diagnostic = terminal_diagnostic(logs.stdout + logs.stderr)
                if diagnostic is not None:
                    answer["safeErrorCode"], answer["message"] = diagnostic
    answer["containerId"] = ids[0][:12]
    return answer


def status_installation(directory, metadata):
    backend = service_status(directory, metadata, "goby")
    if metadata.get("player") is None:
        return backend
    player = service_status(directory, metadata, "player")
    answer = dict(backend if backend["status"] != "ready" else player)
    answer["services"] = {"goby": backend, "player": player}
    if backend["status"] == "ready" and player["status"] == "ready":
        answer["message"] = "Goby, the independent player and its backend API proxy are ready."
    elif backend["status"] in ("stopped", "not_created") and player["status"] != backend["status"]:
        answer.update(status="not_ready", safeErrorCode="service_state_mismatch",
                      message="The selected services have different states. Inspect both service records.")
    return answer


def secret_values(environment):
    values = set()
    for key, value in environment.items():
        if not value:
            continue
        if "DATABASE_URL" in key:
            values.add(value)
            try:
                password = urlsplit(value).password
                if password:
                    values.update((password, unquote(password)))
            except ValueError:
                pass
        elif any(word in key for word in ("TOKEN", "PASSWORD", "SECRET", "API_KEY")) and not key.endswith("_FILE"):
            values.add(value)
    for value in tuple(values):
        values.update((quote(value, safe=""), json.dumps(value)[1:-1]))
    return sorted((value for value in values if value), key=len, reverse=True)


def redact(text, values):
    for value in values:
        text = text.replace(value, "[REDACTED]")
    return re.sub(r"(?i)\bpostgres(?:ql)?://[^\s\"']+", "[REDACTED_DATABASE_URL]", text)


def operate(args):
    directory, metadata = installation(args.directory)
    if args.action == "check":
        emit(check_installation(directory, metadata))
    elif args.action == "status":
        emit(status_installation(directory, metadata))
    elif args.action == "start":
        need(1 <= args.wait_timeout <= 600, "wait_timeout_invalid", "The readiness wait must be between 1 and 600 seconds.")
        check_installation(directory, metadata)
        compose(directory, metadata, ["up", "--detach", "--no-build", "--pull", "never", *selected_services(metadata)], timeout=180)
        deadline = time.monotonic() + args.wait_timeout
        while True:
            answer = status_installation(directory, metadata)
            if answer["status"] == "ready":
                emit(answer)
                return 0
            if answer["status"] in ("exited", "stopped") or time.monotonic() >= deadline:
                answer["message"] += " The container and data are retained; no automatic rollback was attempted."
                emit(answer)
                return 1
            time.sleep(min(1, max(0, deadline - time.monotonic())))
    elif args.action == "stop":
        compose(directory, metadata, ["stop", *reversed(selected_services(metadata))], timeout=180)
        emit({"status": "stopped", "message": "The installation was stopped; containers and persistent data are retained."})
    elif args.action == "logs":
        need(1 <= args.lines <= 500, "log_limit_invalid", "Request between 1 and 500 log lines.")
        environment = raw_environment(read_file(directory / "goby.env", private=True).decode("utf-8"))
        token_path = directory / "setup-token.txt"
        if token_path.exists():
            environment["GOBY_INITIAL_SETUP_TOKEN"] = read_file(token_path, private=True).decode("utf-8").strip()
        result = compose(directory, metadata, ["logs", "--no-color", "--tail", str(args.lines), *selected_services(metadata)])
        output = redact((result.stdout + result.stderr).decode("utf-8", errors="replace"), secret_values(environment))
        if len(output) > 128 * 1024:
            output = "[Earlier output omitted by the helper's size limit]\n" + output[-128 * 1024:]
        print(output, end="" if output.endswith("\n") else "\n")
    return 0


def parser():
    result = argparse.ArgumentParser(description=__doc__)
    actions = result.add_subparsers(dest="action", required=True)
    setup = actions.add_parser("prepare", help="Create a new private installation from an existing release archive")
    setup.add_argument("--release-dir", required=True)
    setup.add_argument("--directory", required=True)
    setup.add_argument("--media-dir", required=True)
    setup.add_argument("--database-url-file", required=True)
    setup.add_argument("--public-url", required=True)
    setup.add_argument("--profile", choices=("software", "amd"), default="software")
    setup.add_argument("--host-port", type=int, default=8096)
    setup.add_argument("--database-ca-file")
    setup.add_argument("--recovery-database-url-file")
    setup.add_argument("--writable-subtitles", action="store_true")
    setup.add_argument("--writable-media", action="store_true",
                       help="Allow persistent media sidecars; does not enable automatic generation")
    setup.add_argument("--with-player", action="store_true", help="Install the independently packaged player")
    setup.add_argument("--player-host-port", type=int, help="Player loopback port (default: 8080 when selected)")
    setup.add_argument("--player-release-dir", help="Separate directory containing the recorded player image archive")
    setup.add_argument("--render-node")
    for name in ("check", "start", "status", "stop", "logs"):
        item = actions.add_parser(name)
        item.add_argument("--directory", required=True)
        if name == "start":
            item.add_argument("--wait-timeout", type=int, default=90)
        if name == "logs":
            item.add_argument("--lines", type=int, default=100)
    return result


def main(argv=None):
    try:
        args = parser().parse_args(argv)
        need(sys.platform == "linux" and sys.version_info >= (3, 10), "platform_unsupported",
             "Run this helper on Linux with Python 3.10 or newer.")
        if args.action == "prepare":
            prepare(args)
            return 0
        return operate(args)
    except OperationError as error:
        emit({"status": "error", "safeErrorCode": error.code, "message": error.message})
    except PermissionError:
        emit({"status": "error", "safeErrorCode": "permission_denied", "message": "Run this installation's helper with sudo/root to read its private configuration."})
    except (OSError, ValueError, TypeError, KeyError, UnicodeError):
        emit({"status": "error", "safeErrorCode": "input_or_operation_invalid", "message": "An installation input or Docker response is invalid or unavailable. Check paths and the current release companion files."})
    except KeyboardInterrupt:
        emit({"status": "error", "safeErrorCode": "interrupted", "message": "The operation was interrupted. Existing installation data is retained."})
    return 1


if __name__ == "__main__":
    sys.exit(main())
