#!/bin/bash

#Usage: ip-reset.sh
cd /mit-ro

echo "$(date) - ip reset started" >> /var/log/qube/management.log

sudo mount -o remount,rw / || { echo "remounting 1 failed"; exit 1; }

sudo rm -f /etc/netplan/*.yaml
sudo cp network/eth-dhcp.yaml /etc/netplan
sudo cp network/qube-net.yaml /etc/netplan

sudo chmod 600 /etc/netplan/*.yaml

sudo mount -o remount,ro / 

echo "Task completed"

(sleep 2; netplan apply;) >> /var/log/qube/management.log 2>&1 &
