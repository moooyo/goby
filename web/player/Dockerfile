# Build the player separately from the Go application and administrator assets.
# Release tooling can supply digest-pinned images through these build arguments.
ARG NODE_IMAGE=node:24-alpine
ARG NGINX_IMAGE=nginxinc/nginx-unprivileged:1.29-alpine
ARG SOURCE_DATE_EPOCH=0

FROM ${NODE_IMAGE} AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN GOMAXPROCS=2 npm run build && node build-notices.mjs

FROM ${NGINX_IMAGE} AS runtime
ARG GOBY_PLAYER_REVISION=local
ARG GOBY_PLAYER_SOURCE_SHA256=unrecorded
LABEL org.opencontainers.image.title="Goby Player" \
    org.opencontainers.image.revision=${GOBY_PLAYER_REVISION} \
    io.goby.player.source.sha256=${GOBY_PLAYER_SOURCE_SHA256}
ENV GOBY_API_UPSTREAM=http://goby:8096 \
    NGINX_ENVSUBST_FILTER="^GOBY_API_UPSTREAM$" \
    NGINX_ENVSUBST_OUTPUT_DIR=/tmp
COPY nginx.conf /etc/nginx/templates/nginx.conf.template
COPY --from=build /app/dist /usr/share/nginx/html
COPY --from=build /app/notices /usr/share/doc/goby-player
USER 101:101
EXPOSE 8080
STOPSIGNAL SIGQUIT
CMD ["nginx", "-c", "/tmp/nginx.conf", "-g", "daemon off;"]
