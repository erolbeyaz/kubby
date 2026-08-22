# syntax=docker/dockerfile:1.7

# Base image sources are parameterised so anyone can build through their own mirror
# (ADR-027). Defaults point at upstream:
#   docker build --build-arg REGISTRY=my-registry.local -t kubby .
ARG REGISTRY=docker.io
ARG RUNTIME_REGISTRY=gcr.io

# Base images are pinned by digest (ADR-025). Tags are kept alongside for readability.
ARG NODE_IMAGE=${REGISTRY}/library/node:24.19.0-trixie@sha256:66bb8d36ae1ddd72199ed235a089904874ca4079ee517936ca3adb80506a75c1
ARG GO_IMAGE=${REGISTRY}/library/golang:1.27.0-trixie@sha256:6212da3924947f4b6a939df02ea627c13f338f1a41d6c3fcb0dd9d076eef46c4
ARG RUNTIME_IMAGE=${RUNTIME_REGISTRY}/distroless/static:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7

# ---------------------------------------------------------------- frontend
FROM ${NODE_IMAGE} AS web

WORKDIR /build
COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

# ---------------------------------------------------------------- backend
FROM ${GO_IMAGE} AS server

WORKDIR /build
COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./
COPY --from=web /build/dist/ ./internal/webassets/dist/

ARG VERSION=dev
ARG COMMIT_SHA=unknown
ARG BUILD_DATE=unknown
ARG PKG=github.com/erolbeyaz/kubby/internal/httpapi

RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X '${PKG}.Version=${VERSION}' \
      -X '${PKG}.CommitSHA=${COMMIT_SHA}' \
      -X '${PKG}.BuildDate=${BUILD_DATE}'" \
    -o /kubby ./cmd/kubby

# ---------------------------------------------------------------- runtime
FROM ${RUNTIME_IMAGE}

# distroless/static:nonroot runs as uid 65532 with no shell and no package manager.
COPY --from=server /kubby /kubby

EXPOSE 8080
USER 65532:65532

ENTRYPOINT ["/kubby"]
