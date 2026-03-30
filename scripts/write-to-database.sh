#!/bin/bash

# this has to be run from a pen drive. Insert the SD card after booting the pendrive for raspberry devices (if you want to run the device off SD card)
# copy all files required to run the system (should we include the docker images as well ?)
# copy /etc/rc.local file, so first time boot will install docker and other things.

if [ `whoami` != "root" ]
then
	echo "Need to run as root"
	echo "Usage sudo ./write-to-db.sh <HOST_NAME> <REGISTRATION_KEY> <MAINTENANCE_KEY>"
	exit
fi

if [ "$3" == "" ]
then
	echo "Arguments required"
	echo "Usage sudo ./write-to-db.sh <HOST_NAME> <REGISTRATION_KEY> <MAINTENANCE_KEY>"
	exit
fi

DHOST=$1
REG=$2
MNTN=$3

## write to DB
echo "------------------------------------------- inserting new device data to database"

CMD="insert into Devices (device_id, device_name, reg_number, mntn_key, arch) values ('$DHOST', '$DHOST', '$REG', '$MNTN', 'arm64');"
echo "SQL: $CMD"
ssh ora-test "mysql -u device -pdevice qube -e \"$CMD\""

CMD="insert into Device_Commands (device_id, command, parameters, data_file, status) values ('$DHOST', 'scripts/get_info.sh', '', '', 0);" 
echo "SQL: $CMD"
ssh ora-test "mysql -u device -pdevice qube -e \"$CMD\""

CMD="insert into FW_Rules (device_id, protocol, network, port) values ('$DHOST', 'TCP', '0', 22), ('$DHOST', 'TCP', '0', 8080), ('$DHOST', 'UDP', '0', 5353);"
echo "SQL: $CMD"
ssh ora-test "mysql -u device -pdevice qube -e \"$CMD\""

echo "------------------------------------------- db write completed"

## ── Qube Enterprise registration (added alongside existing Qube Lite insert) ──
## Inserts the same device into Enterprise Postgres so it's ready for customer claiming.
## ENTERPRISE_DB_HOST must be set in environment (or defaults below).
EHOST="${ENTERPRISE_DB_HOST:-cloud-vm:5432}"
EUSER="${ENTERPRISE_DB_USER:-qubeadmin}"
EPASS="${ENTERPRISE_DB_PASS:-qubepass}"
EDBNAME="${ENTERPRISE_DB_NAME:-qubedb}"

echo "------------------------------------------- inserting device into Enterprise Postgres"
PGPASSWORD=$EPASS psql -h ${EHOST%:*} -p ${EHOST#*:} -U $EUSER -d $EDBNAME \
  -c "INSERT INTO qubes (id, register_key, maintain_key, device_type, status)
      VALUES ('$DHOST', '$REG', '$MNTN', 'arm64', 'pending')
      ON CONFLICT (id) DO NOTHING;" 2>&1 \
  && echo "Enterprise DB: device registered" \
  || echo "Enterprise DB: insert failed (check ENTERPRISE_DB_HOST)"

PGPASSWORD=$EPASS psql -h ${EHOST%:*} -p ${EHOST#*:} -U $EUSER -d $EDBNAME \
  -c "INSERT INTO config_state (qube_id) VALUES ('$DHOST') ON CONFLICT DO NOTHING;" 2>&1
echo "------------------------------------------- Enterprise DB write completed"
