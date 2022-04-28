#!/bin/bash
echo "Clearing delays on s1-eth1"
tc qdisc del dev s1-eth1 parent 5:1