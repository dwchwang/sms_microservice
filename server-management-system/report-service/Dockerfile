FROM golang:1.24-alpine AS builder

ENV GOTOOLCHAIN=auto

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY shared/ ./shared/
COPY report-service/ ./report-service/

WORKDIR /app/report-service
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/report-service ./cmd/main.go

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
RUN mkdir -p /app/bin
COPY --from=builder /app/bin/report-service ./bin/report-service

# Copy email templates to the source-relative path used by runtime.Caller.
COPY --from=builder /app/report-service/internal/infrastructure/email/templates /app/report-service/internal/infrastructure/email/templates

RUN mkdir -p /var/log/vcs-sms

EXPOSE 8084

CMD ["./bin/report-service"]
