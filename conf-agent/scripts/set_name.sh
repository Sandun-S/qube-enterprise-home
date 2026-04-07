#!/bin/bash

#Usage: set_name.sh <new-device-name>

echo "$(date) - Set new device name" >> /var/log/qube/management.log

if [ "$1" == "" ]
then
	echo "invalid hostname"
	exit 2
fi

mount -o remount,rw / || { echo "remounting 1 failed"; exit 1; }

sed -i "s/^.*host-name=.*/host-name=$1/" /etc/avahi/avahi-daemon.conf || exit 1
sed -i "s/devicename:.*/devicename: $1/" /boot/mit.txt

mount -o remount,ro / || { echo "remounting 2 failed"; exit 1; }

systemctl restart avahi-daemon.service

echo "set name completed"
