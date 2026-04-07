#!/bin/bash

# should run maintenance_start.sh /mit-ro/scripts/backup_image.sh before running this

LOG="/logs/maintenance.log"

source /mit-ro/device.txt

echo "$(date) - Starting Image backup $RUN_MITRW -> $RUN_BACKUP" >> $LOG

START=`date +%s`
{ dd if=$RUN_MITRW of=$RUN_BACKUP bs=1M && cmp -b $RUN_MITRW $RUN_BACKUP; } >> $LOG 2>&1

END=`date +%s`
let DIFF=END-START

echo "$(date) - Backup completed in $DIFF seconds" >> $LOG

/mit-ro/scripts/maintenance_complete.sh >> $LOG
