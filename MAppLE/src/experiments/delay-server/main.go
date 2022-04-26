package main

import (
	"flag"
	"fmt"
	"math/rand"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/h2quic"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
)

var NUMBER_OF_SOURCE_SYMBOLS uint = 20
var NUMBER_OF_REPAIR_SYMBOLS uint = 10
var NUMBER_OF_INTERLEAVED_BLOCKS uint = 1

var RAND_BYTES []byte

type binds []string

func (b binds) String() string {
	return strings.Join(b, ",")
}

func (b *binds) Set(v string) error {
	*b = strings.Split(v, ",")
	return nil
}

func initRandomData(size uint) []byte {
	// size bytes of random data
	// encoded using base64, so we send 4*ceil(size/3) bytes per frame
	randBytes := make([]byte, size)
	rand.Read(randBytes)
	return randBytes
}

func init() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		timestampStart := time.Now().UnixNano()

		mw := multipart.NewWriter(w)
		mw.SetBoundary("foo")

		fw, err := mw.CreateFormField("timestampStart")
		if err != nil {
			panic("failed creating form field")
		}
		fmt.Fprintf(fw, "%d", timestampStart)

		fw, err = mw.CreateFormField("value")
		if err != nil {
			panic("failed creating form field")
		}

		for i := 0; i < 1; i++ {
			if size, err := fw.Write(RAND_BYTES); size < len(RAND_BYTES) || err != nil {
				panic("failed writing data")
			}
		}
	})
}

func main() {
	bs := binds{}

	verbose := flag.Bool("v", false, "verbose")
	flag.Var(&bs, "bind", "bind to")
	certPath := flag.String("certpath", "/etc/ssl/certs/", "certificate directory")
	schedSchemeFlag := flag.String("sched", "rr", "rr, lr, ll, hr or s-edpf")
	nomultipath := flag.Bool("no-multipath", false, "")
	fecScheme := flag.String("fecScheme", "xor4", "fec scheme to use")
	fecEnable := flag.Bool("fec", false, "enable fec")
	fecOnIdle := flag.Bool("fecForceOnIdle", false, "force sending FEC data on idle paths")
	packetSize := flag.Uint("ps", 1, "number of bytes to append to responses")
	flag.Parse()

	RAND_BYTES = initRandomData(*packetSize)
	fmt.Println("Response Size:", len(RAND_BYTES), "bytes")

	fs, fc := quic.FECConfigFromString(*fecScheme)

	if *verbose {
		utils.SetLogLevel(utils.LogLevelDebug)
	} else {
		utils.SetLogLevel(utils.LogLevelInfo)
	}

	maxPathID := uint8(8)
	if !*nomultipath {
		fmt.Println("using multipath")
		maxPathID = 2
	}

	schedScheme := protocol.ParseSchedulingScheme(*schedSchemeFlag)

	certFile := *certPath + "/fullchain.pem"
	keyFile := *certPath + "/privkey.pem"

	var wg sync.WaitGroup
	wg.Add(len(bs))
	for _, b := range bs {
		bCap := b
		go func() {
			var err error
			config := quic.Config{
				Versions:                    []quic.VersionNumber{protocol.VersionMP},
				MaxPathID:                   maxPathID,
				FECScheme:                   fs,
				RedundancyController:        fc,
				SchedulingScheme:            schedScheme,
				ProtectReliableStreamFrames: *fecEnable,
				DisableFECRecoveredFrames:   false,
				ForceSendFECOnIdlePath:      *fecOnIdle,
			}
			err = h2quic.ListenAndServeQUICWIthConfig(bCap, certFile, keyFile, nil, &config)
			if err != nil {
				panic(err)
			}
			wg.Done()
		}()
	}
	wg.Wait()
}
