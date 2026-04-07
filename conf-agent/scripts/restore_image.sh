#!/bin/bash

# should run maintenance_start.sh /mit-ro/scripts/restore_image.sh before running this

LOGS="/logs/maintenance.log"
source /mit-ro/device.txt

echo "Restore image $RUN_BACKUP to $RUN_MITRW" >> $LOGS

START=`date +%s`
{ dd if=$RUN_BACKUP of=$RUN_MITRW bs=1M && cmp -b $RUN_BACKUP $RUN_MITRW; } >> $LOGS

END=`date +%s`
let DIFF=END-START

echo "Restore time: $DIFF seconds" >> $LOGS

/mit-ro/scripts/maintenance_complete.sh >> $LOGS
