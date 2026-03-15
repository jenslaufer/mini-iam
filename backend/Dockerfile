# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Cache dependency downloads separately from source compilation
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o launch-kit .

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates wget \
    && addgroup -S iam \
    && adduser -S -G iam iam \
    && mkdir /data \
    && chown iam:iam /data

WORKDIR /app

COPY --from=builder /build/launch-kit .

VOLUME /data

ENV DATABASE_PATH=/data/launch-kit.db

EXPOSE 8080

USER iam

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/launch-kit"]
