#!/usr/bin/env bash
# Start the reference in a private network namespace, never on the host network.
# Execute only on the authorized Linux test environment after reference-prepare.sh.
set -euo pipefail

base=/dev/shm/goby-emby-reference
data=/opt/goby-test/emby-reference-data
marker=goby-emby-reference-owned-v1
unit=goby-emby-reference

umask 077
for directory in "$base" "$data"; do
    [[ -f "$directory/.goby-managed" ]]
    [[ "$(cat "$directory/.goby-managed")" == "$marker" ]]
done
[[ -f "$base/.extracted" ]]
if systemctl is-active --quiet "$unit"; then
    printf 'Reference service is already active.\n'
    exit 0
fi

install -d -m 700 "$data/config" "$data/private" "$base/runtime"
if [[ ! -f "$data/config/system.xml" ]]; then
    cat > "$data/config/system.xml" <<'XML'
<?xml version="1.0" encoding="utf-8"?>
<ServerConfiguration xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema">
  <HttpServerPortNumber>18097</HttpServerPortNumber>
  <PublicPort>18097</PublicPort>
  <HttpsPortNumber>18497</HttpsPortNumber>
  <PublicHttpsPort>18497</PublicHttpsPort>
  <EnableHttps>false</EnableHttps>
  <EnableUPnP>false</EnableUPnP>
  <EnableRemoteAccess>false</EnableRemoteAccess>
  <EnableAutoUpdate>false</EnableAutoUpdate>
  <EnableAutomaticRestart>false</EnableAutomaticRestart>
  <AutoRunWebApp>false</AutoRunWebApp>
  <IsStartupWizardCompleted>false</IsStartupWizardCompleted>
  <EnableExternalContentInSuggestions>false</EnableExternalContentInSuggestions>
  <ServerName>Goby Emby Reference</ServerName>
  <LocalNetworkAddresses><string>127.0.0.1</string></LocalNetworkAddresses>
  <PreferredMetadataLanguage>en</PreferredMetadataLanguage>
  <MetadataCountryCode>US</MetadataCountryCode>
  <UICulture>en-US</UICulture>
  <DatabaseCacheSizeMB>64</DatabaseCacheSizeMB>
  <LogFileRetentionDays>2</LogFileRetentionDays>
</ServerConfiguration>
XML
fi

cat > "$base/runtime/launch.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
APP_DIR=/dev/shm/goby-emby-reference/package/opt/emby-server
EMBY_DATA=/opt/goby-test/emby-reference-data
export EMBY_DATA
export AMDGPU_IDS="$APP_DIR/extra/share/libdrm/amdgpu.ids"
export FONTCONFIG_PATH="$APP_DIR/etc/fonts"
export LD_LIBRARY_PATH="$APP_DIR/lib:$APP_DIR/extra/lib"
export LIBVA_DRIVERS_PATH="$APP_DIR/extra/lib/dri"
export OCL_ICD_VENDORS="$APP_DIR/extra/etc/OpenCL/vendors"
export PATH="$APP_DIR/bin:$PATH"
export PCI_IDS_PATH="$APP_DIR/share/hwdata/pci.ids"
export SSL_CERT_FILE="$APP_DIR/etc/ssl/certs/ca-certificates.crt"
export XDG_CACHE_HOME="$EMBY_DATA/cache"
export NEOReadDebugKeys=1
export OverrideGpuAddressSpace=48
cd "$APP_DIR"
exec "$APP_DIR/system/EmbyServer" \
    -programdata "$EMBY_DATA" \
    -ffdetect "$APP_DIR/bin/ffdetect" \
    -ffmpeg "$APP_DIR/bin/ffmpeg" \
    -ffprobe "$APP_DIR/bin/ffprobe" \
    -restartexitcode 3 \
    -updatepackage 'emby-server-deb_{version}_amd64.deb'
SH
chmod 700 "$base/runtime/launch.sh"

systemd-run --unit="$unit" --collect \
    --property=Description='Goby isolated official Emby reference' \
    --property=PrivateNetwork=yes \
    --property=PrivateTmp=yes \
    --property=NoNewPrivileges=yes \
    --property=ProtectSystem=strict \
    --property=ProtectHome=yes \
    --property=ReadOnlyPaths=/opt/goby-fixtures \
    --property="ReadWritePaths=$data $base/runtime" \
    --property=CPUQuota=150% \
    --property=MemoryMax=1G \
    --property=TasksMax=256 \
    --property=LimitNOFILE=65536 \
    --property=TimeoutStopSec=25 \
    --property=KillMode=control-group \
    --property=WorkingDirectory="$data" \
    "$base/runtime/launch.sh"

systemctl show "$unit" -p ActiveState -p SubState -p MainPID -p PrivateNetwork
printf '\nNo reference listener may be present on the host network:\n'
ss -ltnp
