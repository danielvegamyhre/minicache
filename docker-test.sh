#!/bin/sh

# build client image
docker build -t cacheclient -f Dockerfile.client . || { echo "failed to build docker image for cache client"; exit 1; }

# spin up containerized cache servers using docker compose
docker-compose start || { echo "docker-compose start failed"; exit 1; }

# run tests in docker
docker run --network minicache_default cacheclient || { echo "failed to run docker image cacheclient"; exit 1; }

# stop cache server containers
docker-compose stop
