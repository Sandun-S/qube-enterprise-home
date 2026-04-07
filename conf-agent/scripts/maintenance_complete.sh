#!/bin/bash

cp /etc/fstab.keep /etc/fstab
cp /etc/crypttab.keep /etc/crypttab

if [ -f /etc/rc.local ]
then
	rm /etc/rc.local
fi

systemctl enable docker.service docker.socket mit-qube.service
sync

echo "$(date) - Maintenance task complete. Rebooting to switch to normal mode." >> /logs/maintenance.log
reboot
