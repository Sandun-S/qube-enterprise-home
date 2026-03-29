FROM golang:1.22-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git
COPY pkg /pkg
COPY http-reader/go.mod http-reader/go.sum* ./
RUN go mod download 2>/dev/null || true
COPY http-reader/ .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o http-reader .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/http-reader .
ENTRYPOINT ["./http-reader"]
