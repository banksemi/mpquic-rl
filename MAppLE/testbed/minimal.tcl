set ns [new Simulator]
source tb_compat.tcl

set client [$ns node]
set server [$ns node]

# gigabit link
set link [$ns duplex-link $client $server 1000.0Mb 0.0ms DropTail]

# link with delay node
#set link [$ns duplex-link $client $server 10.0Mb 40.0ms DropTail]
#tb-set-link-loss $link 0.01

tb-set-node-os $server UBUNTU18-64-STD
tb-set-node-os $client UBUNTU18-64-STD

$ns rtproto Static
$ns run

