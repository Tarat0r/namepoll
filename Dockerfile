# syntax=docker/dockerfile:1

FROM golang:1.27.1-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/namepoll \
    ./cmd/server

FROM alpine:3.22

RUN addgroup -S namepoll \
    && adduser -S -G namepoll namepoll \
    && mkdir -p /app/data \
    && chown -R namepoll:namepoll /app

WORKDIR /app

COPY --from=build --chown=namepoll:namepoll /out/namepoll /app/namepoll
COPY --chown=namepoll:namepoll web /app/web

USER namepoll:namepoll

EXPOSE 8080

ENTRYPOINT ["/app/namepoll"]
