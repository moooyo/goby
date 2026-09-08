#!/usr/bin/env bash
# Create an isolated PostgreSQL role/database and a private test environment file.
set -euo pipefail

[[ $(id -u) == 0 ]] || { echo 'Run this script as root.' >&2; exit 1; }
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install --no-install-recommends -y postgresql python3
systemctl start postgresql
install -d -m 700 /opt/goby-test

if [[ -e /opt/goby-test/test.env ]]; then
  echo 'Existing /opt/goby-test/test.env retained; credentials were not rotated.'
  exit 0
fi

if runuser -u postgres -- psql -Atqc \
  "SELECT 1 FROM pg_roles WHERE rolname = 'goby_test'" | grep -q 1; then
  echo 'Role goby_test already exists without the managed environment file; refusing to overwrite it.' >&2
  exit 1
fi
if runuser -u postgres -- psql -Atqc \
  "SELECT 1 FROM pg_database WHERE datname = 'goby_test'" | grep -q 1; then
  echo 'Database goby_test already exists without the managed environment file; refusing to overwrite it.' >&2
  exit 1
fi

umask 077
password=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
runuser -u postgres -- psql -v ON_ERROR_STOP=1 <<SQL
CREATE ROLE goby_test LOGIN PASSWORD '$password';
CREATE DATABASE goby_test OWNER goby_test;
SQL
cat > /opt/goby-test/test.env <<EOF
GOBY_DATABASE_URL='postgresql://goby_test:$password@127.0.0.1:5432/goby_test?sslmode=disable'
GOBY_TEST_DATABASE_URL='postgresql://goby_test:$password@127.0.0.1:5432/goby_test?sslmode=disable'
DATABASE_URL='postgresql://goby_test:$password@127.0.0.1:5432/goby_test?sslmode=disable'
GOBY_FFMPEG='/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg'
GOBY_FFPROBE='/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffprobe'
GOBY_FFMPEG_PATH='/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg'
GOBY_FFPROBE_PATH='/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffprobe'
PATH='/opt/goby-toolchains/go1.27.1/bin:/opt/goby-toolchains/ffmpeg-9.0.1/bin:/opt/node22/bin:/usr/local/bin:/usr/bin:/bin'
EOF
chmod 600 /opt/goby-test/test.env
unset password
echo 'Created dedicated goby_test PostgreSQL role and database; credentials are in /opt/goby-test/test.env (0600).'
