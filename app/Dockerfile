# build the signalsd service image for prod
FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY app/go.mod .
COPY app/go.sum .

RUN go mod download

COPY app .

RUN mkdir runtime

# Build args for version info
ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG GIT_COMMIT=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X github.com/information-sharing-networks/signalsd/app/internal/version.version=${VERSION} -X github.com/information-sharing-networks/signalsd/app/internal/version.buildDate=${BUILD_DATE} -X github.com/information-sharing-networks/signalsd/app/internal/version.gitCommit=${GIT_COMMIT}" \
    -o runtime/signalsd cmd/signalsd/main.go

COPY /app/docs runtime/docs/
COPY /app/assets runtime/assets/

# --- Create a runtime image ---
FROM alpine:3.21

RUN addgroup -S signalsd && adduser -S -G signalsd signalsd

WORKDIR /app

COPY --from=builder --chown=signalsd:signalsd /app/runtime .

USER signalsd

ENTRYPOINT ["/app/signalsd"]