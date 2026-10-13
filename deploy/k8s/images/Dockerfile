# syntax=docker/dockerfile:1
# 从后端仓库根目录构建，保留 /workspace 源码路径供 runtime.Caller 定位资源。
FROM --platform=$BUILDPLATFORM golang:1.23.12-bookworm AS builder
ARG SERVICE
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /workspace
COPY util ./util
COPY vw_user ./vw_user
COPY vw_video ./vw_video
COPY vw_gateway ./vw_gateway
RUN case "$SERVICE" in vw_user|vw_video|vw_gateway) ;; *) exit 1 ;; esac \
    && cd "/workspace/$SERVICE" \
    && CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -mod=readonly -o /out/service "./cmd/$SERVICE"

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates netbase ffmpeg \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1000 videoweb \
    && useradd --uid 1000 --gid 1000 --no-create-home videoweb \
    && mkdir -p /workspace/resources /app /seed \
    && chown -R 1000:1000 /workspace/resources /app /seed
COPY --from=builder /out/service /app/service
COPY resources/default/avatar.jpg /seed/avatar.jpg
WORKDIR /app
USER 1000:1000
ENTRYPOINT ["/app/service"]
CMD ["-conf", "/etc/videoweb/config.yaml"]
