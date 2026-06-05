FROM registry.suse.com/bci/golang:1.25.7 AS builder
ARG MK_HOST_ARCH
ENV ARCH=$MK_HOST_ARCH

RUN zypper -n rm container-suseconnect && \
    zypper -n install git curl docker gzip tar wget awk docker-buildx

COPY --from=golangci/golangci-lint:v2.11.4-alpine@sha256:72bcd68512b4e27540dd3a778a1b7afd45759d8145cfb3c089f1d7af53e718e9 \
    /usr/bin/golangci-lint /usr/local/bin/golangci-lint

RUN go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.18.0

RUN go install k8s.io/code-generator/cmd/openapi-gen@v0.29.13

ENV HOME=/go/src/github.com/harvester/node-disk-manager

# ---- base ----
FROM builder AS base
WORKDIR /go/src/github.com/harvester/node-disk-manager
COPY . .

# ---- build ----
FROM base AS build
ARG MK_REPO_ID

RUN --mount=type=cache,target=/go/pkg/mod,id=harvester-ndm-go-mod-${MK_REPO_ID} \
    --mount=type=cache,target=/go/src/github.com/harvester/node-disk-manager/.cache/go-build,id=harvester-ndm-go-build-${MK_REPO_ID} \
    ./scripts/build

FROM scratch AS build-output
COPY --from=build /go/src/github.com/harvester/node-disk-manager/bin/ /bin/

# ---- validate ----
FROM base AS validate
ARG MK_REPO_ID

RUN --mount=type=cache,target=/go/pkg/mod,id=harvester-ndm-go-mod-${MK_REPO_ID} \
    --mount=type=cache,target=/go/src/github.com/harvester/node-disk-manager/.cache/go-build,id=harvester-ndm-go-build-${MK_REPO_ID} \
    ./scripts/validate

# ---- validate-ci ----
FROM base AS validate-ci
ARG MK_REPO_ID

RUN git config --global user.email "ci@example.com" && \
    git config --global user.name "ci" && \
    git init 2>/dev/null && git add . && git commit -q -m "commit for validate-ci"

RUN --mount=type=cache,target=/go/pkg/mod,id=harvester-ndm-go-mod-${MK_REPO_ID} \
    --mount=type=cache,target=/go/src/github.com/harvester/node-disk-manager/.cache/go-build,id=harvester-ndm-go-build-${MK_REPO_ID} \
    ./scripts/validate-ci

# ---- test ----
FROM base AS test
ARG MK_REPO_ID

RUN --mount=type=cache,target=/go/pkg/mod,id=harvester-ndm-go-mod-${MK_REPO_ID} \
    --mount=type=cache,target=/go/src/github.com/harvester/node-disk-manager/.cache/go-build,id=harvester-ndm-go-build-${MK_REPO_ID} \
    ./scripts/test

# ---- generate ----
FROM base AS generate
ARG MK_REPO_ID

RUN --mount=type=cache,target=/go/pkg/mod,id=harvester-ndm-go-mod-${MK_REPO_ID} \
    --mount=type=cache,target=/go/src/github.com/harvester/node-disk-manager/.cache/go-build,id=harvester-ndm-go-build-${MK_REPO_ID} \
    ./scripts/generate

FROM scratch AS generate-output
COPY --from=generate /go/src/github.com/harvester/node-disk-manager/pkg/ /pkg/

# ---- generate-manifest ----
FROM base AS generate-manifest

RUN ./scripts/generate-manifest

FROM scratch AS generate-manifest-output
COPY --from=generate-manifest /go/src/github.com/harvester/node-disk-manager/manifests/ /manifests/
