MAppLE (MPQUIC Application Latency Evaluation platform)
==========
A unified MPQUIC Application Latency Evaluation platform (MAppLE) to evaluate and develop MPQUIC with modular multiplexers, stream schedulers, and packet schedulers.

# Traffic Generator

This is an experimental network traffic generator for MPTCP and MP-QUIC

Usage:

cd ~/mpquic-latency/mpquic-go/src/github/lucas-clemente/quic-go/traffic-gen

go build

go run traffic-gen.go

  -a string
  
        Destination address (default "localhost")
        
  -arrdist string
  
        arrival distribution (default "c")
        
  -arrval float
  
        arrival value (default 1000)
        
  -cc string
  
        Congestion control (default "cubic")
        
  -csizedist string
  
        data chunk size distribution (default "c")
        
  -csizeval float
  
        data chunk size value (default 1000)
        
  -log string
  
        Log folder
        
  -m    Enable multipath (default true)
  
  -mode string
  
        start in client or server mode (default "server")
        
  -p string
  
        TCP or QUIC (default "tcp")
        
  -sched string
  
        Scheduler
        
  -t uint
  
        time to run (ms) (default 10000)
        
  -v    Debug mode



MPQUIC-FEC
==========

This suite implements a testbed for multi-path QUIC with Forward Erasure
Correction. The main application is a DASH video stream on HTTP2 over QUIC.

Go version 1.14 was used during development. Everything was compiled and run
under Linux on x86_64.

MPQUIC-FEC
----------

The MPQUIC-FEC implementation itself can be found in the `src` directory. Go
modules are used to handle external dependencies.

DASH Server
-----------

_Caddy_ is used as the HTTP2 server and can be found in the `caddy` directory.
Its `go.mod` file is setup in such a way that the local MPQUIC-FEC
implementation is used.

DASH Stream
-----------

Files relevant to the DASH stream can be found in the `dash` sub directory. To
host, run _Caddy_ from here, as it searches for the `Caddyfile` in the current
working directory.

Because you may want to inspect packets with Wireshark, a specific certificate
should be used by Caddy. Generate a self-signed keypair using OpenSSL. This can
be automatically done with the `makecert.sh` script. Use the default for each
option except `CN`. Set the common name of the certificate to the initial IP
you want to open the multipath connection on. E.g. if the connection is opened
on `https://10.1.1.1:4242`, set the `CN` to `10.1.1.1`. Don't use hostnames,
they cause issues with the testbed.

A video is not provided, but can be encoded and prepared for DASH using
`ffmpeg`. Run the `prepare_video.sh` shell script. On the first run it will
download the source video from `http://www.jell.yfish.us`. ffmpeg version
n4.2.3 was used during development.

It outputs the encoded video segments and the DASH manifest into the `video`
sub directory, which can then be hosted by _Caddy_.

To use all features in caddy (QUIC, multipath, FEC, etc.) use the following
set of parameters:

```sh
./caddy -quic -mp -scheduler s-edpf -fec --fecConfig win-xor
```

DASH Client
-----------

_AStream_'s client portion is used to access the DASH stream. Here, it is
ported to Python3. Python 3.6 is the lowest version the software was tested
with.

An additional proxy module is required to allow using MPQUIC-FEC in _AStream_.
The module can be found in the `proxy_module` sub directory. To build, navigate
into this sub directory and execute:

```sh
go build -o proxy_module.so -buildmode=c-shared proxy_module.go
```

Copy then the binary `proxy_module.so` into `astream/`

`dash_client.py` is the entry point to _AStream_.

```sh
python3 astream/dash_client.py -q -mp -m https://10.1.1.1/manifest.mpd -s s-edpf --fec --fecConfig win-xor
```

Testbed
-------

Evaluations were performed on Emulab. The utilized testbed descriptions are
found in the `testbed` sub directory.

The configuration used in the experiments is `experiment-moongen.tcl`. It is
made to use two moongen based delay nodes. Those form two connections from the
client to the intermediate router and simulate different link properties.

S-EDPF
------

The implementation of S-EDPF is found in the `src` directory. It is split into
two Go packages due to the testbed's code structure.

The employed statistical methods are found in `src/sedpf/`. In contrast, the
multipath scheduler itself is found in the file `src/scheduler_sedpf.go`

S-IOD
------

Currently the implementation of S-IOD lives in `src/scheduler_iod.go`. The main
function is `SelectPathsAndOrder`. In contrast to other scheduling functions
this also prepares the actual packets in order to interweave with the FEC
framework. The return value is a list of the packets to be sent and their
respective paths.  They are already ordered per-path and are actually sent in
parallel via goroutines. One such routine is spawned per path, allowing packets
to be sent out of order via multiple paths simultaneously.
