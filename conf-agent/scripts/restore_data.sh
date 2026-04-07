#!/bin/bash

# cmd line: <cifs|nfs> <path> <user> <pass>

cd /mit-ro

echo "Restore data"

echo "#!/bin/bash" > /tmp/restore.sh
echo "TYPE=\"$1\"" >> /tmp/restore.sh
echo "FLDR=\"$2\"" | sed 's/\\/\\\\/g' >> /tmp/restore.sh
echo "USER=\"$3\"" >> /tmp/restore.sh
echo "PASS=\"$4\"" >> /tmp/restore.sh

cat scripts/restore_data_ext.sh >> /tmp/restore.sh

scripts/maintenance_start.sh /tmp/restore.sh
