#!/bin/bash

set -e

# Set default environment variables
export OWNER=${OWNER:-nick@gmail.com}
export PASSWORD=${PASSWORD:-12345678901}
export BASE_URL=${BASE_URL:-http://localhost:8080}

echo "🔧 Setting up performance test environment..."
echo "SiteAdmin: ${OWNER}"
echo "Base URL: ${BASE_URL}"

echo "👤 Creating owner account: ${OWNER}"
curl --location "${BASE_URL}/api/auth/register" \
--header 'Content-Type: application/json' \
--data-raw "{
    \"email\": \"${OWNER}\",
    \"password\": \"${PASSWORD}\"
}" || echo "Account may already exist, continuing..."

echo ""
echo "🔐 Getting fresh authentication token from ${BASE_URL}..."
AUTH_TOKEN=$(curl -s -X POST \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"${OWNER}\",\"password\":\"${PASSWORD}\"}" \
    "${BASE_URL}/api/auth/login" | \
    grep -o '"access_token":"[^"]*"' | \
    cut -d'"' -f4)

if [ -z "$AUTH_TOKEN" ]; then
    echo "❌ Failed to get authentication token"
    exit 1
fi

echo "✅ Authentication token obtained: ${AUTH_TOKEN:0:20}..."

echo ""
echo "🏗️  Creating ISN: 'Perf Test'..."
ISN_RESPONSE=$(curl -s -X POST \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{
        "title": "Perf Test",
        "detail": "Performance testing ISN for signalsd load testing",
        "is_in_use": true,
        "visibility": "private"
    }' \
    "${BASE_URL}/api/isn")

ISN_SLUG=$(echo "$ISN_RESPONSE" | grep -o '"slug":"[^"]*"' | cut -d'"' -f4)

if [ -z "$ISN_SLUG" ]; then
    echo "❌ Failed to create ISN. Response: $ISN_RESPONSE"
    exit 1
fi

echo "✅ ISN created with slug: $ISN_SLUG"

echo ""
echo "📋 Creating signal type: 'Perf test unvalidated'..."
SIGNAL_TYPE_RESPONSE=$(curl -s -X POST \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{
        "title": "Perf test unvalidated",
        "schema_url": "https://github.com/skip/validation/main/schema.json",
        "readme_url": "https://github.com/skip/validation/main/readme.md",
        "detail": "Performance test signal type with validation disabled for maximum throughput",
        "bump_type": "major"
    }' \
    "${BASE_URL}/api/isn/${ISN_SLUG}/signal-types")
SIGNAL_TYPE_SLUG=$(echo "$SIGNAL_TYPE_RESPONSE" | grep -o '"slug":"[^"]*"' | cut -d'"' -f4)
SIGNAL_TYPE_VERSION=$(echo "$SIGNAL_TYPE_RESPONSE" | grep -o '"sem_ver":"[^"]*"' | cut -d'"' -f4)

echo "✅ Signal type created with slug: $SIGNAL_TYPE_SLUG, version: $SIGNAL_TYPE_VERSION"

echo "📋 Creating signal type: 'Perf test validated'..."
SIGNAL_TYPE_RESPONSE=$(curl -s -X POST \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{
        "title": "Perf test validated",
        "schema_url": "https://github.com/information-sharing-networks/signalsd_test_schemas/blob/main/2025.05.13/complex_schema.json",
        "readme_url": "https://github.com/main/readme.md",
        "detail": "Performance test signal type with validation",
        "bump_type": "major"
    }' \
    "${BASE_URL}/api/isn/${ISN_SLUG}/signal-types")
SIGNAL_TYPE_SLUG=$(echo "$SIGNAL_TYPE_RESPONSE" | grep -o '"slug":"[^"]*"' | cut -d'"' -f4)
SIGNAL_TYPE_VERSION=$(echo "$SIGNAL_TYPE_RESPONSE" | grep -o '"sem_ver":"[^"]*"' | cut -d'"' -f4)

if [ -z "$SIGNAL_TYPE_SLUG" ] || [ -z "$SIGNAL_TYPE_VERSION" ]; then
    echo "❌ Failed to create signal type. Response: $SIGNAL_TYPE_RESPONSE"
    exit 1
fi

echo "✅ Signal type created with slug: $SIGNAL_TYPE_SLUG, version: $SIGNAL_TYPE_VERSION"

echo ""
echo "🎉 Setup complete!"
echo "=================================================="
echo "ISN Slug: $ISN_SLUG"
echo "Signal Type Slug: $SIGNAL_TYPE_SLUG"
echo "Signal Type Version: $SIGNAL_TYPE_VERSION"
echo "=================================================="


