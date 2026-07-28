# Build stage
FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0 is safe: the SQLite driver is modernc.org/sqlite, which is pure Go.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/khapi ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/khapi-scrape ./cmd/scrape

# Runtime stage
FROM alpine:3.24

# ca-certificates is required: every source is fetched over HTTPS.
# tzdata keeps date handling correct if you set TZ=Asia/Phnom_Penh.
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 khapi

COPY --from=build /out/khapi /usr/local/bin/khapi
COPY --from=build /out/khapi-scrape /usr/local/bin/khapi-scrape

# The database lives on a volume so scraped data survives redeploys.
RUN mkdir -p /data && chown khapi:khapi /data
VOLUME /data

USER khapi
ENV KHAPI_DB=/data/holidays.db \
    KHAPI_ADDR=:8080 \
    TZ=Asia/Phnom_Penh

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/healthz || exit 1

ENTRYPOINT ["khapi"]
