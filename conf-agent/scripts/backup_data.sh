#!/bin/bash

# cmd line: <cifs|nfs> <path> <user> <pass> 
# example: cifs \\192.168.1.1\share user pass123
#        : nfs 192.168.1.1:/nfs-share

TYPE=$1
FLDR=$2
USER=$3
PASS=$4

LOG="/var/log/qube/data-backup.log"

echo "$(date) - Starting backup" >> $LOG

if [ "$TYPE" == "cifs" ]
then
	mount.cifs $FLDR /mnt -o username=$USER,password="$PASS",rw || { echo "cannot mount cifs folder: $FLDR, User:  $USER"; exit 1; }
elif [ "$TYPE" == "nfs" ]
then
	mount $FLDR /mnt -o rw || { echo "cannot mount nfs folder: $FLDR"; exit 1; }
else
	echo "invalid mount type"
	exit 2
fi

START=`date +%s`
rsync -avu --delete /data/* /mnt   || { echo "Error encountered during backup"; umount -f /mnt; exit 3; }

sync
END=`date +%s`
let DIFF=END-START

umount -f /mnt

echo "$(date) - Backup completed in $DIFF seconds" >> $LOG
echo "Backup time: $DIFF seconds"
