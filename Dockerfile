# SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
#
# HPK Console as a team server (docs/docker.md). The image holds hpk only; it
# packages a binary built outside (goreleaser, or `GOOS=linux go build`), so the
# image never needs a Go toolchain.
#
#   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/docker/hpk-linux-amd64 ./cmd/hpk
#   docker build -t hpk .
#
# Model work needs Hermes Agent next to hpk (same Hermes home, hermes and
# hermes-acp on PATH); see docs/docker.md for that setup.
FROM alpine:3.22

ARG TARGETARCH=amd64
# git: project wikis and the board keep their history in git.
RUN apk add --no-cache git ca-certificates tzdata \
 && addgroup -S hpk && adduser -S -G hpk -h /data -s /sbin/nologin hpk \
 && mkdir -p /data && chown hpk:hpk /data

COPY --chmod=0755 dist/docker/hpk-linux-${TARGETARCH} /usr/local/bin/hpk
COPY --chmod=0755 docker/entrypoint.sh /usr/local/bin/hpk-entrypoint

ENV HPK_HOME=/data \
    HPK_ADDR=0.0.0.0:7878
VOLUME ["/data"]
EXPOSE 7878
USER hpk
WORKDIR /data

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD hpk-entrypoint health

ENTRYPOINT ["hpk-entrypoint"]
CMD ["serve"]
