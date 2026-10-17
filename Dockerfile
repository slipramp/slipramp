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

FROM scratch AS controller

COPY --from=go-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=go-build /out/slipramp-controller /slipramp-controller

USER 65532:65532
ENTRYPOINT ["/slipramp-controller"]

FROM scratch AS harness

COPY --from=go-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=go-build /out/slipramp-harness /slipramp-harness

ENTRYPOINT ["/slipramp-harness"]

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS checkout

RUN apk add --no-cache ca-certificates git openssh-client
