set ns [new Simulator]
source tb_compat.tcl

set client [$ns node]
set server [$ns node]
set intermediate [$ns node]

# long-fat link
set link0 [$ns duplex-link $client $intermediate 10.0Mb 40.0ms DropTail]
tb-set-link-loss $link0 0.01
# queue size must be set to 40

# short-skinny link
set link1 [$ns duplex-link $client $intermediate 5.0Mb 15.0ms DropTail]
tb-set-link-loss $link1 0.001
# queue size must be set to 1

# server is behind a third, intermediate link
# intermediate <-> server
set ilink0 [$ns duplex-link $intermediate $server 1000.0Mb 0.0ms DropTail]
set ilink1 [$ns duplex-link $intermediate $server 1000.0Mb 0.0ms DropTail]

# long-fat link between client and intermediate
tb-set-ip-link $client $link0 10.1.1.1
tb-set-ip-link $intermediate $link0 10.1.1.2

# short-skinny link between client and intermediate
tb-set-ip-link $client $link1 10.1.2.1
tb-set-ip-link $intermediate $link1 10.1.2.2

# two regular links between server and intermediate
tb-set-ip-link $intermediate $ilink0 10.1.3.2
tb-set-ip-link $server $ilink0 10.1.3.3
tb-set-ip-link $intermediate $ilink1 10.1.4.2
tb-set-ip-link $server $ilink1 10.1.4.3

tb-set-node-os $server UBUNTU18-64-STD
tb-set-node-os $client UBUNTU18-64-STD
tb-set-node-os $intermediate UBUNTU18-64-STD

$ns rtproto Static
$ns run

