FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git make gcc musl-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -o /satlight-api ./cmd/api
RUN CGO_ENABLED=1 GOOS=linux go build -o /satlight-worker ./cmd/worker

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata postgresql-client

WORKDIR /app

COPY --from=builder /satlight-api /app/satlight-api
COPY --from=builder /satlight-worker /app/satlight-worker
COPY --from=builder /app/migrations /app/migrations
COPY --from=builder /app/.env.example /app/.env.example

ENV HTTP_ADDR=:8080
ENV APP_ENV=production

EXPOSE 8080

CMD ["/app/satlight-api"]