# Multi-App Load Balancer Performance Testing

This directory contains everything needed to simulate Google Cloud autoscaling by running multiple app instances behind an nginx load balancer.

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────┐
│   Test Client   │───▶│  nginx (port 8081) │───▶│   App 1     │
│                 │    │  Load Balancer   │    │ (port 8082) │
└─────────────────┘    │                  │    └─────────────┘
                       │                  │    ┌─────────────┐
                       │                  │───▶│   App 2     │
                       │                  │    │ (port 8083) │
                       │                  │    └─────────────┘
                       │                  │    ┌─────────────┐
                       │                  │───▶│   App 3     │
                       │                  │    │ (port 8084) │
                       │                  │    └─────────────┘
                       │                  │    ┌─────────────┐
                       │                  │───▶│   App 4     │
                       │                  │    │ (port 8085) │
                       └──────────────────┘    └─────────────┘
                                │
                                ▼
                       ┌──────────────────┐
                       │   Shared DB      │
                       │ (port 15432)     │
                       └──────────────────┘
```

## Quick Start

1. **Start the environment:**
   ```bash
   cd test/perf
   ./run-multi-app.sh start
   ```

2. **Run performance tests:**
   ```bash
   ./run-multi-app.sh test
   ```

3. **Check health:**
   ```bash
   ./run-multi-app.sh health
   ```

4. **Stop the environment:**
   ```bash
   ./run-multi-app.sh stop
   ```

## Files

- `docker-compose.multi-app.yml` - Multi-container setup with load balancer
- `nginx.conf` - Load balancer configuration
- `run-multi-app.sh` - Management script for the environment
- `run_parallel_tests.sh` - Performance testing script (updated to use load balancer)

## Available Commands

- `./run-multi-app.sh start` - Start all containers
- `./run-multi-app.sh stop` - Stop all containers  
- `./run-multi-app.sh restart` - Restart all containers
- `./run-multi-app.sh status` - Show container status
- `./run-multi-app.sh logs` - Show all logs
- `./run-multi-app.sh logs-lb` - Show load balancer logs only
- `./run-multi-app.sh logs-app` - Show app logs only
- `./run-multi-app.sh test [parallel_instances]` - Run performance tests
- `./run-multi-app.sh health` - Check service health
- `./run-multi-app.sh clean` - Clean up everything

## Endpoints

- **Load Balancer:** http://localhost:8081
- **Individual Apps:**
  - App 1: http://localhost:8082
  - App 2: http://localhost:8083  
  - App 3: http://localhost:8084
  - App 4: http://localhost:8085

## Load Balancer Configuration

The nginx load balancer uses:
- **Round-robin** distribution (default)
- **Health checks** with automatic failover
- **Connection pooling** for better performance
- **Request/response logging** for debugging

## Performance Testing

The `run_parallel_tests.sh` script has been configured to:
- Use the load balancer endpoint by default (port 8081)
- Run multiple parallel test instances
- Collect detailed metrics including latency percentiles
- Show which app instances handled requests

### Running Tests

```bash
# Run with default 3 parallel instances
./run-multi-app.sh test

# Run with custom number of parallel instances
./run-multi-app.sh test 5

# Run directly with environment variable
PARALLEL_INSTANCES=8 ./run-multi-app.sh test
```

## Monitoring

### Check Load Distribution
```bash
# View nginx access logs to see request distribution
./run-multi-app.sh logs-lb
```

### Check App Performance
```bash
# View individual app logs
./run-multi-app.sh logs-app
```

### Health Monitoring
```bash
# Check all services
./run-multi-app.sh health

# Check load balancer status
curl http://localhost:8081/nginx-status
```

## Troubleshooting

1. **Services not starting:** Check if ports are available and database is running
2. **Load balancer not distributing:** Check nginx logs for upstream errors
3. **Performance issues:** Monitor individual app logs and resource usage
4. **Database connection issues:** Ensure shared database is running on port 15432
