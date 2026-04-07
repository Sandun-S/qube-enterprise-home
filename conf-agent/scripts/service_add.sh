#!/bin/bash

# prerequisites (per service)
# following files need to be available
# 	qube/install/service_type:version.tar.gz 	- full package with folder structure, image and compose file
# 	qube/install/service_name.file_name.config	- configuration file for the new service
#	qube/install/service_name.file_name.params	- params file for the new service (optional)
#	qube/install/service_name.folder_name.tar.gz.folder    - additional custom folder. this can be tar.gz or zip file

# Usage: service_add.sh <service_name> <service_type> <version> <ports>

LOG_FILE="/var/log/qube/service-mgmt.log"

echo "$(date) - Service Add started" >> $LOG_FILE

NAME=$1		# name of the service
TYPE=$2		# type of the service
VERSION=$3	# version of the service
PORTS=$4
TGZ_FILE="$NAME.tar.gz"

if [ "$VERSION" == "" ]
then
	echo "Incomplete command line" >> $LOG_FILE
	exit 1
fi

cd /mit/qube

if [ -d "$NAME" ]
then
	echo "service already exist" >> $LOG_FILE
	exit 2
fi

# create the service folder
mkdir -p "$NAME"

if [ ! -f "install/$TGZ_FILE" ]
then
	echo "Service file install/$TGZ_FILE does not exist" >> $LOG_FILE
	exit 3
fi

# un-tar content into new service folder
tar -xzvf "install/$TGZ_FILE" -C "$NAME" || { echo "Could not un-tar install/$TGZ_FILE"; exit 4; } >> $LOG_FILE

IMAGE=`ls -1 $NAME/*.image`
COMPOSE=`ls -1 $NAME/*.compose`
LINK=`ls -1 $NAME/link`
CONFIG=`ls -1 install/$NAME.*.config`
PARAMS=`ls -1 install/$NAME.*.params`
FOLDER=`ls -1 install/$NAME.*.folder`
CONFIG2=""
PARAMS2=""
FOLDER2=""

if [ "$COMPOSE" == "" ]
then
	echo "Missing compose file" >> $LOG_FILE
	exit 4
fi

if [ "$CONFIG" != "" ]
then
	CONFIG2=`echo $CONFIG |sed -e "s|^install/$NAME\.||" -e "s|\.config$||"`
	mv $CONFIG $NAME/$CONFIG2	# move the config file into service-name folder
	chmod 777 $NAME/$CONFIG2
fi

if [ "$PARAMS" != "" ]
then
	PARAMS2=`echo $PARAMS |sed -e "s|^install/$NAME\.||" -e "s|\.params$||"`
	# update the file name in COMPOSE file
	sed -i "s|PARAMS_FILE|$PARAMS2|g" $COMPOSE || { echo "Could not update Params file name in compose file"; exit; } >> $LOG_FILE 

	mv $PARAMS $NAME/$PARAMS2	# move the params file into service-name folder
	chmod 777 $NAME/$PARAMS2
fi

if [ "$FOLDER" != "" ]
then
	FOLDER2=`echo $FOLDER |sed -e "s|^install/$NAME\.||" -e "s|\.folder$||"`	# remove additional name padding
	FOLDER3=`echo $FOLDER2 |sed -e "s|\.tar\.gz$||" -e "s|\.zip$||"`	# remove .tar.gz or .zip part

	# update the file name in COMPOSE file
	sed -i "s|FOLDER|$FOLDER3|g" $COMPOSE || { echo "Could not update folder name in compose file"; exit; } >> $LOG_FILE 

	mv $FOLDER $NAME/$FOLDER2

	ZIP=`echo $FOLDER2 |sed "s/$FOLDER3\.//"`

	echo "Folder details $FOLDER : $FOLDER2 : $FOLDER3 : $ZIP"

	if [ "$ZIP" == "zip" ]
	then
		unzip $NAME/$FOLDER2 -d $NAME
	elif [ "$ZIP" == "tar.gz" ]
	then
		tar -xzvf $NAME/$FOLDER2 -C $NAME
	fi

	rm $NAME/$FOLDER2
fi

sed -i '/FOLDER/d' $COMPOSE # remove folder entry if no folder

# remove if there are no volumes
LINES=`grep -A 1 'volumes:' $COMPOSE  |grep . |wc -l`

if [ "$LINES" -lt 2 ]
then
	sed -i '/volumes:/d' $COMPOSE
fi

chown -R root:root "$NAME"
chmod -R 777 "${NAME}/*"

if [ "$IMAGE" != "" ]		# if this is a public image, no need to upload
then
	echo "loading image $IMAGE" >> $LOG_FILE
	docker image load -i $IMAGE || { echo "docker image load failed - $IMAGE"; exit 5; } >> $LOG_FILE
	rm $IMAGE
else
	IMAGE=`grep 'image: ' $COMPOSE | awk '{print $2}'`
	docker pull $IMAGE || { echo "docker pull failed - $IMAGE"; exit 5; } >> $LOG_FILE
fi

if [ "$LINK" != "" ]
then
	mv $NAME /data/$NAME
	ln -s /data/$NAME $NAME
	rm $LINK
fi

sed -i "s/SERVICE_NAME/$NAME/g" $COMPOSE
sed -i "s/IMAGE_NAME/$IMAGE/g" $COMPOSE
sed -i "s/CONF_FILE/$CONFIG2/g" $COMPOSE
sed -i "s/PARAMS_FILE/$PARAMS2/g" $COMPOSE

if [ "$PORTS" != "" ]		# if there are ports available
then
	echo "configuring ports $PORTS" >> $LOG_FILE
	P=`echo "$PORTS" |awk -F ',' '{sep="\""; for (i=1; i<=NF; i++) {printf("%s%s\"",sep,$i); sep=",\""}}'`
	sed -i "s/PORTS/$P/" $COMPOSE
else
	sed -i '/PORTS/d' $COMPOSE  # remove line if there are no PORTS
fi

mv $COMPOSE "services/${NAME}_compose.yml"	# move the compose file from service_name folder to services folder while renaming to the standard


if [ -f "$NAME/init.sh" ]
then
	echo "About to run $NAME/init.sh" >> $LOG_FILE
	cd $NAME
	./init.sh >> $LOG_FILE
	rm init.sh
	cd ..
fi

echo "$NAME" >> services/services.txt

services/generate-compose.sh

rm "install/$NAME.*"

./start.sh >> $LOG_FILE 2>&1

