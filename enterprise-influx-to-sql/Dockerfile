FROM golang:1.22-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git
# Copy shared pkg module (needed by transfer package for SQLite sensor_map loading)
COPY ../pkg /pkg
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o enterprise-influx-to-sql .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/enterprise-influx-to-sql .
COPY configs.yml .
ENTRYPOINT ["./enterprise-influx-to-sql"]
