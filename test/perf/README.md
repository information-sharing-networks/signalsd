1. start with an empty database

```sh
cd signalsd

# remove any running signalsd app and singalsd db containers and volumes
docker compose down --rmi local -v --remove-orphans 
docker volume rm signalsd_db-data-perf


# env setup
export SECRET_KEY=perf-test-secret-key
export RATE_LIMIT_RPS=0

```
2. (A) 8GB postgress and single container app
```sh
docker compose -f docker-compose.perf-test.tuned.yml up -d
```

2. (B) 2GB postgress and single container app
```sh
docker compose -f docker-compose.perf-test.yml up -d
```

2. (C) large postgress and 4 app containers behind load balancer
```sh
docker compose -f docker-compose.perf-test.tuned.yml up db -d
 
cd test/perfdocker-compose.muti-app.yml up -
docker compose -f docker-compose.multi-app.yml up -d
```

3. set up db
```sh
bash test/perf/setup.sh
```

4. Run tests
```sh
# accounts send 10 requests eaach
# request payloads have 5 signals
# 50 concurrent users 
BATCH_SIZE=10 NUM_BATCHES=5 ./run-parallel-tests.sh 50
```


5. connect to db
```sh
docker exec -it signalsd-app-perf psql postgres://signalsd-dev@db:5432/signalsd_admin?sslmode=disable
```