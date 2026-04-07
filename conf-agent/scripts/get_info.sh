#!/bin/bash

source /mit-ro/device.txt

if [ "$WIRED_INT" != "" ]
then
	ip addr show dev "$WIRED_INT" |awk '{if ($1=="link/ether") print "eth_mac:",$2; if ($1=="inet") print "eth_ipv4:", $2; if ($1=="inet6") print "eth_ipv6:", $2}'
else 
	echo "eth_mac: no-device"
fi
if [ "$WIFI_INT" != "" ]
then
	ip addr show dev "$WIFI_INT" |awk '{if ($1=="link/ether") print "wlan_mac:",$2; if (($1=="inet") && (index($0,"secondary") == 0)) print "wlan_ipv4:", $2; if ($1=="inet6") print "wlan_ipv6:", $2}'
	iwgetid | awk '{split($2, a,"\""); print "wlan_ssid:",a[2];}'
else
	echo "wlan_mac: no-device"
fi

grep '^devicename:' /boot/mit.txt |sed 's/^devicename:/device_name:/'

P=`docker ps |awk '{print $(NF-1)}' |grep -o '[0-9]*' |tr '\n' ',' |sed 's/,$/\n/'`
echo "open_ports: $P"
exit 0
