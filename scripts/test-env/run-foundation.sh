#!/usr/bin/env bash
set -euo pipefail

# Run only on the dedicated Linux test host. Preserve unrelated services and data.
repository=/opt/goby-test/repository
deployment=/opt/goby-dev
marker=goby-foundation-test-deployment
if [[ -d "$deployment" && ! -f "$deployment/.goby-managed" ]]; then
  echo "Refusing to replace an unmanaged deployment directory." >&2
  exit 1
fi
if [[ -f "$deployment/.goby-managed" && "$(cat "$deployment/.goby-managed")" != "$marker" ]]; then
  echo "Deployment ownership marker does not match." >&2
  exit 1
fi
if ! id goby >/dev/null 2>&1; then
  useradd --system --user-group --no-create-home --shell /usr/sbin/nologin goby
fi
install -d -m 755 "$deployment" "$deployment/admin"
printf '%s\n' "$marker" > "$deployment/.goby-managed"
install -d -m 750 -o goby -g goby /var/lib/goby-test
set -a
source /opt/goby-test/test.env
set +a
export GOCACHE=/dev/shm/goby-go-cache GOMODCACHE=/dev/shm/goby-go-mod
cd "$repository"
go build -trimpath -o /opt/goby-test/goby.next ./cmd/goby
if [[ ! -f web/admin/dist/index.html ]]; then
  echo "The built administrator assets are missing." >&2
  exit 1
fi
if [[ ! -f /opt/goby-test/runtime.env ]]; then
  umask 077
  cp /opt/goby-test/test.env /opt/goby-test/runtime.env
  {
    printf 'GOBY_LISTEN=127.0.0.1:18096\n'
    printf 'GOBY_PUBLIC_URL=http://127.0.0.1:18096\n'
    printf 'GOBY_COOKIE_SECURE=false\n'
    printf 'GOBY_WEB_DIR=/opt/goby-dev/admin\n'
    printf 'GOBY_SETUP_TOKEN=%s\n' "$(openssl rand -hex 32)"
  } >> /opt/goby-test/runtime.env
fi
if [[ ! -f /opt/goby-test/browser.env ]]; then
  umask 077
  {
    printf 'GOBY_SMOKE_NAME=goby-admin-test\n'
    printf 'GOBY_SMOKE_PASSWORD=%s\n' "$(openssl rand -hex 24)"
  } > /opt/goby-test/browser.env
fi
if systemctl is-active --quiet goby-foundation-test.service; then
  systemctl stop goby-foundation-test.service
fi
install -m 755 /opt/goby-test/goby.next "$deployment/goby"
cp -a web/admin/dist/. "$deployment/admin/"
chmod -R a+rX "$deployment/admin"
cat > /etc/systemd/system/goby-foundation-test.service <<'UNIT'
[Unit]
Description=Goby foundation test deployment
After=network-online.target postgresql.service

[Service]
Type=simple
User=goby
Group=goby
EnvironmentFile=/opt/goby-test/runtime.env
ExecStart=/opt/goby-dev/goby
WorkingDirectory=/var/lib/goby-test
Restart=on-failure
RestartSec=3s
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
CapabilityBoundingSet=
UMask=0077
UNIT
systemctl daemon-reload
systemctl start goby-foundation-test.service
for attempt in $(seq 1 30); do
  if curl --fail --silent http://127.0.0.1:18096/readyz >/dev/null; then
    echo "Goby foundation service is ready on 127.0.0.1:18096 as user goby."
    exit 0
  fi
  sleep 1
done
echo "Goby did not become ready; inspect its journal on the test host." >&2
exit 1
