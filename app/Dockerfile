# build the signalsd service
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

RUN go install github.com/pressly/goose/v3/cmd/goose@latest && \
    go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest && \
    go install github.com/swaggo/swag/cmd/swag@latest

WORKDIR /app

COPY app/go.mod .
COPY app/go.sum .

RUN go mod download

COPY app .

COPY ../entrypoint.sh .

# Generate code and documentation
RUN sqlc generate && \
    swag init -g cmd/signalsd/main.go

RUN CGO_ENABLED=0 GOOS=linux go build -o signalsd cmd/signalsd/main.go

# final stage
FROM alpine:3.21

# non-root user 
RUN addgroup -S signalsd && adduser -S -G signalsd signalsd


COPY --from=builder /go/bin/goose /usr/local/bin/

WORKDIR /app
COPY --from=builder /app/entrypoint.sh .
RUN chmod +x ./entrypoint.sh

RUN chown -R signalsd:signalsd .

USER signalsd

COPY --from=builder /app/signalsd .
COPY --from=builder /app/sql/schema ./sql/schema
COPY --from=builder /app/docs ./docs
COPY --from=builder /app/assets ./assets
