# Firstbid — Dockerfile
#
# Builds the two Go binaries that make up the running system:
#   dashboard  - read-only UI + API (embeds the Next.js static export, no
#                Node/JS runtime needed to serve it)
#   firstbid   - the quoting engine (defaults to DRY RUN; -live requires
#                FIRSTBID_PRIVATE_KEY, which is never baked into the image)
#
# executor/ is excluded on purpose: per its own package.json description it is
# a TypeScript reference used to cross-check the Go engine and "not part of
# the running system".

# ---------------------------------------------------------------------------
# Stage 1: build the Next.js static export and embed it into core/cmd/dashboard
# ---------------------------------------------------------------------------
FROM node:22-alpine AS web

WORKDIR /repo/web
COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
# `npm run embed` = `next build` (static export, see next.config.ts) followed
# by scripts/embed.mjs, which copies ./out into ../core/cmd/dashboard/dist.
RUN npm run embed

# ---------------------------------------------------------------------------
# Stage 2: compile the Go binaries
# ---------------------------------------------------------------------------
FROM golang:1.25-alpine AS gobuild

WORKDIR /repo/core
COPY core/go.mod core/go.sum ./
RUN go mod download

COPY core/ ./
# Overwrite whatever dist/ was committed with the freshly built export from
# stage 1, so the image always serves what was just compiled from source.
COPY --from=web /repo/core/cmd/dashboard/dist ./cmd/dashboard/dist

# modernc.org/sqlite is a pure-Go driver (no cgo), so a static binary is just
# CGO_ENABLED=0 — no gcc/musl-dev needed in this stage.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/dashboard ./cmd/dashboard
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/firstbid ./cmd/firstbid

# distroless has no shell/mkdir, so the volume mount point is created here,
# owned by the nonroot uid the runtime stage runs as, and copied over.
RUN mkdir -p /data && chown 65532:65532 /data

# ---------------------------------------------------------------------------
# Stage 3: minimal runtime image
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

WORKDIR /app
COPY --from=gobuild /out/dashboard /out/firstbid ./
COPY docs/calibration.json ./docs/calibration.json
COPY --from=gobuild --chown=65532:65532 /data /data

# Ledger SQLite file lives here so it survives restarts when /data is a
# mounted volume, and so `dashboard` and `firstbid` can share one P&L ledger.
VOLUME ["/data"]

EXPOSE 8080

# distroless/nonroot already runs as uid/gid 65532; no extra user setup needed.
ENTRYPOINT ["/app/dashboard"]
CMD ["-addr=:8080", "-calibration=/app/docs/calibration.json", "-db=/data/firstbid.db"]
