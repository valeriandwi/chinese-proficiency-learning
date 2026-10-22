# syntax=docker/dockerfile:1
#
# The SPA and the edge, in one image: Caddy serves the build and proxies /api, /media and
# /healthz to the API.
#
# One container rather than nginx plus a separate TLS proxy, because Caddy obtains and
# renews the certificate itself — no certbot service, no webroot volume, and none of the
# ordering trouble where a proxy refuses to start before a certificate exists.
#
# Why the SPA and the API must share this origin at all: the session cookie is issued for
# the SPA's host, so a second hostname for the API would make it third-party and browsers
# would drop it silently.

FROM node:24-alpine AS build

# The repo layout is reproduced inside the image: web/scripts/copy-hanzi-data.mjs walks
# `../content` to find the authored characters, so `content/` has to be a sibling of the
# web directory here too. Without it the script reports "No `character:` fields found"
# and the build stops.
WORKDIR /src/web

RUN corepack enable

# pnpm 11 reads build-script approvals from pnpm-workspace.yaml, so it has to be present
# before install, not just the lockfile.
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY web/ ./
COPY content/ /src/content/

# Stroke-order data for the authored characters (web/public/hanzi) is committed, but the
# copy step is cheap, idempotent, and fails loudly if the content uses a character with no
# stroke data. The build then type-checks (tsc -b) and bundles.
RUN pnpm run hanzi-data && pnpm build


FROM caddy:2-alpine

# Caddy reads /etc/caddy/Caddyfile by default and keeps certificates in /data, which the
# compose file mounts as a volume so a rebuild does not re-request them.
COPY docker/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /src/web/dist /srv

# SITE_ADDRESS decides the mode: http://localhost:8080 for a local run, a bare hostname for
# automatic HTTPS. API_UPSTREAM is the API on the compose network.
ENV SITE_ADDRESS=http://localhost:8080
ENV API_UPSTREAM=api:8081

EXPOSE 8080 80 443
