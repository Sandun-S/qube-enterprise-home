# influxdb-relay — Qube Enterprise edge write buffer
# Accepts writes on :9096 and relays to influxdb:8086 with disk-backed buffering.
# Config is baked in: always relays to hostname "influxdb" (Docker Swarm service name).
FROM golang:1.20-alpine AS build
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o influxdb-relay .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /build/influxdb-relay .
COPY config.toml .
EXPOSE 9096
CMD ["/app/influxdb-relay", "-config=config.toml"]
