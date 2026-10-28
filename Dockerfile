# syntax=docker/dockerfile:1
# Multi-stage, multi-arch build: the UI is built once on the build platform,
# the Go binary is cross-compiled for each target, and the result runs on a
# distroless base as a non-root user.

FROM --platform=$BUILDPLATFORM node:26-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
COPY docs/ /src/docs/
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH TARGETVARIANT
ARG VERSION=dev COMMIT=none DATE=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN GOARM=${TARGETVARIANT#v} CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w \
      -X github.com/hurseyc/guardtower/internal/version.Version=${VERSION} \
      -X github.com/hurseyc/guardtower/internal/version.Commit=${COMMIT} \
      -X github.com/hurseyc/guardtower/internal/version.Date=${DATE}" \
    -o /out/guardtower ./cmd/guardtower
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.title="GuardTower" \
      org.opencontainers.image.description="Fleet manager for AdGuard Home" \
      org.opencontainers.image.source="https://github.com/hurseyc/GuardTower" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/guardtower /usr/local/bin/guardtower
COPY --from=build --chown=65532:65532 /out/data /data
ENV GT_DATA_DIR=/data GT_LISTEN=:8080
VOLUME ["/data"]
EXPOSE 8080
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/usr/local/bin/guardtower", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/guardtower"]
CMD ["serve"]
