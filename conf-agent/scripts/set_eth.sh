#!/bin/bash

# Cmd line: <interface> <ipv4/subnet> <geteway> <dns>
#           $1          $2            $3        $4

echo "$(date) - Setting ethernet settings" >> /var/log/qube/management.log
source /mit-ro/device.txt

if [ "$WIRED_INT" == "" ]
then
	echo "This device has no wired interface"
	exit 1
fi

mount -o remount,rw / || { echo "remounting 1 failed"; exit 1; }

rm /etc/netplan/eth*.yaml

if [ "$2" == "auto" ]
then
	cp network/eth-dhcp.yaml /etc/netplan
	sed -i "s/eth0:/$WIRED_INT:/" /etc/netplan/eth-dhcp.yaml
else
	cp network/eth-static.yaml /etc/netplan
	sed -i "s/INTF/$WIRED_INT/" /etc/netplan/eth-static.yaml
	sed -i "s^ADDR4^$2^" /etc/netplan/eth-static.yaml
	sed -i "s/DNS4/$4/" /etc/netplan/eth-static.yaml
	sed -i "s/GATE4/$3/" /etc/netplan/eth-static.yaml
fi

chmod 600 /etc/netplan/*.yaml

mount -o remount,ro /

echo "About to apply network configuartion. You may lose connectivity if the connecting interface is changed"
(sleep 2; netplan apply;) &
