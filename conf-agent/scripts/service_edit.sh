#!/bin/bash

# prerequisites (per service)
# following files need to be available
# 	qube/services/service_name.file_name.config	- configuration file for the service edit
#	qube/services/service_name.file_name.params	- params file for the service edit

# Usage: service_edit.sh <service_name> <ports>

LOG_FILE="/var/log/qube/service-mgmt.log"

echo "$(date) - Service edit started" >> $LOG_FILE

NAME="$1"		# name of the service
PORTS="$2"

cd /mit/qube

if [ ! -d $NAME ]
then
	echo "service not found" >> $LOG_FILE
	exit 2
fi

CONFIG=`ls -1 install/$NAME.*.config`
PARAMS=`ls -1 install/$NAME.*.params`
FOLDER=`ls -1 install/$NAME.*.folder`
COMPOSE="services/${NAME}_compose.yml"

if [ "$CONFIG" != "" ]
then
	echo "updating config file" >> $LOG_FILE
	CONFIG2=`echo $CONFIG |sed -e "s|^install/$NAME\.||" -e "s|\.config$||"`
	mv $CONFIG $NAME/$CONFIG2	# move the config file into service-name folder 
	# cannot change the name, we don't do anything to the compose file
fi

if [ "$PARAMS" != "" ]
then
	echo "updating parameter file" >> $LOG_FILE
	PARAMS2=`echo $PARAMS |sed -e "s|^install/$NAME\.||" -e "s|\.params$||"`
	mv $PARAMS $NAME/$PARAMS2	# move the params file into service-name folder
	# cannot change the name, we don't do anything to the compose file

	echo "$PARAMS2" |grep '\.gz$' > /dev/null

	if [ $? -eq 0 ]
	then
		gzip -df $NAME/$PARAMS2
	fi
fi

if [ "$FOLDER" != "" ]
then
	FOLDER2=`echo $FOLDER |sed -e "s|^install/$NAME\.||" -e "s|\.folder$||"`
	FOLDER3=`echo $FOLDER2 |sed -e "s|\.tar\.gz$||" -e "s|\.zip$||"`        # remove .tar.gz or .zip part

	mv $FOLDER $NAME/$FOLDER2

	ZIP=`echo $FOLDER2 |sed "s/$FOLDER3\.//"`

	echo "Folder compression $ZIP" >> $LOG_FILE

	if [ $ZIP == "zip" ]
	then
		unzip $NAME/$FOLDER2 -d $NAME
	elif [ $ZIP == "tar.gz" ]
	then
		tar -xzvf $NAME/$FOLDER2 -C $NAME
	fi

	rm $NAME/$FOLDER2
	# cannot change the name, we don't do anything to the compose file
fi

SERVICE="qube_$NAME"

if [ "$PORTS" != "" ]
then
	echo "updating ports" >> $LOG_FILE

	P=`echo "$PORTS" |awk -F ',' '{sep="\""; for (i=1; i<=NF; i++) {printf("%s%s\"",sep,$i); sep=",\""}}'`

	sed -i "/ports:/ s/ports:.*/ports: [$P]/" $COMPOSE
	services/generate-compose.sh

	docker service rm $SERVICE
	./start.sh >> $LOG_FILE 2>&1
else
	echo "restarting service" >> $LOG_FILE

	for DD in `docker ps |grep -w $SERVICE |cut -d ' ' -f 1`
	do
		docker stop $DD || echo "docker stop failed - $DD"
	done
fi

rm "install/$NAME.*"

