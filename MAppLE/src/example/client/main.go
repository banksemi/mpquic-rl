package main

import (
	"bytes"
	"crypto/tls"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"sync"

	quic "github.com/lucas-clemente/quic-go"

	"github.com/lucas-clemente/quic-go/h2quic"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
)

func main() {
	verbose := flag.Bool("v", false, "verbose")
	multipath := flag.Bool("m", false, "multipath")
	output := flag.String("o", "", "logging output")
	skipVerify := flag.Bool("s", false, "skip TLS verification")
	flag.Parse()
	urls := flag.Args()

	if *verbose {
		utils.SetLogLevel(utils.LogLevelDebug)
	} else {
		utils.SetLogLevel(utils.LogLevelInfo)
	}
	utils.SetLogTimeFormat("")

	if *output != "" {
		logfile, err := os.Create(*output)
		if err != nil {
			panic(err)
		}
		defer logfile.Close()
		log.SetOutput(logfile)
	}

	var maxPathID uint8
	if *multipath {
		// Two path topology
		maxPathID = 2
	}

	quicConfig := quic.Config{
		MaxPathID:        maxPathID,
		SchedulingScheme: protocol.SchedRR,
	}

	hclient := &http.Client{
		Transport: &h2quic.RoundTripper{
			QuicConfig:      &quicConfig,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: *skipVerify},
		},
	}

	var wg sync.WaitGroup
	wg.Add(len(urls))
	for _, addr := range urls {
		utils.Infof("GET %s", addr)
		go func(addr string) {
			rsp, err := hclient.Get(addr)
			if err != nil {
				panic(err)
			}
			utils.Infof("Got response for %s: %#v", addr, rsp)

			body := &bytes.Buffer{}
			_, err = io.Copy(body, rsp.Body)
			if err != nil {
				panic(err)
			}
			utils.Infof("Request Body:")
			utils.Infof("%s", body.Bytes())
			wg.Done()
		}(addr)
	}
	wg.Wait()
}
