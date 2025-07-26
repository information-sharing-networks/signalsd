#!/bin/bash

# Baseline Performance Testing Script
# Runs multiple iterations of parallel tests to establish performance baseline

set -e

# Configuration
SIGNALS_IN_PAYLOAD=${SIGNALS_IN_PAYLOAD:-10}
NUM_REQUESTS=${NUM_REQUESTS:-3}
PARALLEL_CLIENTS=${PARALLEL_CLIENTS:-50}
ITERATIONS=${ITERATIONS:-5}

# Create results directory
RESULTS_DIR="tmp/logs/baseline/$(date +%Y%m%d_%H%M%S)"
mkdir -p "$RESULTS_DIR"

# Get signalsd version from admin container
SIGNALSD_VERSION=$(docker exec signalsd-admin-multi /signalsd/app/signalsd -v 2>/dev/null | head -1 || echo "unknown")

echo "🔬 BASELINE PERFORMANCE TESTING"
echo "=================================="
echo "Configuration:"
echo "  signalsd Version: $SIGNALSD_VERSION"
echo "  Concurrent clients: $PARALLEL_CLIENTS"
echo "  Payload Size: $SIGNALS_IN_PAYLOAD signals per request"
echo "  Number of requests made per client: $NUM_REQUESTS"
echo "  Test iterations: $ITERATIONS"
echo "  Total signals per iteration: $((SIGNALS_IN_PAYLOAD * NUM_REQUESTS * PARALLEL_CLIENTS))"
echo
echo "To rerun this test use: SIGNALS_IN_PAYLOAD=$SIGNALS_IN_PAYLOAD NUM_REQUESTS=$NUM_REQUESTS PARALLEL_CLIENTS=$PARALLEL_CLIENTS ITERATIONS=$ITERATIONS ./baseline-tests.sh"
echo
echo "see $RESULTS_DIR for outputs"
echo "=================================="
echo ""


# Arrays to store aggregate results
declare -a THROUGHPUTS
declare -a AVG_LATENCIES
declare -a P95_LATENCIES
declare -a P99_LATENCIES
declare -a TEST_DURATIONS

echo "🚀 Starting $ITERATIONS baseline test iterations..."
echo ""

for i in $(seq 1 $ITERATIONS); do
    echo "📊 Running iteration $i/$ITERATIONS..."
    
    # Run the test and capture output
    OUTPUT_FILE="$RESULTS_DIR/iteration_${i}_summary.log"
    
    if SIGNALS_IN_PAYLOAD=$SIGNALS_IN_PAYLOAD NUM_REQUESTS=$NUM_REQUESTS LOG_DIR=${RESULTS_DIR}/iteration_${i} ./run-parallel-tests.sh $PARALLEL_CLIENTS > "$OUTPUT_FILE" 2>&1; then
        echo "✅ Iteration $i completed successfully"
        
        # Extract key metrics from the output
        THROUGHPUT_LINE=$(grep "Combined Throughput:" "$OUTPUT_FILE")
        if [[ "$THROUGHPUT_LINE" =~ \>([0-9]+)\ signals/sec ]]; then
            # Handle case where test completed in <1 second
            THROUGHPUT="${BASH_REMATCH[1]}"
        else
            # Handle normal case with decimal throughput
            THROUGHPUT=$(echo "$THROUGHPUT_LINE" | awk '{print $3}')
        fi
        AVG_LATENCY=$(grep "Average Latency (across all tests):" "$OUTPUT_FILE" | awk '{print $6}' | sed 's/ms//')
        P95_LATENCY=$(grep "Latency P95:" "$OUTPUT_FILE" | awk '{print $3}' | sed 's/ms//')
        P99_LATENCY=$(grep "Latency P99:" "$OUTPUT_FILE" | awk '{print $3}' | sed 's/ms//')
        DURATION=$(grep "Test Duration:" "$OUTPUT_FILE" | awk '{print $3}')
        
        # Store results
        THROUGHPUTS+=($THROUGHPUT)
        AVG_LATENCIES+=($AVG_LATENCY)
        P95_LATENCIES+=($P95_LATENCY)
        P99_LATENCIES+=($P99_LATENCY)
        TEST_DURATIONS+=($DURATION)
        
        echo "   Throughput: ${THROUGHPUT} signals/sec"
        echo "   Avg Latency: ${AVG_LATENCY}ms"
        echo "   P95 Latency: ${P95_LATENCY}ms"
        echo ""
    else
        echo "❌ Iteration $i failed - check $OUTPUT_FILE for details"
    fi
    
    # Brief pause between iterations
    sleep 2
done

# Create summary text file
SUMMARY_TEXT_FILE="$RESULTS_DIR/baseline_summary.txt"

