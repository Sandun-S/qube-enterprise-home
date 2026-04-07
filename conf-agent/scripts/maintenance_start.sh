#!/bin/bash

# Usage: maintenance_start.sh <rc.local file>

LOG_FILE="/logs/maintenance.log"
RC_LOCAL=$1

if [ "$RC_LOCAL" == "" ]
then
	echo "no command line parameter"
	exit
fi

mount -o remount,rw / || { echo "remounting 1 failed"; exit 1; }

echo "preparing to reboot"

if [ ! -f /etc/fstab.keep ]
then
	cp /etc/fstab /etc/fstab.keep
fi

if [ ! -f /etc/crypttab.keep ]
then
	        cp /etc/crypttab /etc/crypttab.keep
fi

rm /etc/crypttab

cat /etc/fstab.keep | grep -v -e 'mit-rw' -e '/data' | sed '/[\t ]\/[\t ]/ s/,ro/,rw/' > /etc/fstab

cp $RC_LOCAL /etc/rc.local
chmod +x /etc/rc.local

systemctl disable docker.service docker.socket mit-qube.service
sync

echo "$(date) - Starting maintaninace $RC_LOCAL" >> $LOG_FILE

echo "rebooting..."
exit 99			# send result to conf-api and reboot
