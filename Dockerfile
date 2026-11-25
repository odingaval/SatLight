# Build the web UI so the API image can serve it (single-origin deploy).
FROM node:20-alpine AS webbuild

WORKDIR /app/web

COPY web/package*.json ./
RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi

COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git make gcc musl-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Embed the built UI into the API binary (replaces the local placeholder).
RUN rm -f ./internal/api/webdist/.gitkeep
COPY --from=webbuild /app/web/dist/. ./internal/api/webdist/

RUN CGO_ENABLED=1 GOOS=linux go build -o /satlight-api ./cmd/api
RUN CGO_ENABLED=1 GOOS=linux go build -o /satlight-worker ./cmd/worker
RUN CGO_ENABLED=1 GOOS=linux go build -o /satlight-migrate ./cmd/migrate

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata postgresql-client

WORKDIR /app

COPY --from=builder /satlight-api /app/satlight-api
COPY --from=builder /satlight-worker /app/satlight-worker
COPY --from=builder /satlight-migrate /app/satlight-migrate
COPY --from=builder /app/migrations /app/migrations
COPY --from=builder /app/.env.example /app/.env.example
COPY --from=builder /app/scripts/start-render.sh /app/start-render.sh
RUN chmod +x /app/start-render.sh

ENV APP_ENV=production

EXPOSE 8080

CMD ["/app/satlight-api"]
