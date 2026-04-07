#!/bin/bash

# Cmd line: <interface> <ipv4/subnet> <geteway> <dns> <ssid> <passwd> <key-mgmnt>
# auto      $1          $2                            $3     $4       $5
# static    $1          $2            $3        $4    $5     $6       $7

source /mit-ro/device.txt

echo "$(date) - Setting WIFI settings" >> /var/log/qube/management.log

if [ "$WIFI_INT" == "" ]
then
	echo "This device has no wifi interface"
	exit 1
fi

mount -o remount,rw / || { echo "remounting 1 failed"; exit 1; }

rm /etc/netplan/wlan*.yaml

if [ "$2" == "auto" ]
then
	cp network/wlan-custom-dhcp.yaml /etc/netplan
	sed -i "s/INTF/$WIFI_INT/" /etc/netplan/wlan-custom-dhcp.yaml
	sed -i "s/WWIIFFII/$3/" /etc/netplan/wlan-custom-dhcp.yaml
	sed -i "s/PPAASS/$4/" /etc/netplan/wlan-custom-dhcp.yaml
	sed -i "s/KKYY/$5/" /etc/netplan/wlan-custom-dhcp.yaml
else
	cp network/wlan-custom-static.yaml /etc/netplan
	sed -i "s/INTF/$WIFI_INT/" /etc/netplan/wlan-custom-static.yaml
	sed -i "s^ADDR4^$2^" /etc/netplan/wlan-custom-static.yaml
	sed -i "s/DNS4/$4/" /etc/netplan/wlan-custom-static.yaml
	sed -i "s/GATE4/$3/" /etc/netplan/wlan-custom-static.yaml
	sed -i "s/WWIIFFII/$5/" /etc/netplan/wlan-custom-static.yaml
	sed -i "s/PPAASS/$6/" /etc/netplan/wlan-custom-static.yaml
	sed -i "s/KKYY/$7/" /etc/netplan/wlan-custom-static.yaml
fi

chmod 600 /etc/netplan/*.yaml

mount -o remount,ro /

echo "About to apply network configuartion. You may lose connectivity if the connecting interface is changed"

(sleep 2; netplan apply;) &

# The supported key management modes are none (no key management); 
# psk (WPA with pre-shared key, common for home Wi-Fi); 
# eap (WPA with EAP, common for enterprise Wi-Fi); 
# eap-sha256 (used with WPA3-Enterprise); 
# eap-suite-b-192 (used with WPA3-Enterprise); 
# sae (used by WPA3); 
# 802.1x (used primarily for wired Ethernet connections).
