# Internal Linux amd64 recipe. Building or starting it is not runtime acceptance.
# This is the observed trixie-slim amd64 manifest, not a mutable release tag.
FROM debian@sha256:abc9cb88a5587630d7f915f47b23b0668fe250fbfc6457aa4d52b534c1bbf73f AS base
RUN rm -f /etc/apt/sources.list /etc/apt/sources.list.d/* \
    && printf '%s\n' \
      'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/20260915T000000Z trixie main' \
      'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/20260915T000000Z trixie-updates main' \
      'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian-security/20260915T000000Z trixie-security main' \
      > /etc/apt/sources.list

FROM base AS artifact
ARG TARGETARCH
ARG GOBY_BINARY_SHA256
ARG GOBY_BUILD_MANIFEST_SHA256
ARG SOURCE_DATE_EPOCH=1789430400
COPY goby manifest.json /payload/
COPY check-artifact.py /build/check-artifact.py
# Reject missing/mismatched input pins before installing build dependencies.
RUN set -eu; test "$TARGETARCH" = amd64; \
    for pin in "$GOBY_BINARY_SHA256" "$GOBY_BUILD_MANIFEST_SHA256"; do \
      test "${#pin}" -eq 64; case "$pin" in *[!0-9a-f]*) exit 1 ;; esac; \
    done; \
    printf '%s  /payload/goby\n%s  /payload/manifest.json\n' "$GOBY_BINARY_SHA256" "$GOBY_BUILD_MANIFEST_SHA256" | sha256sum --check --strict -
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates python3 \
    && python3 /build/check-artifact.py /payload "$GOBY_BINARY_SHA256" "$GOBY_BUILD_MANIFEST_SHA256" \
    && chmod 0755 /payload/goby \
    && chmod 0644 /payload/manifest.json /payload/oci-artifact-binding.json \
    && printf 'validated build inputs\n' > /artifact-inputs.valid \
    && chmod 0444 /artifact-inputs.valid \
    && touch -d @1789430400 /artifact-inputs.valid \
    && rm -rf /var/lib/apt/lists/*

FROM base AS media-build
ARG FFMPEG_BUILD_JOBS=2
ENV SOURCE_DATE_EPOCH=1789430400
# This fixed-content dependency gates expensive stages on the complete artifact
# check without putting changing Goby bytes into the FFmpeg filesystem layer.
# It is a build dependency, not a standalone verification receipt.
COPY --from=artifact /artifact-inputs.valid /build/artifact-inputs.valid
COPY build-ffmpeg.sh /build/build-ffmpeg.sh
COPY toolchain-patches/ /build/toolchain-patches/
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      build-essential ca-certificates curl gnupg libass-dev libdrm-dev libmp3lame-dev \
      libnuma-dev libopus-dev libssl-dev libva-dev libvorbis-dev libvpl-dev \
      libx264-dev nasm patch pkg-config python3 xz-utils yasm \
    && GOBY_FFMPEG_BUILD_JOBS="$FFMPEG_BUILD_JOBS" bash /build/build-ffmpeg.sh

FROM base AS runtime
ARG SOURCE_DATE_EPOCH=1789430400
COPY --from=artifact /artifact-inputs.valid /usr/share/goby/artifact-inputs.valid
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates fonts-dejavu-core passwd postgresql-client-17 \
      libass9 libbz2-1.0 libdrm2 liblzma5 libmp3lame0 libnuma1 libopus0 \
      libstdc++6 libva2 libva-drm2 libva-x11-2 libvorbis0a libvorbisenc2 \
      libvpl2 libx264-164 zlib1g \
    && groupadd --gid 10001 goby \
    && useradd --uid 10001 --gid 10001 --home-dir /var/lib/goby --no-create-home --shell /usr/sbin/nologin goby \
    && install -d -m 0700 -o 10001 -g 10001 /var/lib/goby /var/cache/goby /var/log/goby \
    && install -d -m 0755 /media /usr/share/goby/toolchain \
    && dpkg-query -W -f='${binary:Package}\t${Version}\t${Architecture}\n' > /usr/share/goby/runtime-packages.tsv \
    && rm -rf /var/lib/apt/lists/*
COPY --from=artifact /payload/goby /usr/local/bin/goby
COPY --from=artifact /payload/manifest.json /usr/share/goby/build-manifest.json
COPY --from=artifact /payload/oci-artifact-binding.json /usr/share/goby/oci-artifact-binding.json
COPY --from=media-build /opt/ffmpeg/9.0.1/ /opt/ffmpeg/9.0.1/
COPY --from=media-build /opt/goby-toolchain-evidence/ /usr/share/goby/toolchain/
COPY entrypoint.sh /usr/local/bin/goby-entrypoint
COPY Dockerfile .dockerignore build-ffmpeg.sh check-artifact.py entrypoint.sh source-pins.json /usr/share/goby/recipe/
RUN chmod 0755 /usr/local/bin/goby-entrypoint \
    && /opt/ffmpeg/9.0.1/bin/ffmpeg -version > /usr/share/goby/toolchain/runtime-ffmpeg-version.txt \
    && grep -Eq '^ffmpeg version 9\.0\.1([ -]|$)' /usr/share/goby/toolchain/runtime-ffmpeg-version.txt \
    && /opt/ffmpeg/9.0.1/bin/ffprobe -version > /usr/share/goby/toolchain/runtime-ffprobe-version.txt \
    && grep -Eq '^ffprobe version 9\.0\.1([ -]|$)' /usr/share/goby/toolchain/runtime-ffprobe-version.txt \
    && /usr/lib/postgresql/17/bin/pg_dump --version > /usr/share/goby/toolchain/pg-dump-version.txt \
    && grep -Eq '^pg_dump \(PostgreSQL\) 17\.' /usr/share/goby/toolchain/pg-dump-version.txt \
    && /usr/lib/postgresql/17/bin/pg_restore --version > /usr/share/goby/toolchain/pg-restore-version.txt \
    && grep -Eq '^pg_restore \(PostgreSQL\) 17\.' /usr/share/goby/toolchain/pg-restore-version.txt \
    && sha256sum /usr/local/bin/goby /opt/ffmpeg/9.0.1/bin/ffmpeg /opt/ffmpeg/9.0.1/bin/ffprobe \
      /usr/lib/postgresql/17/bin/pg_dump /usr/lib/postgresql/17/bin/pg_restore > /usr/share/goby/runtime-executables.sha256
ENV GOBY_FFMPEG=/opt/ffmpeg/9.0.1/bin/ffmpeg \
    GOBY_FFPROBE=/opt/ffmpeg/9.0.1/bin/ffprobe \
    GOBY_PG_DUMP=/usr/lib/postgresql/17/bin/pg_dump \
    GOBY_PG_RESTORE=/usr/lib/postgresql/17/bin/pg_restore \
    GOBY_LISTEN=:8096 \
    GOBY_API_KEY_MASTER_KEY_FILE=/var/lib/goby/application-key-master.key \
    GOBY_TRANSCODE_CACHE=/var/cache/goby/transcodes \
    GOBY_LOG_DIR=/var/log/goby \
    XDG_CACHE_HOME=/var/cache/goby
LABEL org.opencontainers.image.title="Goby internal candidate" \
    org.opencontainers.image.source="https://github.com/moooyo/goby"
WORKDIR /var/lib/goby
USER 10001:10001
EXPOSE 8096/tcp
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/goby-entrypoint"]
