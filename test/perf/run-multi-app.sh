#!/bin/bash

# Multi-App Load Balancer Test Environment Manager

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
    BASE_URL="$LOAD_BALANCER_URL" ./run-parallel-tests.sh "$@"
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
