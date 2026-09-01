# Multi-stage build for compositionfactory (cf)
FROM golang:alpine AS builder

WORKDIR /src

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /bin/cf ./cmd/cf

# Final minimal runtime image
FROM alpine:3.21

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
