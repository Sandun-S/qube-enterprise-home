#!/bin/bash

# Cmd line: <trusted-net-ports>
# eg: trusted_reset.sh "tcp:122.255.48.0/24:0,tcp:10.0.0.0/8:1883,tcp:0:8080"

cd /mit-ro

echo "$(date) - Setting firewall rules" >> /var/log/qube/management.log

ARGS=($(echo $1 |sed 's/,/\n/g'))

mount -o remount,rw / || { echo "remounting 1 failed"; exit 1; }

# remove all existing in INPUT and DOCKER-USER chains
iptables -t filter -F INPUT
iptables -t filter -F DOCKER-USER

# insert the initial ones
iptables -A INPUT -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
iptables -A INPUT -p vrrp -j ACCEPT
iptables -A INPUT -i lo -j ACCEPT
iptables -A INPUT -i docker0 -j ACCEPT
iptables -A INPUT -i docker_gwbridge -j ACCEPT

iptables -A DOCKER-USER -m conntrack --ctstate RELATED,ESTABLISHED -j RETURN
iptables -A DOCKER-USER -i lo -j RETURN
iptables -A DOCKER-USER -i docker0 -j RETURN
iptables -A DOCKER-USER -i docker_gwbridge -j RETURN

# create new /etc/iptables/rules.v4 file
cp network/base-rules.v4 /etc/iptables/rules.v4

# add trusted networks
for NN in ${ARGS[@]}
do
	ARR=($(echo $NN | sed 's/:/\n/g'))
	PROTO="${ARR[0]}"
	NET="${ARR[1]}"
	PORT="${ARR[2]}"

	VALID=0

	if [ "$NET" == "0" ]
	then
		NET=""
	else
		NET="-s $NET"
		VALID=1
	fi

	if [ "$PORT" == "0" ]
	then
		PORT=""
	else
		PORT="--dport $PORT"
		VALID=1
	fi

	if [ $VALID -eq 1 ]
	then
		echo "-A INPUT -p $PROTO $NET $PORT -j ACCEPT" >> /etc/iptables/rules.v4
		echo "-A DOCKER-USER -p $PROTO $NET $PORT -j RETURN" >> /etc/iptables/rules.v4

		iptables -A INPUT -p $PROTO $NET $PORT -j ACCEPT
		iptables -A DOCKER-USER -p $PROTO $NET $PORT -j RETURN
	fi

done

# insert the final rows
echo "-A DOCKER-USER -j DROP" >> /etc/iptables/rules.v4
echo "COMMIT" >> /etc/iptables/rules.v4
iptables -A DOCKER-USER -j DROP

# remount the root into ro mode
mount -o remount,ro /

