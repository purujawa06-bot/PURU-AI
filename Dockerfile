# PURU-AI lightweight — single Go stage, no frontend, no Node.
FROM golang:1.26-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /app/puru-ai .

# Runtime stage
FROM alpine:3.20

# tini as PID 1 reaps orphaned exec grandchildren (git, tar, ssl_client)
# that outlive their shell on timeout/kill so they never pile up as zombies.
RUN apk add --no-cache ca-certificates tzdata tini

WORKDIR /app
COPY --from=build /app/puru-ai .
COPY --from=build /app/example.config.json ./example.config.json

# Lightweight profile: heap cap 50MB (main.go also sets it via debug.SetMemoryLimit).
ENV GOMEMLIMIT=50MiB
# Config + workspace live here; mount a volume to persist.
VOLUME ["/root/.puru"]

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/app/puru-ai", "--config", "/root/.puru/config.json"]
