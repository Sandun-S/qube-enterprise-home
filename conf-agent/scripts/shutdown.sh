#!/bin/bash

#Usage: reboot.sh
cd /mit-ro

echo "$(date) - Shutting down device now..." >> /var/log/qube/management.log
echo "Shutting down device now..."

exit 98
