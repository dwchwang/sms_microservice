FROM golang:1.24-alpine AS builder

ENV GOTOOLCHAIN=auto

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY shared/ ./shared/
COPY fileio-service/ ./fileio-service/

WORKDIR /app/fileio-service
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/fileio-service ./cmd/main.go

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/bin/fileio-service .

# Create upload directory matching FILEIO_UPLOAD_DIR in docker-compose env.
RUN mkdir -p /app/uploads

EXPOSE 8085

CMD ["./fileio-service"]
