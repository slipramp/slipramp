# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

FROM golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS go-build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

ARG TARGETARCH
ARG TARGETOS
RUN CGO_ENABLED=0 GOARCH="${TARGETARCH}" GOOS="${TARGETOS}" \
    go build -trimpath -ldflags="-s -w" \
    -o /out/slipramp-controller ./cmd/slipramp-controller
RUN CGO_ENABLED=0 GOARCH="${TARGETARCH}" GOOS="${TARGETOS}" \
    go build -trimpath -ldflags="-s -w" \
    -o /out/slipramp-harness ./cmd/slipramp-harness
RUN CGO_ENABLED=0 GOARCH="${TARGETARCH}" GOOS="${TARGETOS}" \
    go build -trimpath -ldflags="-s -w" \
    -o /out/slipramp-kargo-tools ./cmd/slipramp-kargo-tools
RUN CGO_ENABLED=0 GOARCH="${TARGETARCH}" GOOS="${TARGETOS}" \
    go build -trimpath -ldflags="-s -w" \
    -o /out/slipramp-server ./cmd/slipramp-server

FROM scratch AS controller

COPY --from=go-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=go-build /out/slipramp-controller /slipramp-controller

USER 65532:65532
ENTRYPOINT ["/slipramp-controller"]

FROM scratch AS harness

COPY --from=go-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=go-build /out/slipramp-harness /slipramp-harness

ENTRYPOINT ["/slipramp-harness"]

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS server

RUN apk add --no-cache ca-certificates git openssh-client \
    && addgroup -S -g 65532 slipramp \
    && adduser -S -D -H -u 65532 -G slipramp slipramp

COPY --from=go-build /out/slipramp-server /slipramp-server

ENV HOME=/tmp

USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/slipramp-server"]

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS checkout

RUN apk add --no-cache ca-certificates git openssh-client

FROM quay.io/argoproj/argocd:v3.5.0@sha256:c298cedbaeb31532ba8d4e9904eba9e4987e067293fbd86400c5194e78f743d5 AS kargo-tools

RUN test "$(git --version)" = "git version 2.53.0" \
    && test "$(argocd version --client --short)" = "argocd: v3.5.0"

COPY --from=go-build /out/slipramp-kargo-tools /usr/local/bin/slipramp-kargo-tools

ENV HOME=/tmp

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/slipramp-kargo-tools"]

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS acceptance-git

RUN apk add --no-cache ca-certificates git openssh-server \
    && addgroup -S git \
    && adduser -S -D -h /home/git -s /usr/bin/git-shell -G git git \
    && printf 'git:acceptance-disabled-password\n' | chpasswd \
    && mkdir -p /run/sshd

FROM node:24-bookworm-slim@sha256:a5f4366e834695d5f363b920c104953a535d3a9864d1bdf74566a0ebe01daee5 AS web-build

WORKDIR /src

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc ./
COPY web/package.json web/package.json

RUN corepack enable \
    && corepack prepare pnpm@12.14.0 --activate \
    && pnpm install --frozen-lockfile

COPY web web

RUN pnpm --filter @slipramp/web run build \
    && pnpm --filter @slipramp/web deploy --prod /out

FROM node:24-bookworm-slim@sha256:a5f4366e834695d5f363b920c104953a535d3a9864d1bdf74566a0ebe01daee5 AS web

ENV HOST=0.0.0.0 \
    NODE_ENV=production \
    PORT=3000

WORKDIR /app

COPY --from=web-build --chown=node:node /out ./

USER node
EXPOSE 3000
CMD ["./node_modules/.bin/react-router-serve", "./build/server/index.js"]
