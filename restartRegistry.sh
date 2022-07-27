#!/usr/bin/env bash

IFS='
'
cd -P "$(dirname "$0")"

# Stop and remove registry container
docker container stop registry && docker container rm registry

# Kill processes using port 8081 (registry service)
kill -9 $(lsof -t -i:8081)

# Re-create and run the registry contianer
docker run --detach --publish 5000:5000 --restart=unless-stopped --name registry --volume registry:/var/lib/registry registry:2.7.1

# Relaunch registry service
./registry -port 8081 2>log.txt &
