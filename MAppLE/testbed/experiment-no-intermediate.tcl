set ns [new Simulator]
source tb_compat.tcl

set opt(DELAY_WORKAROUND) 1

set client [$ns node]
set server [$ns node]

tb-use-endnodeshaping $opt(DELAY_WORKAROUND)

# WIFI link
set link0 [$ns duplex-link $client $server 20.0Mb 15.0ms DropTail]
tb-set-link-loss $link0 0.001
set queue0 [[$ns link $client $server] queue]
$queue0 set limit_ 30

# LTE link
set link1 [$ns duplex-link $client $server 25.0Mb 25.0ms DropTail]
tb-set-link-loss $link1 0.005
set queue1 [[$ns link $client $server] queue]
$queue1 set limit_ 50

# long-fat link (10.1.1.0/24)
tb-set-ip-link $client $link0 10.1.1.1
tb-set-ip-link $server $link0 10.1.1.2

# short-skinny link (10.1.2.0/24)
tb-set-ip-link $client $link1 10.1.2.1
tb-set-ip-link $server $link1 10.1.2.2

tb-set-node-os $server UBUNTU18-64-STD
tb-set-node-os $client UBUNTU18-64-STD

# automatic routing
$ns rtproto Static

# go!
$ns run

