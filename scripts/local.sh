#!/bin/bash

echo "-------------------------------------------- Starting local setup in 30 seconds"

sleep 30

while [ 1 ]
do
	ping -c 1 www.google.com

	if [ $? -eq 0 ]
	then
		break
	else
		echo "Waiting for network to continue installation"
		sleep 5
	fi
done

cd /mit
source /mit-ro/device.txt

if [ "$ENCRYPTION" -eq 1 ]
then
	# wait till /boot/firmware is mounted
	while [ ! -f $INITRD_FILE ] 
	do
		echo "$INITRD_FILE does not exist, waiting to be mounted"
		sleep 5
	done

	# overwrite the current initrd
	mkinitramfs -o $INITRD_FILE || { echo "Could not finish mkinitramfs"; exit; }
fi

# ----------------------------------------------- make root readonly
sed -i '/ \/ / s/defaults/defaults,ro/' /etc/fstab

# ----------------------------------------------- get rc-local.service to start after network-online.target
sed -i '/After=network.target/ s/$/ network-online.target/' /lib/systemd/system/rc-local.service

# ----------------------------------------------- set passwords
PW=$(cat /boot/mit.txt| grep maintain| cut -d ' ' -f 2)
echo -e "$PW\n$PW" | passwd menu

NUM=$(cat /boot/mit.txt| grep deviceid| cut -d '-' -f 2)
let NUM=NUM*2
NUM=$(echo $NUM |rev)	# qube id x 2 and reverse
PW="7349$NUM"
echo -e "$PW\n$PW" | passwd mit

PW="19730409$NUM"
echo -e "$PW\n$PW" | passwd root

# ----------------------------------------------- docker swarm setup
docker swarm init || { echo "******************************* docker init failed"; exit; }
docker network create --attachable --driver overlay qube-net

# ----------------------------------------------- setup data partition
echo "LABEL=data /data  ext4  defaults,discard,errors=remount-ro,nofail  0  2" >> /etc/fstab
mkfs.ext4 -F $RUN_DATA -L "data" || exit
mount -a || sleep 60

./install.sh
rm install.sh

cp /mit-ro/network/conf-agent.service /etc/systemd/system
cp /mit-ro/network/99-disable-network-config.cfg /etc/cloud/cloud.cfg.d
rm /etc/netplan/*.yaml
cp /mit-ro/network/eth-dhcp.yaml /etc/netplan
cp /mit-ro/network/qube-net.yaml /etc/netplan
netplan apply

systemctl enable conf-agent.service || { echo "Problem creating conf-agent.service"; exit; }

# ═══════════════════════════════════════════════════════════════════
# Qube Enterprise — additions (everything below this line is new)
# ═══════════════════════════════════════════════════════════════════

# ----------------------------------------------- Enterprise conf-agent binary
cp /mit-ro/network/enterprise-conf-agent /usr/local/bin/enterprise-conf-agent
chmod +x /usr/local/bin/enterprise-conf-agent

# ----------------------------------------------- Enterprise conf-agent config
mkdir -p /opt/qube
cat > /opt/qube/.env << ENV
TPAPI_URL=http://REPLACE_WITH_CLOUD_IP:8081
WORK_DIR=/opt/qube
POLL_INTERVAL=30
MIT_TXT_PATH=/boot/mit.txt
ENV

# ----------------------------------------------- Enterprise conf-agent service
cp /mit-ro/network/enterprise-conf-agent.service /etc/systemd/system/
systemctl enable enterprise-conf-agent.service || { echo "Problem creating enterprise-conf-agent.service"; exit; }

# ═══════════════════════════════════════════════════════════════════
# End of Enterprise additions
# ═══════════════════════════════════════════════════════════════════

figlet -Wc "Installtion Completed"

rm /etc/rc.local 

echo "local.sh completed" >> /mit/install.status
sync

echo "This Unit is now ready to be shipped"
read ENT
