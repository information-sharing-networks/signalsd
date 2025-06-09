# build the signalsd service image for prod
FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY app/go.mod .
COPY app/go.sum .

RUN go mod download

COPY app .

RUN mkdir runtime

RUN CGO_ENABLED=0 GOOS=linux go build -o runtime/signalsd cmd/signalsd/main.go

COPY /app/docs runtime/docs/

# --- Create a runtime image ---
FROM alpine:3.21

RUN addgroup -S signalsd && adduser -S -G signalsd signalsd

WORKDIR /app

COPY --from=builder --chown=signalsd:signalsd /app/runtime .

USER signalsd

ENTRYPOINT ["/app/signalsd"]