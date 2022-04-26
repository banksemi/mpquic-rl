set ns [new Simulator]
source tb_compat.tcl

set opt(DELAY_WORKAROUND) 1

set client [$ns node]
set server [$ns node]
set intermediate0 [$ns node]
set intermediate1 [$ns node]

tb-use-endnodeshaping $opt(DELAY_WORKAROUND)

# long-fat link
set link0 [$ns duplex-link $client $intermediate0 10.0Mb 40.0ms DropTail]
tb-set-link-loss $link0 0.01
set queue0 [[$ns link $client $intermediate0] queue]
$queue0 set limit_ 40

# short-skinny link
set link1 [$ns duplex-link $client $intermediate1 5.0Mb 15.0ms DropTail]
tb-set-link-loss $link1 0.001
set queue1 [[$ns link $client $intermediate1] queue]
$queue1 set limit_ 1

# server is behind intermediate links
# intermediates <-> server
set ilink0 [$ns duplex-link $intermediate0 $server 1000.0Mb 0.0ms DropTail]
set ilink1 [$ns duplex-link $intermediate1 $server 1000.0Mb 0.0ms DropTail]

# long-fat link between client and intermediate0
tb-set-ip-link $client $link0 10.1.1.1
tb-set-ip-link $intermediate0 $link0 10.1.1.2

# short-skinny link between client and intermediate1
tb-set-ip-link $client $link1 10.1.2.1
tb-set-ip-link $intermediate1 $link1 10.1.2.2

# two regular links between server and intermediates
tb-set-ip-link $intermediate0 $ilink0 10.1.3.2
tb-set-ip-link $server $ilink0 10.1.3.3
tb-set-ip-link $intermediate1 $ilink1 10.1.4.2
tb-set-ip-link $server $ilink1 10.1.4.3

tb-set-node-os $server UBUNTU18-64-STD
tb-set-node-os $client UBUNTU18-64-STD
tb-set-node-os $intermediate0 UBUNTU18-64-STD
tb-set-node-os $intermediate1 UBUNTU18-64-STD

# automatic routing
$ns rtproto Static

# go!
$ns run

