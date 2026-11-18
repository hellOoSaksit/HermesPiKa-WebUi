# SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
#
# Hermes Agent + hpk in one container (docs/docker.md): the official Hermes image,
# with hpk installed into its Hermes home and HPK Console as a supervised service
# next to the Hermes gateway.
#
#   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/docker/hpk-linux-amd64 ./cmd/hpk
#   docker build -f docker/hermes/Dockerfile -t hpk-hermes:local .
ARG HERMES_IMAGE=nousresearch/hermes-agent:v0.23.1
FROM ${HERMES_IMAGE}

ARG TARGETARCH=amd64
COPY --chmod=0755 dist/docker/hpk-linux-${TARGETARCH} /usr/local/bin/hpk
COPY --chmod=0644 tools/hermes-consent.py /opt/hpk/hermes-consent.py
# First-start setup: hpk into Hermes' config.yaml (hooks + skills), hooks allowed,
# team mode from HPK_OWNER. Runs after Hermes' own setup (01–02).
COPY --chmod=0755 docker/hermes/03-hpk /etc/cont-init.d/03-hpk
# HPK Console as an s6 service, as the hermes user.
COPY --chmod=0755 docker/hermes/hpk-run /etc/s6-overlay/s6-rc.d/hpk/run
COPY --chmod=0755 docker/hermes/hpk-finish /etc/s6-overlay/s6-rc.d/hpk/finish
RUN chmod 0755 /opt/hpk \
 && echo longrun > /etc/s6-overlay/s6-rc.d/hpk/type \
 && mkdir -p /etc/s6-overlay/s6-rc.d/hpk/dependencies.d \
 && touch /etc/s6-overlay/s6-rc.d/hpk/dependencies.d/base /etc/s6-overlay/s6-rc.d/user/contents.d/hpk

ENV HPK_ADDR=0.0.0.0:7878
EXPOSE 7878
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 \
  CMD curl -fsk -o /dev/null "http$([ -n "${HPK_TLS_CERT:-}" ] && echo s)://127.0.0.1:${HPK_ADDR##*:}/api/health"

# The Hermes gateway (messaging, cron); HPK Console runs beside it.
CMD ["gateway", "run"]
