FROM golang:1.22-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod ./
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o enterprise-influx-to-sql .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/enterprise-influx-to-sql .
COPY configs.yml .
ENTRYPOINT ["./enterprise-influx-to-sql"]
