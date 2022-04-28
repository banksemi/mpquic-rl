set ns [new Simulator]
source tb_compat.tcl

set opt(DELAY_WORKAROUND) 1
set opt(MOONGEN_HARDWARE) pc2666c6n10g
set opt(NODE_HARDWARE) pc2260

set server [$ns node]
set intermediate [$ns node]
set moongenA [$ns node]
set moongenB [$ns node]
set client [$ns node]

tb-set-hardware $moongenA $opt(MOONGEN_HARDWARE)
tb-set-hardware $moongenB $opt(MOONGEN_HARDWARE)
tb-set-hardware $server $opt(NODE_HARDWARE)
tb-set-hardware $client $opt(NODE_HARDWARE)
tb-set-hardware $intermediate $opt(NODE_HARDWARE)

tb-use-endnodeshaping $opt(DELAY_WORKAROUND)

# server to intermediate
set link12 [$ns duplex-link $server $intermediate 1.0Gb 0.0ms DropTail]
tb-set-ip-link $server $link12 10.1.1.2
tb-set-ip-link $intermediate $link12 10.1.1.3

# intermediate over network
set link2da [$ns duplex-link $intermediate $moongenA 1.0Gb 0.0ms DropTail]
tb-set-ip-link $intermediate $link2da 10.1.2.3
tb-set-ip-link $moongenA $link2da 10.1.2.4

set link2db [$ns duplex-link $intermediate $moongenB 1.0Gb 0.0ms DropTail]
tb-set-ip-link $intermediate $link2db 10.1.3.3
tb-set-ip-link $moongenB $link2db 10.1.3.4

# client to network
set link3da [$ns duplex-link $moongenA $client 1.0Gb 0.0ms DropTail]
tb-set-ip-link $moongenA $link3da 10.1.4.4
tb-set-ip-link $client $link3da 10.1.4.5

set link3db [$ns duplex-link $moongenB $client 1.0Gb 0.0ms DropTail]
tb-set-ip-link $moongenB $link3db 10.1.5.4
tb-set-ip-link $client $link3db 10.1.5.5

# automatic routing
$ns rtproto Static

# go!
$ns run

