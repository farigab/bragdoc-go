# ── Stage 1: Build ──────────────────────────────────────────────
FROM golang:1.21-alpine AS build

WORKDIR /src

# Dependências em camada separada para aproveitar cache
COPY go.mod go.sum ./
RUN go mod download

# Copia o restante e compila
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-s -w' \
    -o /app/bragdev \
    ./cmd/bragdev

# ── Stage 2: Runtime ─────────────────────────────────────────────
FROM alpine:3.19

# Certificados para HTTPS + cria usuário não-root
RUN apk add --no-cache ca-certificates && \
    addgroup -S app && \
    adduser -S app -G app

WORKDIR /app

# Copia apenas o necessário do stage de build
COPY --from=build /app/bragdev       ./bragdev
COPY --from=build /src/db/migrations ./db/migrations

# Ajusta dono dos arquivos para o usuário não-root
RUN chown -R app:app /app

USER app

EXPOSE 8080

ENTRYPOINT ["./bragdev"]
