# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:24-alpine AS web
RUN corepack enable
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/ipxemultiboot ./cmd/ipxemultiboot

FROM alpine:3.22
LABEL org.opencontainers.image.title="iPXE Multiboot" \
      org.opencontainers.image.licenses="GPL-3.0-only" \
      org.opencontainers.image.source="https://github.com/haouarihk/ipxemultiboot"
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/ipxemultiboot /usr/local/bin/ipxemultiboot
ENV IPXEMULTIBOOT_DATA_DIR=/data \
    IPXEMULTIBOOT_ISO_DIR=/isos
VOLUME ["/data", "/isos"]
# DHCP 67 (+4011 proxy), TFTP 69, HTTP 8080. Use host networking: PXE relies on broadcasts.
EXPOSE 67/udp 69/udp 4011/udp 8080/tcp
ENTRYPOINT ["/usr/local/bin/ipxemultiboot"]
