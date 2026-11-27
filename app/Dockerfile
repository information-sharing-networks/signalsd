# check=skip=InvalidDefaultArgInFrom
# (GO_VERSION has no default by design - see below)
#
# build the signalsd service image for prod using the distroless static base.
#
# distroless static (https://github.com/GoogleContainerTools/distroless) contains no shell or package manager,
# only CA root certificates (required by signalsd when it validates JSON schemas from github), timezone data and
# a non-root user. Static compilation (CGO_ENABLED=0) ensures all the go dependencies are embedded in the binary.


# GO_VERSION must be supplied as a build arg - the github workflows pass the version in app/go.mod.
# There is deliberately no default (a default would go out of date when go.mod is updated) - if it is not set
# the build fails with an "invalid reference format" error. To build locally:
#   docker build --build-arg GO_VERSION=$(grep '^go ' app/go.mod | awk '{print $2}') -f app/Dockerfile .

ARG GO_VERSION
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

# --- Create a minimal runtime image using distroless static ---
# pinned by digest - dependabot updates it
FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5

# Copy application files
COPY --from=builder /app/runtime /app/

WORKDIR /app

# run as the distroless non-root user (uid 65532)
USER nonroot

EXPOSE 8080

# GCP uses the ENTRYPOINT and supplies --args="run,all"`
ENTRYPOINT ["/app/signalsd"]
# AWS ECS Express services don't set a command, so CMD provides the default
CMD ["run", "all"]
