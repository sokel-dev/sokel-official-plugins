# syntax=docker/dockerfile:1
# One image recipe for every plugin in this repository; PLUGIN selects which.
#
#   docker build --build-arg PLUGIN=telegram-bot -t sokel-plugin-telegram-bot .
#
# The plugin SDK is open source (github.com/sokel-dev/sokel-plugin-sdk) and comes from the Go module proxy.
FROM --platform=$BUILDPLATFORM golang:1.26.8 AS build
ARG PLUGIN
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY ${PLUGIN}/ ${PLUGIN}/
WORKDIR /src/${PLUGIN}
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /out/plugin .

FROM alpine:3.24
ARG VERSION=
ENV SOKEL_VERSION=${VERSION}
RUN apk add --no-cache ca-certificates tzdata su-exec && mkdir -p /var/lib/sokel
# Runs as a non-root user; drop-root fixes ownership of the state directory once for volumes created by older images.
COPY docker/drop-root.sh /usr/local/bin/drop-root
ENV SOKEL_OWN_DIRS=/var/lib/sokel
WORKDIR /var/lib/sokel
COPY --from=build /out/plugin /plugin
ENTRYPOINT ["/usr/local/bin/drop-root", "/plugin"]
