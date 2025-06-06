# build the signalsd service
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

# Install Go tools (goose, sqlc, swag)
ENV GOBIN=/usr/local/bin
RUN go install github.com/pressly/goose/v3/cmd/goose@latest && \
    go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest && \
    go install github.com/swaggo/swag/cmd/swag@latest


WORKDIR /app

COPY entrypoint.sh . 

COPY app/go.mod .
COPY app/go.sum .

RUN go mod download

COPY app .

# Generate code and documentation 
RUN sqlc generate && \
    swag init -g cmd/signalsd/main.go

RUN CGO_ENABLED=0 GOOS=linux go build -o signalsd cmd/signalsd/main.go

# --- FINAL STAGE: Create a lean runtime image ---
FROM alpine:3.21

RUN addgroup -S signalsd && adduser -S -G signalsd signalsd

COPY --from=builder /usr/local/bin/goose /usr/local/bin/
COPY --from=builder /usr/local/bin/swag /usr/local/bin/
COPY --from=builder /usr/local/bin/sqlc /usr/local/bin/

WORKDIR /app

COPY --from=builder /app .
RUN chmod +x ./entrypoint.sh

RUN chown -R signalsd:signalsd .

USER signalsd

ENV DOCKER_ENV=true

# Define the default command to run when the container starts
#ENTRYPOINT ["sh /app/entrypoint.sh -l "]
ENTRYPOINT ["/app/entrypoint.sh", "-e", "prod"] 