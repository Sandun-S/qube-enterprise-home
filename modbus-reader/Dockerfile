FROM golang:1.22-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git
COPY ../pkg /pkg
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o modbus-reader .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/modbus-reader .
ENTRYPOINT ["./modbus-reader"]
