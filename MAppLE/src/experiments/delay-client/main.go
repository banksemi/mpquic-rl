package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"time"

	quic "github.com/lucas-clemente/quic-go"

	"github.com/lucas-clemente/quic-go/h2quic"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/sedpf"
)

func sum(elems ...int64) int64 {
	var a int64 = 0
	for i := 0; i < len(elems); i++ {
		a += elems[i]
	}
	return a
}

func main() {
	verbose := flag.Bool("v", false, "verbose")
	nomultipath := flag.Bool("no-multipath", false, "")
	schedSchemeFlag := flag.String("sched", "rr", "rr, lr, ll, hr or s-edpf")
	skipVerify := flag.Bool("s", false, "skip TLS verification")
	host := flag.String("h", "https://localhost:4430", "remote host")
	iter := flag.Int("i", 1024, "iterations")
	fecScheme := flag.String("fecScheme", "xor4", "fec scheme to use")
	fecEnable := flag.Bool("fec", false, "enable fec")
	fecOnIdle := flag.Bool("fecForceOnIdle", false, "force sending FEC data on idle paths")
	logFilename := flag.String("log", "log.csv", "filename to store log (csv) in")
	flag.Parse()

	fs, fc := quic.FECConfigFromString(*fecScheme)

	if *verbose {
		utils.SetLogLevel(utils.LogLevelDebug)
	} else {
		utils.SetLogLevel(utils.LogLevelInfo)
	}
	utils.SetLogTimeFormat("")

	var maxPathID uint8
	if !*nomultipath {
		fmt.Println("using multipath")
		// Two path topology
		maxPathID = 2
	}

	quicConfig := quic.Config{
		Versions:                    []quic.VersionNumber{protocol.VersionMP},
		MaxPathID:                   maxPathID,
		SchedulingScheme:            protocol.ParseSchedulingScheme(*schedSchemeFlag),
		FECScheme:                   fs,
		RedundancyController:        fc,
		ProtectReliableStreamFrames: *fecEnable,
		DisableFECRecoveredFrames:   false,
		ForceSendFECOnIdlePath:      *fecOnIdle,
	}

	log, err := os.OpenFile(*logFilename, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		panic(err)
	}

	logWriter := bufio.NewWriter(log)

	fmt.Fprintln(logWriter, "i,REQ,SND,RCV,SIZE")

	var transferTime float64
	var transferData uint

	rtts := make([]float64, *iter)
	owds := make([]float64, *iter)

	transport := h2quic.RoundTripper{
		QuicConfig:      &quicConfig,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: *skipVerify},
	}

	hclient := &http.Client{
		Transport: &transport,
	}

	var failedRequests uint

	for i := 0; i < *iter; i += 1 {
		utils.Infof("GET %s", *host)
		reqTimestamp := time.Now().UnixNano()
		rsp, err := hclient.Get(*host)
		if err != nil {
			fmt.Println("frame loss occured")
			failedRequests++
			continue
		}
		fmt.Println("no frame loss occured")

		mr := multipart.NewReader(rsp.Body, "foo")

		p, err := mr.NextPart()
		if err == io.EOF {
			return
		}
		if err != nil {
			panic(err)
		}
		slurp, err := ioutil.ReadAll(p)
		if err != nil {
			panic(err)
		}

		str := string(slurp)

		sendTimestamp, err := strconv.ParseInt(str, 10, 64)

		if err != nil {
			panic(err)
		}

		var nBytes uint
		// read all parts
		for part, err := mr.NextPart(); err == nil; part, err = mr.NextPart() {
			data, _ := ioutil.ReadAll(part)
			nBytes += uint(len(data))
		}
		transferData += nBytes

		recvTimestamp := time.Now().UnixNano()

		owd := recvTimestamp - sendTimestamp
		rtt := recvTimestamp - reqTimestamp

		fmt.Fprintf(
			logWriter,
			"%d,%d,%d,%d,%d\n",
			i,
			reqTimestamp,
			sendTimestamp,
			recvTimestamp,
			nBytes,
		)

		rtts[i] = float64(rtt) / 1000000000. // want values in seconds
		owds[i] = float64(owd) / 1000000000.
		transferTime += float64(owd) / 1000000000.
	}

	transport.Close()

	rttDist := sedpf.NewGaussianFromSeries(rtts)
	owdDist := sedpf.NewGaussianFromSeries(owds)

	fmt.Println("RTT\tmean:", rttDist.Mean, "stddev:", rttDist.StdDev())
	fmt.Println("OWD\tmean:", owdDist.Mean, "stddev:", owdDist.StdDev())
	fmt.Println("Goodput\n", float64(transferData)/transferTime, "B/s")

	fmt.Println("failed HTTP requests", failedRequests)

	logWriter.Flush()
	log.Close()
}
