# Multi-stage build for compositionfactory (cf)
# Build on native host platform using Go cross-compilation (avoids slow QEMU emulation)
# The toolchain is pinned to the minor the module declares: this project's
# contract is byte-identical output for the same inputs, so the compiler is
# an input worth naming.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

WORKDIR /src

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS TARGETARCH VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /bin/cf ./cmd/cf

# Final minimal runtime image for the target architecture
FROM alpine:3.24

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S cf && adduser -S cf -G cf -h /home/cf && \
    mkdir -p /workspace /home/cf/.cache/compositionfactory && \
    chown -R cf:cf /workspace /home/cf

WORKDIR /workspace

COPY --from=builder /bin/cf /usr/local/bin/cf

USER cf

EXPOSE 8080

ENTRYPOINT ["cf"]
CMD ["serve", "--help"]
