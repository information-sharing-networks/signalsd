# Multi-App Load Balancer Performance Testing

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

# DB config
### perf - default postgres
```sql
ALTER SYSTEM SET shared_buffers = '128MB';        
ALTER SYSTEM SET effective_cache_size = '4GB';       
ALTER SYSTEM SET work_mem = '4MB';                   
ALTER SYSTEM SET maintenance_work_mem = '64MB';      
```

### Neon free tier (8GB+ RAM, 2 CPUs)
```sql
ALTER SYSTEM SET shared_buffers = '230MB';
ALTER SYSTEM SET effective_cache_size = '6553MB';
ALTER SYSTEM SET max_wal_size = '1GB';
ALTER SYSTEM SET work_mem = '4MB';
```

## Available Commands

- `./run-multi-app.sh stop` - Stop all containers  
- `./run-multi-app.sh restart` - Restart all containers
- `./run-multi-app.sh status` - Show container status
- `./run-multi-app.sh logs` - Show all logs
- `./run-multi-app.sh logs-lb` - Show load balancer logs only
- `./run-multi-app.sh logs-app` - Show app logs only
- `./run-multi-app.sh test [parallel_instances]` - Run performance tests
- `./run-multi-app.sh health` - Check service health
- `./run-multi-app.sh clean` - Clean up everything


## Load Balancer Configuration

The nginx load balancer uses:
- **Round-robin** distribution (default)
- **Health checks** with automatic failover
- **Connection pooling** for better performance
- **Request/response logging** for debugging


### Running Tests

```bash

# make sure the perf database is running
cd signalsd
docker compose -f docker-compose.perf-test.yml up -d db

# batch size = number of signals in each request
cd test/perf
BATCH_SIZE=1 NUM_BATCHES=50 ./run-multi-app.sh test 150
```