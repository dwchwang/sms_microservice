FROM golang:1.24-alpine AS builder

ENV GOTOOLCHAIN=auto

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY shared/ ./shared/
COPY server-service/ ./server-service/

WORKDIR /app/server-service
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/server-service ./cmd/main.go

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/bin/server-service .

RUN mkdir -p /var/log/vcs-sms

EXPOSE 8082

CMD ["./server-service"]
