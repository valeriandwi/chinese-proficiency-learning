# syntax=docker/dockerfile:1
#
# The SPA, built once and served by nginx — which is also the reverse proxy.
#
# That second job is the important one: the SPA and the API must share an origin, because
# the session cookie is issued for the SPA's host. nginx serves the static build and
# proxies /api, /media and /healthz to the API container, so the browser only ever sees
# one origin and the cookie is never third-party.

FROM node:24-alpine AS build

# The repo layout is reproduced inside the image: web/scripts/copy-hanzi-data.mjs walks
# `../content` to find the authored characters, so `content/` has to be a sibling of the
# web directory here too. Without it the script reports "No `character:` fields found"
# and the build stops — which is what happened the first time.
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


FROM nginx:1.27-alpine

COPY docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/web/dist /usr/share/nginx/html

EXPOSE 80
