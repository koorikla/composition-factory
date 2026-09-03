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

# Container defaults, as environment rather than as CMD arguments, so they
# survive a CMD override: `docker run <image>` and
# `docker run <image> serve --file mine.cf.yaml` both get them.
#
# Binding 0.0.0.0 is what makes `-p` work at all; inside the container that is
# the container's own network namespace, so what is actually exposed is
# whatever the operator publishes. Publish it to loopback --
# `-p 127.0.0.1:8080:8080` -- and the reachability is exactly the same as the
# native default the unauthenticated-bind guard exists to protect.
ENV CF_ADDR=0.0.0.0:8080
ENV CF_I_KNOW_THIS_IS_UNAUTHENTICATED=1

ENTRYPOINT ["cf"]
CMD ["serve"]
