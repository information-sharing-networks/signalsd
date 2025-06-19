
#!/bin/bash


export OWNER=${OWNER:-nick@gmail.com}
export PASSWORD=${PASSWORD:-12345678901}
export ISN_SLUG=${ISN_SLUG:-perf-test}
export SIGNAL_TYPE=${SIGNAL_TYPE:-perf-test-validated}
export SEM_VER=${SEM_VER:-1.0.0}
export BATCH_SIZE=${BATCH_SIZE:-1}
export NUM_BATCHES=${NUM_BATCHES:-500}
export LOG_DIR=${LOG_DIR:-"logs"}
export BASE_URL=${BASE_URL:-http://localhost:8080}

echo "🔐 Getting fresh authentication token..."
AUTH_TOKEN=$(curl -s -X POST \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"${OWNER}\",\"password\":\"${PASSWORD}\"}" \
    "${BASE_URL}/api/auth/login" | \
    grep -o '"access_token":"[^"]*"' | \
    cut -d'"' -f4)

if [ -n "$AUTH_TOKEN" ]; then
    export AUTH_TOKEN
    echo "✅ Fresh token obtained: ${AUTH_TOKEN:0:20}..."
else
    echo "❌ Failed to get authentication token"
    exit 1
fi

go build signal-loader.go
./signal-loader