#!/bin/bash

# Usage: set_timezone.sh "Asia/Colombo"
# use timedatectl list-timezones   to get list of timezones

echo "$(date) - Setting timezone" >> /var/log/qube/management.log

if [ "$1" == "" ]
then
	echo "Please provide timezone"
	exit 1
fi

mount -o remount,rw / || { echo "remounting 1 failed"; exit 1; }

timedatectl set-timezone $1 || exit 2

mount -o remount,ro /
echo "Please reboot device to take effect"
