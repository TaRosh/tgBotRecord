# --- Этап 1: сборка. Тяжёлый образ с компилятором нужен только здесь. ---
FROM golang:1.27-alpine AS build
WORKDIR /src

# Сначала только go.mod/go.sum: слой с зависимостями кэшируется
# и не перекачивается, пока они не изменились.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0 — полностью статический бинарник (SQLite у нас на чистом Go).
# -trimpath убирает локальные пути из бинарника, -s -w — отладочную информацию.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot \
    && mkdir -p /out/data

# --- Этап 2: запуск. Только бинарник, без shell и компилятора. ---
# distroless/static содержит CA-сертификаты (нужны для HTTPS к api.telegram.org)
# и пользователя nonroot (uid 65532): бот не работает от root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bot /bot
# Папка под базу, принадлежащая nonroot: Docker скопирует владельца в новый том.
COPY --from=build --chown=65532:65532 /out/data /data

ENV DB_PATH=/data/bot.db
VOLUME ["/data"]

# Exec-форма: бинарник — процесс №1 и сам получает SIGTERM от `docker stop`.
ENTRYPOINT ["/bot"]
