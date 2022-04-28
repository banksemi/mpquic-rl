Building
========

This suite consists of several interlocked packages.

MPQUIC-FEC
----------

Folder: `src/`

This project was forked from https://bitbucket.org/michelfra/quic-fec/src/networking_2019/
which in turn is a fork of https://github.com/lucas-clemente/quic-go

Most likely you'll just want to include this as a module in your projects. You
may look into `src/experiments/delay-server` or `src/experiments/delay-client`
for an example of this.

You can build both of those applications. Navigate into their respective
directories and run `go build`.

To include the locally modified version of the module as
`github.com/lucas-clemente/quic-go` in other projects, add the following to
their `go.mod` file:

* An entry in the `require` block: `github.com/lucas-clemente/quic-go v0.0.0`
* A line at the end: `replace github.com/lucas-clemente/quic-go => ../src`
    * `../src` in this case is the relative path of MPQUIC-FEC from your
      project
    * You can modify this as necessary

Caddy Web Server
----------------

Folder: `caddy/`

This was forked from https://github.com/caddyserver/caddy version `v1.0.5`.
Newer version of Caddy performed major refactoring, so compatibility will be
very limited.

Building will have to be performed from the `caddy/` subdirectory (thus
`caddy/caddy/`). Navigate into it and run `go build`.


Python Proxy Shared Library
---------------------------

Folder: `proxy_module/`

This implements a C compatible shared library in Go. It builds a bridge between
MPQUIC-FEC and Python. Building this requires a slightly more involved `build`
command:

```sh
go build -o proxy_module.so -buildmode=c-shared proxy_module.go
```

This produces a file called `proxy_module.so`. Copy it into the `astream/`
directory.

**Attention:** In contrast to other Go binaries, this can never be statically
linked (CGO will have to be used to create a shared library). Therefore the
output is not as portable as other Go binaries. If necessary you'll have to build
this one directly on your experimentation machine. This can be, for example,
necessary if your local and experiment machine use a different libc version.
