FROM golang:1.24-alpine AS builder

ENV GOTOOLCHAIN=auto

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY shared/ ./shared/
COPY api-gateway/ ./api-gateway/

WORKDIR /app/api-gateway
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/api-gateway ./cmd

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/bin/api-gateway .
COPY api-gateway/docs/api-spec.yaml ./docs/api-spec.yaml

RUN mkdir -p /var/log/vcs-sms

EXPOSE 8080

CMD ["./api-gateway"]