# Function to output summary (will be teed to file and terminal)
output_summary() {
echo "📈 BASELINE RESULTS SUMMARY"
echo "=========================="
echo "Configuration:"
echo "  signalsd Version: $SIGNALSD_VERSION"
echo "  Concurrent clients: $PARALLEL_CLIENTS"
echo "  Payload Size: $SIGNALS_IN_PAYLOAD signals per request"
echo "  Number of requests made per client: $NUM_REQUESTS"
echo "  Test iterations: $ITERATIONS"
echo "  Total signals per iteration: $((SIGNALS_IN_PAYLOAD * NUM_REQUESTS * PARALLEL_CLIENTS))"

# Calculate statistics
if [ ${#THROUGHPUTS[@]} -gt 0 ]; then
    # Calculate averages
    TOTAL_THROUGHPUT=0
    TOTAL_AVG_LATENCY=0
    TOTAL_P95_LATENCY=0
    TOTAL_P99_LATENCY=0
    TOTAL_DURATION=0
    
    for val in "${THROUGHPUTS[@]}"; do
        TOTAL_THROUGHPUT=$(echo "$TOTAL_THROUGHPUT + $val" | bc -l)
    done
    
    for val in "${AVG_LATENCIES[@]}"; do
        TOTAL_AVG_LATENCY=$(echo "$TOTAL_AVG_LATENCY + $val" | bc -l)
    done
    
    for val in "${P95_LATENCIES[@]}"; do
        TOTAL_P95_LATENCY=$(echo "$TOTAL_P95_LATENCY + $val" | bc -l)
    done
    
    for val in "${P99_LATENCIES[@]}"; do
        TOTAL_P99_LATENCY=$(echo "$TOTAL_P99_LATENCY + $val" | bc -l)
    done
    
    for val in "${TEST_DURATIONS[@]}"; do
        TOTAL_DURATION=$(echo "$TOTAL_DURATION + $val" | bc -l)
    done
    
    COUNT=${#THROUGHPUTS[@]}
    
    AVG_THROUGHPUT=$(echo "scale=2; $TOTAL_THROUGHPUT / $COUNT" | bc -l)
    AVG_OF_AVG_LATENCY=$(echo "scale=2; $TOTAL_AVG_LATENCY / $COUNT" | bc -l)
    AVG_P95_LATENCY=$(echo "scale=2; $TOTAL_P95_LATENCY / $COUNT" | bc -l)
    AVG_P99_LATENCY=$(echo "scale=2; $TOTAL_P99_LATENCY / $COUNT" | bc -l)
    AVG_DURATION=$(echo "scale=2; $TOTAL_DURATION / $COUNT" | bc -l)
    
    # Find min/max throughput
    MIN_THROUGHPUT=${THROUGHPUTS[0]}
    MAX_THROUGHPUT=${THROUGHPUTS[0]}
    
    for val in "${THROUGHPUTS[@]}"; do
        if (( $(echo "$val < $MIN_THROUGHPUT" | bc -l) )); then
            MIN_THROUGHPUT=$val
        fi
        if (( $(echo "$val > $MAX_THROUGHPUT" | bc -l) )); then
            MAX_THROUGHPUT=$val
        fi
    done
    
    echo "signalsd Version: $SIGNALSD_VERSION"
    echo "Successful iterations: $COUNT/$ITERATIONS"
    echo ""
    echo "THROUGHPUT:"
    echo "  Average: ${AVG_THROUGHPUT} signals/sec"
    echo "  Range: ${MIN_THROUGHPUT} - ${MAX_THROUGHPUT} signals/sec"
    echo ""
    echo "LATENCY:"
    echo "  Average (mean): ${AVG_OF_AVG_LATENCY}ms"
    echo "  Average P95: ${AVG_P95_LATENCY}ms"
    echo "  Average P99: ${AVG_P99_LATENCY}ms"
    echo ""
    echo "DURATION:"
    echo "  Average test duration: ${AVG_DURATION} seconds"
    echo ""
    
    # Create summary CSV
    SUMMARY_FILE="$RESULTS_DIR/baseline_summary.csv"
    echo "version,iteration,throughput_signals_per_sec,avg_latency_ms,p95_latency_ms,p99_latency_ms,duration_sec" > "$SUMMARY_FILE"

    for i in $(seq 0 $((COUNT-1))); do
        echo "$SIGNALSD_VERSION,$((i+1)),${THROUGHPUTS[$i]},${AVG_LATENCIES[$i]},${P95_LATENCIES[$i]},${P99_LATENCIES[$i]},${TEST_DURATIONS[$i]}" >> "$SUMMARY_FILE"
    done
    
    echo "📁 Detailed results saved in: $RESULTS_DIR/"
    echo "📊 Summary CSV: $SUMMARY_FILE"
    
else
    echo "❌ No successful test iterations completed"
fi
}

# Output summary to both terminal and file
output_summary | tee "$SUMMARY_TEXT_FILE"

echo ""
echo "📄 Summary text file: $SUMMARY_TEXT_FILE"
