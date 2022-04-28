set ns [new Simulator]
source tb_compat.tcl

set client [$ns node]
set server [$ns node]

# long-fat link
set link0 [$ns duplex-link $client $server 10.0Mb 40.0ms DropTail]
tb-set-link-loss $link0 0.01
# queue size must be set to 40

# short-skinny link
set link1 [$ns duplex-link $client $server 5.0Mb 15.0ms DropTail]
tb-set-link-loss $link1 0.001
# queue size must be set to 1

# long-fat link between client and intermediate
tb-set-ip-link $client $link0 10.1.1.1
tb-set-ip-link $server $link0 10.1.1.2

# short-skinny link between client and intermediate
tb-set-ip-link $client $link1 10.1.2.1
tb-set-ip-link $server $link1 10.1.2.2

tb-set-node-os $server UBUNTU18-64-STD
tb-set-node-os $client UBUNTU18-64-STD

$ns rtproto Static
$ns run
