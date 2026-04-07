#!/bin/bash

# Remove the service from docker and remove its configuration direcotries
# Usage: rm_service.sh <service_name>

LOG_FILE="/var/log/qube/service-mgmt.log"

echo "$(date) - Service remove started" >> $LOG_FILE
NAME=$1

if [ "$NAME" == "" ]
then
	echo "Invalid command line" >> $LOG_FILE
	exit 1
fi

cd /mit/qube

echo "shutting down service" >> $LOG_FILE
docker service rm "qube_$NAME"

echo "change the startup files" >> $LOG_FILE
sed -i  "/^${NAME}$/d" services/services.txt


echo "removing configuration files" >> $LOG_FILE
rm "services/${NAME}_compose.yml"
rm -rf "$NAME"

if [ -d "/data/$NAME" ]
then
	rm -rf "/data/$NAME"
fi

services/generate-compose.sh
