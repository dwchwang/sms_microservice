# ── build (Alpine) ──
FROM golang:1.24-alpine AS builder

ENV GOTOOLCHAIN=auto

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY shared/ ./shared/
COPY report-service/ ./report-service/

WORKDIR /app/report-service
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/report-service ./cmd/main.go

# Log mount point, pre-owned by the nonroot uid so lumberjack can write to it
# whether or not a volume is mounted over /var/log/vcs-sms.
RUN mkdir -p /out/var/log/vcs-sms

# ── run (distroless, non-root) ──
# static-debian12 ships ca-certificates + tzdata (Asia/Ho_Chi_Minh) and a
# nonroot (uid 65532) account; no shell or package manager in the final image.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /app/bin/report-service /app/bin/report-service

# Email templates are resolved via runtime.Caller at the source-relative path.
COPY --from=builder /app/report-service/internal/infrastructure/email/templates /app/report-service/internal/infrastructure/email/templates

COPY --from=builder --chown=65532:65532 /out/var/log/vcs-sms /var/log/vcs-sms

USER nonroot:nonroot
EXPOSE 8084

ENTRYPOINT ["/app/bin/report-service"]
