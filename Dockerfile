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

# The cluster terminal runs these two and nothing else (ADR-094), so they are part of the
# product rather than something an operator is expected to bolt on. Both are static Go
# binaries, which is what lets them run in a distroless image with no libc.
#
# Sources are parameterised like the base images (ADR-027): a site that may only fetch
# from its own mirror overrides the URL, and the checksum is what makes that safe.
ARG KUBECTL_VERSION=v1.36.4
ARG HELM_VERSION=v4.2.3
ARG KUBECTL_URL=https://dl.k8s.io/release
ARG HELM_URL=https://get.helm.sh

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

# ---------------------------------------------------------------- cluster tools
FROM ${GO_IMAGE} AS tools

ARG KUBECTL_VERSION
ARG HELM_VERSION
ARG KUBECTL_URL
ARG HELM_URL
# Supplied by buildx. Defaulted so a plain `docker build` still works.
ARG TARGETARCH=amd64

# Checksums are pinned per architecture and verified before anything is unpacked. A
# downloaded binary that is never checked is a supply-chain hole, and this one ends up
# holding cluster credentials.
ARG KUBECTL_SHA256_amd64=8b8f088da2dab964f853b38464033b1be15ede2839eca751482357c45abdd05a
ARG KUBECTL_SHA256_arm64=0ecf44450ee6063bf19dd166a103ee6df4a9034455c2abce626e6eea657d73fb
ARG HELM_SHA256_amd64=e9b88b4ee95b18c706839c28d3a0220e5bc470e9cd9262410c90793c45ff8b7c
ARG HELM_SHA256_arm64=21abd9354d39b2cd79a8d76be6912cd137a983cbf997193503fb8a6a6e2f2785

WORKDIR /tools

# The last two lines run each binary before it is copied into an image that has no shell
# to try it in. The comment sits outside the RUN deliberately: builders disagree about
# whether a comment inside a line continuation ends the command or is stripped from it,
# and one of those readings silently skips everything after it.
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) kubectl_sha="${KUBECTL_SHA256_amd64}"; helm_sha="${HELM_SHA256_amd64}" ;; \
      arm64) kubectl_sha="${KUBECTL_SHA256_arm64}"; helm_sha="${HELM_SHA256_arm64}" ;; \
      *) echo "no pinned checksum for ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    \
    curl -fsSL -o kubectl "${KUBECTL_URL}/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl"; \
    echo "${kubectl_sha}  kubectl" | sha256sum -c -; \
    chmod 0755 kubectl; \
    \
    curl -fsSL -o helm.tgz "${HELM_URL}/helm-${HELM_VERSION}-linux-${TARGETARCH}.tar.gz"; \
    echo "${helm_sha}  helm.tgz" | sha256sum -c -; \
    tar -xzf helm.tgz --strip-components=1 "linux-${TARGETARCH}/helm"; \
    chmod 0755 helm; \
    rm helm.tgz; \
    \
    ./kubectl version --client=true >/dev/null; \
    ./helm version --short >/dev/null

# ---------------------------------------------------------------- runtime
FROM ${RUNTIME_IMAGE}

# distroless/static:nonroot runs as uid 65532 with no shell and no package manager.
COPY --from=server /kubby /kubby

# The terminal's two tools. On PATH, because that is where the session looks for them.
COPY --from=tools /tools/kubectl /usr/local/bin/kubectl
COPY --from=tools /tools/helm /usr/local/bin/helm

EXPOSE 8080
USER 65532:65532

ENTRYPOINT ["/kubby"]
