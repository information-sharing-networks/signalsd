# build the signalsd service image for prod
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY entrypoint.sh . 

COPY app/go.mod .
COPY app/go.sum .

RUN go mod download

COPY app .

RUN CGO_ENABLED=0 GOOS=linux go build -o signalsd cmd/signalsd/main.go

# --- FINAL STAGE: Create a runtime image ---
FROM alpine:3.21

RUN addgroup -S signalsd && adduser -S -G signalsd signalsd

WORKDIR /app

COPY --from=builder /app .
RUN chmod +x ./entrypoint.sh

RUN chown -R signalsd:signalsd .

USER signalsd

ENV DOCKER_ENV=true

ENTRYPOINT ["/app/signalsd"]