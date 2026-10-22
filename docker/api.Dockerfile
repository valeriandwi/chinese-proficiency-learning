# syntax=docker/dockerfile:1
#
# The Go side of hanpath: one static image carrying the three binaries.
#
# Three details that are not optional:
#   • GOTOOLCHAIN=local — go.mod pins a toolchain, and without this a builder with a
#     newer Go would download a different one and the build would diverge from the
#     machine the tests ran on.
#   • CGO_ENABLED=0 — pgx and x/crypto are pure Go, so the binaries are static and the
#     runtime image needs no libc.
#   • tzdata in the runtime image — streaks and daily goals are bucketed by the
#     learner's IANA timezone, and Go needs /usr/share/zoneinfo to resolve it.

FROM golang:1.25.1-alpine AS build

ENV GOTOOLCHAIN=local \
    CGO_ENABLED=0

WORKDIR /src

# The module graph first, so a source-only change does not refetch dependencies.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker \
 && go build -trimpath -ldflags="-s -w" -o /out/contentctl ./cmd/contentctl


FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget \
 && adduser -D -u 10001 -h /app hanpath

WORKDIR /app

COPY --from=build /out/ /usr/local/bin/

# contentctl reads the authored YAML by path; the API embeds its migrations but not the
# curriculum, so the content is copied rather than assumed.
COPY --chown=hanpath:hanpath content/ /app/content/

# Media (reference audio) is a volume, not an image layer: it is 17 MB of committed
# files under var/media and a deployment may point at object storage instead.
RUN mkdir -p /data/media && chown -R hanpath:hanpath /data

USER hanpath

EXPOSE 8081

# `docker compose` overrides the entrypoint to run migrations, the worker, or contentctl.
ENTRYPOINT ["/usr/local/bin/api"]
