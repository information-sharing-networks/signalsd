#!/bin/bash

# Multi-App Load Balancer Test Environment Manager
# Simulates GCP autoscaling with nginx load balancer

set -e

COMPOSE_FILE="docker-compose.multi-app.yml"
LOAD_BALANCER_URL="http://localhost:8081"

show_help() {
    echo "Multi-App Load Balancer Test Environment"
    echo "========================================"
    echo ""
    echo "Usage: $0 [COMMAND]"
    echo ""
    echo "Commands:"
    echo "  start     - Start all containers (load balancer + 2 app instances)"
    echo "  stop      - Stop all containers"
    echo "  restart   - Restart all containers"
    echo "  status    - Show container status"
    echo "  logs      - Show logs from all containers"
    echo "  logs-lb   - Show load balancer logs only"
    echo "  logs-app  - Show app container logs only"
    echo "  test      - Run performance test against load balancer"
    echo "  health    - Check health of all services"
    echo "  validate  - Validate nginx configuration"
    echo "  scale     - Scale app containers (usage: $0 scale 6)"
    echo "  clean     - Stop and remove all containers and volumes"
    echo ""
    echo "Load Balancer URL: $LOAD_BALANCER_URL"
    echo "Individual App URLs:"
    echo "  App 1: http://localhost:8082"
    echo "  App 2: http://localhost:8083"
}

start_services() {
    echo "🚀 Starting multi-app load balancer environment..."
    docker compose -f "$COMPOSE_FILE" up -d
    
    echo "⏳ Waiting for services to be healthy..."
    sleep 10
    
    check_health
    
    echo ""
    echo "✅ Environment started successfully!"
    echo "🔗 Load Balancer: $LOAD_BALANCER_URL"
    echo "📊 Run tests with: $0 test"
}

stop_services() {
    echo "🛑 Stopping multi-app environment..."
    docker compose -f "$COMPOSE_FILE" down
    echo "✅ Environment stopped"
}

restart_services() {
    echo "🔄 Restarting multi-app environment..."
    stop_services
    start_services
}

show_status() {
    echo "📊 Container Status:"
    docker compose -f "$COMPOSE_FILE" ps
}

show_logs() {
    echo "📋 Showing logs from all containers..."
    docker compose -f "$COMPOSE_FILE" logs -f
}

show_lb_logs() {
    echo "📋 Load Balancer logs:"
    docker compose -f "$COMPOSE_FILE" logs -f loadbalancer
}

show_app_logs() {
    echo "📋 App container logs:"
    docker compose -f "$COMPOSE_FILE" logs -f app1 app2
}

run_performance_test() {
    echo "🧪 Running performance test against load balancer..."
    BASE_URL="$LOAD_BALANCER_URL" ./run_parallel_tests.sh "$@"
}

check_health() {
    echo "🏥 Health Check Results:"
    echo "========================"
    
    # Check load balancer
    if curl -s -f "$LOAD_BALANCER_URL/health/live" > /dev/null; then
        echo "✅ Load Balancer: Healthy"
    else
        echo "❌ Load Balancer: Unhealthy"
    fi
    
    # Check individual apps
    for i in {1..2}; do
        port=$((8081 + i))
        if curl -s -f "http://localhost:$port/health/live" > /dev/null; then
            echo "✅ App $i: Healthy"
        else
            echo "❌ App $i: Unhealthy"
        fi
    done
    
    echo ""
    echo "🔗 Load Balancer Status:"
    curl -s "$LOAD_BALANCER_URL/nginx-status" || echo "Load balancer status unavailable"
}

scale_apps() {
    local target_scale=${1:-4}
    echo "⚖️  Scaling app containers to $target_scale instances..."
    
    # Note: This is a simplified version. For true scaling, you'd need to:
    # 1. Update the docker-compose.yml dynamically
    # 2. Update nginx.conf upstream configuration
    # 3. Reload nginx configuration
    
    echo "⚠️  Dynamic scaling not implemented yet."
    echo "Current setup supports 2 fixed app instances."
    echo "To change the number of instances, edit $COMPOSE_FILE and nginx.conf"
}

validate_nginx() {
    echo "🔍 Validating nginx configuration..."

    # Create a temporary config with localhost IPs for testing
    temp_config=$(mktemp)
    sed -e 's/app1:8080/127.0.0.1:8082/g' \
        -e 's/app2:8080/127.0.0.1:8083/g' \
        -e 's/app3:8080/127.0.0.1:8084/g' \
        -e 's/app4:8080/127.0.0.1:8085/g' \
        nginx.conf > "$temp_config"

    if docker run --rm -v "$temp_config:/etc/nginx/nginx.conf:ro" nginx:alpine nginx -t 2>/dev/null; then
        echo "✅ Nginx configuration is valid"
    else
        echo "❌ Nginx configuration has syntax errors"
        echo "ℹ️  Note: Host resolution errors are expected when testing outside Docker Compose"
    fi

    rm -f "$temp_config"
}

clean_environment() {
    echo "🧹 Cleaning up environment..."
    docker compose -f "$COMPOSE_FILE" down -v --remove-orphans
    echo "✅ Environment cleaned"
}

# Main command handling
case "${1:-help}" in
    start)
        start_services
        ;;
    stop)
        stop_services
        ;;
    restart)
        restart_services
        ;;
    status)
        show_status
        ;;
    logs)
        show_logs
        ;;
    logs-lb)
        show_lb_logs
        ;;
    logs-app)
        show_app_logs
        ;;
    test)
        shift
        run_performance_test "$@"
        ;;
    health)
        check_health
        ;;
    validate)
        validate_nginx
        ;;
    scale)
        scale_apps "$2"
        ;;
    clean)
        clean_environment
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        echo "❌ Unknown command: $1"
        echo ""
        show_help
        exit 1
        ;;
esac
