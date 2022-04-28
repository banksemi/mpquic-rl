#!/usr/bin/env bash

tc qdisc add dev s1-eth1 root handle 5:0 hfsc default 1
tc class add dev s1-eth1 parent 5:0 classid 5:1 hfsc sc rate 30Mbit ul rate 30Mbit
tc qdisc add dev s1-eth2 root handle 5:0 hfsc default 1
tc class add dev s1-eth2 parent 5:0 classid 5:1 hfsc sc rate 50Mbit ul rate 50Mbit
# Base delay
tc qdisc add dev s1-eth2 parent 5:1 netem loss 1.56% delay 13ms 1ms