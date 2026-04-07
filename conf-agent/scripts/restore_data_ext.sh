
LOG_FILE="/logs/maintenance.log"

if [ $TYPE=="cifs" ]
then
	{ mount.cifs $FLDR /mnt -o username=$USER,password="$PASS",rw || { echo "cannot mount cifs folder: $FLDR, User:  $USER"; exit 2; } ; } >> $LOG_FILE
elif [ $TYPE=="nfs" ]
then
	{ mount $FLDR /mnt -o rw || { echo "cannot mount nfs folder: $FLDR"; exit 2; } ; } >> $LOG_FILE
else
        echo "invalid mount type" >> $LOG_FILE
        exit 3
fi

START=`date +%s`
{ rsync -av --delete /mnt/* /data   || { echo "Error encountered during restore"; umount -f /mnt; exit 4; } ; } >> $LOG_FILE

sync
END=`date +%s`
let DIFF=END-START

umount -f /mnt >> $LOG_FILE

echo "Restore time: $DIFF seconds" >> $LOG_FILE

/mit-ro/scripts/maintenance_complete.sh >> $LOG_FILE
