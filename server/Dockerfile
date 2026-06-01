FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app

COPY --from=builder /app/server /app/server

# 数据目录
RUN mkdir -p /app/data

ENV SERVER_PORT=8787
ENV DATA_DIR=/app/data
ENV OBSIDIAN_VAULT_PATH=/obsidian
ENV OBSIDIAN_WORDS_DIR=英语学习/words
ENV DICT_CACHE_TTL=168h

EXPOSE 8787

ENTRYPOINT ["/app/server"]
