FROM golang:1.24-alpine AS builder

ENV GOTOOLCHAIN=auto

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY tcp-simulator/ ./

RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/tcp-simulator ./cmd/main.go

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata netcat-openbsd

WORKDIR /app

COPY --from=builder /app/bin/tcp-simulator .

EXPOSE 9001-19000

CMD ["./tcp-simulator"]
