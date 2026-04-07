#!/bin/bash

# should run maintenance_start.sh /mit-ro/scripts/repair_fs.sh before running this

LOG_FILE="/logs/maintenance.log"

source /mit-ro/device.txt

echo "$(date) - Starting FS repair" >> $LOG_FILE

cryptsetup open $RUN_MITRW mit-rw -d /etc/part3.key >> $LOG_FILE 2>&1

if [ "$?" == "0" ]
then
	e2fsck -fp /dev/mapper/mit-rw >> $LOG_FILE 2>&1
else 
	echo "Could not repair firmware partition" >> $LOG_FILE
fi

e2fsck -fp $RUN_DATA >> $LOG_FILE 2>&1

echo "$(date) - Repair completed" >> $LOG_FILE
sync
/mit-ro/scripts/maintenance_complete.sh >> $LOG_FILE
