#!/bin/bash

# This script creates a new release by incrementing the version number based on the latest github tag 
# the new tag is pushed to github triggering CD pipeline 
# the script also builds the local application with the new version number

set -e

usage() {
    cat << !
    Usage: $0 [-bl] [-t TYPE]

    Options:
        -b          build only (build the application but don't push the tag to github)
        -l          build binary for linux rather than Mac (GOOS=linux GOARCH=amd64)
        -t TYPE     tag version bump type (major|minor|patch) - this will push the tag to github
!
}

get_latest_version() {
    git describe --tags --abbrev=0 |sed -e "s/^v//" 2>/dev/null || echo "v0.0.0"
}

bump_version() {
    local latest_version=$1
    local bump_type=$2

    IFS='.' parts=($latest_version)
    major=${parts[0]}
    minor=${parts[1]}
    patch=${parts[2]}
    

    if [ -z "$major" ] || [ -z "$minor" ] || [ -z "$patch" ]; then
        echo "Error: Invalid version format '$latest_version'" >&2
        exit 1
    fi

    case "$bump_type" in
        major) echo "$((major + 1)).0.0"  ;;
        minor) echo "${major}.$((minor + 1)).0"  ;;
        patch) echo "${major}.${minor}.$((patch + 1))"  ;;
    esac
}

if [ ! -f "app/go.mod" ]; then
    echo "Error: Run from signalsd root directory" >&2
    exit 1
fi

BUMP_TYPE=""

while getopts "hblt:" opt; do
    case $opt in
        h) usage; exit 0 ;;
        b) BUILD_ONLY=true ;;
        t) BUMP_TYPE=$OPTARG;;
        l) LINUX=true ;;
        *) usage >&2; exit 1 ;;
    esac
done

if [ -z "$BUILD_ONLY" ];then
    if [ -z "$BUMP_TYPE" ]; then
        usage
        exit
    fi

    if [ "$BUMP_TYPE" != "major" ] && [ "$BUMP_TYPE" != "minor" ] && [ "$BUMP_TYPE" != "patch" ]; then
        echo "Error: Invalid bump type '$BUMP_TYPE'" >&2
        exit 1
    fi

    latest_version=$(get_latest_version)

    new_version=$(bump_version "$latest_version" "$BUMP_TYPE")

    new_tag="v$new_version"
fi


if [ ${BUILD_ONLY} ]; then
    echo "build only: skipping push of new tag to github"
else
    echo "Creating $BUMP_TYPE release: v$latest_version -> $new_tag"

    git diff-index --quiet HEAD -- || { echo "Error: Uncommitted changes" >&2; exit 1; }
    [ "$(git branch --show-current)" = "main" ] || { echo "Error: Must be on main branch" >&2; exit 1; }

    # Create and push tag
    git tag -a "$new_tag" -m "Release $new_tag"
    git push origin "$new_tag"

    echo "Tag $new_tag created and pushed"
fi

# Build application
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

echo "Building signalsd version: $VERSION"
LDFLAGS="-X github.com/information-sharing-networks/signalsd/app/internal/version.version=$VERSION \
    -X github.com/information-sharing-networks/signalsd/app/internal/version.buildDate=$BUILD_DATE \
    -X github.com/information-sharing-networks/signalsd/app/internal/version.gitCommit=$GIT_COMMIT"

cd app

if [ ${LINUX} ]; then
    echo "Building for linux"
    GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o signalsd ./cmd/signalsd/
else
    echo "Building for darwin"
    go build -ldflags "$LDFLAGS" -o signalsd ./cmd/signalsd/
fi
cd ..

echo "Build complete: signalsd"