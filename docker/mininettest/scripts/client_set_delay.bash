#!/usr/bin/env bash

./scripts/client_clear_delay.bash
echo "Setting delay "$1" for client-eth0"
tc qdisc add dev client-eth0 parent 5:1 netem loss 0% delay $1ms 1ms
