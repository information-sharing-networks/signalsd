#!/bin/bash

set -e

export ACCOUNT=${ACCOUNT:-nick@gmail.com}
export PASSWORD=${PASSWORD:-12345678901}
export ISN_SLUG=${ISN_SLUG:-perf-test}
export SIGNAL_TYPE=${SIGNAL_TYPE:-perf-test-validated}
export SEM_VER=${SEM_VER:-1.0.0}
export BATCH_SIZE=${BATCH_SIZE:-10}
export NUM_BATCHES=${NUM_BATCHES:-10}
export LOG_DIR=${LOG_DIR:-"logs"}
export BASE_URL=${BASE_URL:-http://localhost}
export SIGNALS_PORT=${SIGNALS_PORT:-8080}
export ADMIN_PORT=${ADMIN_PORT:-8080}


echo "🔐 Getting fresh authentication token from ${BASE_URL}..."
AUTH_TOKEN=$(curl -s -X POST \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"${ACCOUNT}\",\"password\":\"${PASSWORD}\"}" \
    "${BASE_URL}:${ADMIN_PORT}/api/auth/login" | \
    grep -o '"access_token":"[^"]*"' | \
    cut -d'"' -f4)

if [ -n "$AUTH_TOKEN" ]; then
    export AUTH_TOKEN
    echo "✅ Fresh token obtained: ${AUTH_TOKEN:0:20}..."
else
    echo "❌ Failed to get authentication token"
    exit 1
fi

run_test() {
    local test_id=$1
    local log_file="$LOG_DIR/test_$test_id.log"

    echo "Starting test $test_id...  $BATCH_SIZE"
    PORT=${SIGNALS_PORT} ./signal-loader > "$log_file" 2>&1
    echo "Test $test_id completed"
}


convert_to_ms() {
    local value="$1"
    if [[ "$value" == *"ms"* ]]; then
        echo "$value" | sed 's/ms//'
    elif [[ "$value" == *"µs"* ]]; then
        local us_value=$(echo "$value" | sed 's/µs//')
        echo "scale=3; $us_value / 1000" | bc -l
    elif [[ "$value" == *"s"* ]]; then
        local s_value=$(echo "$value" | sed 's/s//')
        echo "scale=3; $s_value * 1000" | bc -l
    else
        echo "$value"
    fi
}

NUM_PARALLEL_TESTS=${PARALLEL_INSTANCES:-${1:-3}}  # Use PARALLEL_INSTANCES env var, or first param, or default to 3

echo "🚀 Starting $NUM_PARALLEL_TESTS parallel performance tests"
echo "=================================================="

mkdir -p "$LOG_DIR"

rm -f "$LOG_DIR"/test_*.log

# Start parallel tests
echo "Launching $NUM_PARALLEL_TESTS concurrent tests..."
start_time=$(date +%s)

# Launch tests in background
for i in $(seq 1 $NUM_PARALLEL_TESTS); do
    run_test $i &
done

# Wait for all tests to complete
echo "Waiting for all tests to complete..."
wait

end_time=$(date +%s)
total_time=$((end_time - start_time))

echo ""
echo "🎉 All parallel tests completed in ${total_time} seconds!"
echo "=================================================="

# Analyze results
echo ""
echo "📊 PARALLEL TEST RESULTS SUMMARY"
echo "=================================================="

total_signals=0
total_successful_batches=0
total_failed_batches=0
min_signals_per_sec=999999
max_signals_per_sec=0

# Latency tracking variables
total_avg_latency_ms=0
min_latency_ms=999999
max_latency_ms=0
latency_count=0
all_min_latencies=()
all_max_latencies=()
all_avg_latencies=()

for i in $(seq 1 $NUM_PARALLEL_TESTS); do
    log_file="$LOG_DIR/test_$i.log"
    
    if [ -f "$log_file" ]; then
        # Extract test instance ID
        instance_id=$(grep "Starting performance test" "$log_file" | grep -o '\[.*\]' | tr -d '[]')
        
        # Extract metrics
        signals=$(grep "Total Signals:" "$log_file" | grep -o '[0-9]*')
        successful=$(grep "Successful Batches:" "$log_file" | grep -o '[0-9]*')
        failed=$(grep "Failed Batches:" "$log_file" | grep -o '[0-9]*')
        signals_per_sec=$(grep "Signals/Second:" "$log_file" | grep -o '[0-9]*\.[0-9]*')

        # Extract latency metrics (convert to milliseconds for easier aggregation)
        avg_latency=$(grep "Average Latency:" "$log_file" | grep -o '[0-9]*\.[0-9]*[a-z]*' | head -1)
        min_latency=$(grep "Min Latency:" "$log_file" | grep -o '[0-9]*\.[0-9]*[a-z]*' | head -1)
        max_latency=$(grep "Max Latency:" "$log_file" | grep -o '[0-9]*\.[0-9]*[a-z]*' | head -1)
        
        if [ -n "$signals" ] && [ -n "$successful" ] && [ -n "$failed" ] && [ -n "$signals_per_sec" ]; then
            # Convert latencies to milliseconds
            avg_latency_ms=""
            min_latency_ms_val=""
            max_latency_ms_val=""

            if [ -n "$avg_latency" ]; then
                avg_latency_ms=$(convert_to_ms "$avg_latency")
                all_avg_latencies+=("$avg_latency_ms")
            fi
            if [ -n "$min_latency" ]; then
                min_latency_ms_val=$(convert_to_ms "$min_latency")
                all_min_latencies+=("$min_latency_ms_val")
            fi
            if [ -n "$max_latency" ]; then
                max_latency_ms_val=$(convert_to_ms "$max_latency")
                all_max_latencies+=("$max_latency_ms_val")
            fi

            # Display test results with latency info
            latency_info=""
            if [ -n "$avg_latency_ms" ]; then
                latency_info=", avg latency: ${avg_latency_ms}ms"
            fi
            echo "Test $i [$instance_id]: $signals signals, $successful/$((successful + failed)) batches successful, ${signals_per_sec} signals/sec${latency_info}"

            total_signals=$((total_signals + signals))
            total_successful_batches=$((total_successful_batches + successful))
            total_failed_batches=$((total_failed_batches + failed))

            # Track min/max signals per second
            if (( $(echo "$signals_per_sec < $min_signals_per_sec" | bc -l) )); then
                min_signals_per_sec=$signals_per_sec
            fi
            if (( $(echo "$signals_per_sec > $max_signals_per_sec" | bc -l) )); then
                max_signals_per_sec=$signals_per_sec
            fi

            # Track latency aggregation
            if [ -n "$avg_latency_ms" ]; then
                total_avg_latency_ms=$(echo "scale=3; $total_avg_latency_ms + $avg_latency_ms" | bc -l)
                latency_count=$((latency_count + 1))
            fi
            if [ -n "$min_latency_ms_val" ] && (( $(echo "$min_latency_ms_val < $min_latency_ms" | bc -l) )); then
                min_latency_ms=$min_latency_ms_val
            fi
            if [ -n "$max_latency_ms_val" ] && (( $(echo "$max_latency_ms_val > $max_latency_ms" | bc -l) )); then
                max_latency_ms=$max_latency_ms_val
            fi
        else
            echo "Test $i: Failed to parse results (check $log_file)"
        fi
    else
        echo "Test $i: Log file not found"
    fi
done

# Calculate latency statistics
overall_avg_latency=""
latency_p50=""
latency_p95=""
latency_p99=""

if [ $latency_count -gt 0 ]; then
    overall_avg_latency=$(echo "scale=2; $total_avg_latency_ms / $latency_count" | bc -l)

    # Calculate percentiles from all latency measurements
    if [ ${#all_avg_latencies[@]} -gt 0 ]; then
        # Sort latencies for percentile calculation
        IFS=$'\n' sorted_latencies=($(sort -n <<<"${all_avg_latencies[*]}"))
        unset IFS

        count=${#sorted_latencies[@]}
        p50_idx=$(echo "($count * 50) / 100" | bc)
        p95_idx=$(echo "($count * 95) / 100" | bc)
        p99_idx=$(echo "($count * 99) / 100" | bc)

        latency_p50=${sorted_latencies[$p50_idx]}
        latency_p95=${sorted_latencies[$p95_idx]}
        latency_p99=${sorted_latencies[$p99_idx]}
    fi
fi

echo "=================================================="
echo "AGGREGATE RESULTS:"
echo "Total Signals Processed: $total_signals"
echo "Total Successful Batches: $total_successful_batches"
echo "Total Failed Batches: $total_failed_batches"
echo "Success Rate: $(echo "scale=1; $total_successful_batches * 100 / ($total_successful_batches + $total_failed_batches)" | bc -l)%"
echo "Combined Throughput: $(echo "scale=2; $total_signals / $total_time" | bc -l) signals/sec"
echo "Min Instance Throughput: $min_signals_per_sec signals/sec"
echo "Max Instance Throughput: $max_signals_per_sec signals/sec"
echo "Test Duration: ${total_time} seconds"
echo ""
echo "📊 LATENCY STATISTICS:"
if [ -n "$overall_avg_latency" ]; then
    echo "Average Latency (across all tests): ${overall_avg_latency}ms"
    echo "Fastest Response Time: ${min_latency_ms}ms"
    echo "Slowest Response Time: ${max_latency_ms}ms"
    if [ -n "$latency_p50" ]; then
        echo "Latency P50 (median): ${latency_p50}ms"
    fi
    if [ -n "$latency_p95" ]; then
        echo "Latency P95: ${latency_p95}ms"
    fi
    if [ -n "$latency_p99" ]; then
        echo "Latency P99: ${latency_p99}ms"
    fi

else
    echo "⚠️  Latency data not available"
fi
