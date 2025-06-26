1. start with an empty database

```sh

cd test/perf

# remove any running signalsd app and singalsd db containers and volumes
docker compose down --rmi local -v --remove-orphans 
docker volume rm signalsd_db-data-perf
```


# env setup
```sh
export SECRET_KEY=perf-test-secret-key
export RATE_LIMIT_RPS=0
```


2. (A) 4cpu x 8GB postgress and single container app
```sh
docker compose -f docker-compose.perf-test-db.tuned.yml up -d
```

2. (B) 2cpu x 2GB postgress and single container app
```sh
docker compose -f docker-compose.perf-test-db.yml up -d
```

3. (A) 4 app containers behind load balancer
```sh
    # if running for the first time, bring the admin container up first so it runs the db migration
    docker compose -f docker-compose.mult-app.yml up admin
    # then bring up the remaining containers
    docker compose -f docker-compose.multi-app.yml up signal1 signals2 signals3
    # finally start the load balancer
    docker compose -f docker-compose.multi-app.yml up loadbalancer

    # otherwise bring them all up at once
    docker compose -f docker-compose.multi-app.yml up -d
```

3. (A) 4 app containers behind load balancer - use precompiled binary (to test specific versions)
```sh
docker compose -f docker-compose.multi-app-use-binary.yml up  admin -d
```

4. set up db
```sh
bash setup.sh
```

5. Run tests
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