# syntax=docker/dockerfile:1
# Dockerfile multi-stage untuk Elvan Catalog API (ARCHITECTURE §15).
#
# Build context = root repo:
#   docker build -f deploy/Dockerfile -t elvan-api:dev .
#
# Target:
#   api     — image runtime minimal non-root berisi cmd/api (default)
#   migrate — image berisi goose + db/migrations untuk langkah migrasi terpisah
#
# Catatan: migrasi TIDAK dijalankan saat startup API (lihat deploy/compose.yaml).

# --- build ---------------------------------------------
FROM golang:1.26-alpine AS build

WORKDIR /src

# Unduh dependensi dulu supaya lapisan cache stabil.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO dimatikan supaya binary statis dan bisa jalan di distroless.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/adminctl ./cmd/adminctl \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/importer ./cmd/importer

# --- api runtime ---------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS api

# Binari + direktori kerja; tanpa shell/package manager (image minimal).
# adminctl & importer ikut dibawa supaya operasional (buat admin, impor data,
# verifikasi) bisa dijalankan dengan `docker compose run --rm` tanpa toolchain
# Go di VPS.
COPY --from=build /out/api /usr/local/bin/api
COPY --from=build /out/adminctl /usr/local/bin/adminctl
COPY --from=build /out/importer /usr/local/bin/importer

# nonroot (uid 65532) sudah jadi user bawaan image :nonroot.
USER nonroot:nonroot

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]

# --- migrate -------------------------------------------
# Dipakai service `migrate` di compose: dijalankan sekali sebelum API naik,
# lalu berhenti. Berisi goose + file migrasi, tanpa binary API.
FROM golang:1.26-alpine AS goose

# goose versi sama dengan yang dipakai CI/lokal (lihat ci.yml); dipasang dari
# sumber supaya tidak bergantung pada rilis binari pihak ketiga.
RUN CGO_ENABLED=0 go install github.com/pressly/goose/v3/cmd/goose@v3.28.0

FROM alpine:3.20 AS migrate

RUN addgroup -S app && adduser -S -G app app

COPY --from=goose /go/bin/goose /usr/local/bin/goose
COPY --from=build /src/db/migrations /migrations

# Verifikasi binari benar-benar jalan sebelum image dipakai (fail cepat saat
# build, bukan saat cutover).
RUN apk add --no-cache ca-certificates && /usr/local/bin/goose --version

USER app:app
WORKDIR /migrations

# DSN diambil dari GOOSE_DBSTRING (di-set compose dari DATABASE_URL), jadi
# tidak perlu argumen posisional / perintah shell di compose.
ENV GOOSE_DRIVER=postgres
ENTRYPOINT ["/usr/local/bin/goose"]
CMD ["-dir", "/migrations", "up"]
