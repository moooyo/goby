#!/usr/bin/env bash
# Prepare the official reference package without installing a system service.
# Execute only on the authorized Linux test environment.
set -euo pipefail

base=/dev/shm/goby-emby-reference
data=/opt/goby-test/emby-reference-data
marker=goby-emby-reference-owned-v1
package=emby-server-deb_4.9.5.0_amd64.deb
url=https://github.com/MediaBrowser/Emby.Releases/releases/download/4.9.5.0/$package
sha256=1d718ffa0169c393de3eafda65b1b057a3db4ead93ffeb5883abd01735de9843

umask 077
for directory in "$base" "$data"; do
    if [[ -e "$directory" ]]; then
        [[ -d "$directory" && -f "$directory/.goby-managed" ]]
        [[ "$(cat "$directory/.goby-managed")" == "$marker" ]]
    else
        install -d -m 700 "$directory"
        printf '%s\n' "$marker" > "$directory/.goby-managed"
    fi
done

[[ "$(uname -m)" == x86_64 ]]
available=$(df --output=avail -B1 "$base" | tail -n 1 | tr -d ' ')
[[ "$available" -gt 1073741824 ]]

if [[ ! -f "$base/$package" ]]; then
    curl --fail --show-error --location --connect-timeout 15 --max-time 180 \
        "$url" -o "$base/$package.partial"
    mv -- "$base/$package.partial" "$base/$package"
fi
printf '%s  %s\n' "$sha256" "$base/$package" | sha256sum --check --status
printf 'Verified official package: %s\n' "$package"
dpkg-deb --field "$base/$package" Package Version Architecture Installed-Size Depends

if [[ ! -f "$base/.extracted" ]]; then
    install -d -m 700 "$base/package" "$base/control"
    dpkg-deb --extract "$base/$package" "$base/package"
    dpkg-deb --control "$base/$package" "$base/control"
    printf '%s\n' "$sha256" > "$base/.extracted"
fi

printf '\nOfficial package launch files:\n'
find "$base/package" -maxdepth 6 -type f \
    \( -name '*.service' -o -name 'emby-server' -o -name 'emby-server.sh' \) -print
printf '\nReference storage:\n'
du -sh "$base" "$data"
df -h / /dev/shm
