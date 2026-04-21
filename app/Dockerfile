# build the signalsd service image for prod using scratch base.
#
# Note that the historical need to copy ca-certificates and tzdata from builder images do not apply in Go 1.25+
# - CA root certificates: Go's crypto/x509 package includes embedded root certificates for TLS connections (required by signalsd when it validates JSON schemas from github)
# - Timezone data: Go's time/tzdata package can embed timezone data when needed (not required for signalsd)
# - Static compilation (CGO_ENABLED=0) ensures all dependencies are embedded in the binary 


# GO version is replaced by the github workflow with the version in app/go.mod
# if running the dockerfile locally set GO_VERSION in your env to overwrite the default below

ARG GO_VERSION=1.26.2 
FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /app

COPY app/go.mod .
COPY app/go.sum .

RUN go mod download

COPY app .

RUN mkdir runtime


# placeholder args for version info
ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG GIT_COMMIT=unknown

# Build static binary 
RUN CGO_ENABLED=0 GOOS=linux go build \
    -a -installsuffix cgo \
    -ldflags "-s -w -extldflags '-static' -X github.com/information-sharing-networks/signalsd/app/internal/version.version=${VERSION} -X github.com/information-sharing-networks/signalsd/app/internal/version.buildDate=${BUILD_DATE} -X github.com/information-sharing-networks/signalsd/app/internal/version.gitCommit=${GIT_COMMIT}" \
    -o runtime/signalsd cmd/signalsd/main.go

COPY /app/docs runtime/docs/
COPY /app/assets runtime/assets/
COPY /app/web runtime/web

# --- Create a minimal runtime image using scratch ---
FROM scratch

# Copy application files
COPY --from=builder /app/runtime /app/

WORKDIR /app

EXPOSE 8080

# GCP uses the ENTRYPOINT and supplies --args="run,all"`
ENTRYPOINT ["/app/signalsd"]
# AWS App Runner has no runtime arg override (unlike Cloud Run's --args), so CMD provides the default
CMD ["run", "all"]
