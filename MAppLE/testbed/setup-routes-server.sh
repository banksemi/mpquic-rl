#!/bin/bash

echo Using IF $1 for 10.1.3.3
echo Using IF $2 for 10.1.4.3

ip rule add from 10.1.3.3 table 1
ip rule add from 10.1.4.3 table 2

ip route add 10.1.3.0/24 dev $1 scope link table 1
ip route add default via 10.1.3.2 dev $1 table 1

ip route add 10.1.4.0/24 dev $2 scope link table 2
ip route add default via 10.1.4.2 dev $2 table 2
