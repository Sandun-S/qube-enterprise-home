FROM golang:1.22-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod ./
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o conf-agent .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata docker-cli
WORKDIR /app
COPY --from=builder /app/conf-agent .
ENTRYPOINT ["./conf-agent"]
