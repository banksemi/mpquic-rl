#!/usr/bin/env bash

echo "Clearing delays on client-eth0"
tc qdisc del dev client-eth0 parent 5:1