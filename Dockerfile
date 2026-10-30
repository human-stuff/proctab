# Cross-compiles on the build host (no QEMU needed), ships a scratch image.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ARG TARGETOS TARGETARCH TARGETVARIANT
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /out/proctab ./cmd/proctab

FROM scratch
COPY --from=build /out/proctab /proctab
ENV TERM=xterm-256color
ENTRYPOINT ["/proctab"]
