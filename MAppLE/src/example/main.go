package main

import (
	"crypto/md5"
	"errors"
	"flag"
	"fmt"
	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"path"
	"runtime"
	"strings"
	"sync"

	_ "net/http/pprof"

	"github.com/lucas-clemente/quic-go/h2quic"
	"github.com/lucas-clemente/quic-go/internal/utils"
)

var NUMBER_OF_SOURCE_SYMBOLS uint = 20
var NUMBER_OF_REPAIR_SYMBOLS uint = 10
var NUMBER_OF_INTERLEAVED_BLOCKS uint = 1

type binds []string

func (b binds) String() string {
	return strings.Join(b, ",")
}

func (b *binds) Set(v string) error {
	*b = strings.Split(v, ",")
	return nil
}

// Size is needed by the /demo/upload handler to determine the size of the uploaded file
type Size interface {
	Size() int64
}

func init() {
	http.HandleFunc("/demo/tile", func(w http.ResponseWriter, r *http.Request) {
		// Small 40x40 png
		w.Write([]byte{
			0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
			0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x28,
			0x01, 0x03, 0x00, 0x00, 0x00, 0xb6, 0x30, 0x2a, 0x2e, 0x00, 0x00, 0x00,
			0x03, 0x50, 0x4c, 0x54, 0x45, 0x5a, 0xc3, 0x5a, 0xad, 0x38, 0xaa, 0xdb,
			0x00, 0x00, 0x00, 0x0b, 0x49, 0x44, 0x41, 0x54, 0x78, 0x01, 0x63, 0x18,
			0x61, 0x00, 0x00, 0x00, 0xf0, 0x00, 0x01, 0xe2, 0xb8, 0x75, 0x22, 0x00,
			0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
		})
	})

	http.HandleFunc("/demo/tiles", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "<html><head><style>img{width:40px;height:40px;}</style></head><body>")
		for i := 0; i < 200; i++ {
			fmt.Fprintf(w, `<img src="/demo/tile?cachebust=%d">`, i)
		}
		io.WriteString(w, "</body></html>")
	})

	http.HandleFunc("/demo/echo", func(w http.ResponseWriter, r *http.Request) {
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			fmt.Printf("error reading body while handling /echo: %s\n", err.Error())
		}
		w.Write(body)
	})

	// accept file uploads and return the MD5 of the uploaded file
	// maximum accepted file size is 1 GB
	http.HandleFunc("/demo/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			err := r.ParseMultipartForm(1 << 30) // 1 GB
			if err == nil {
				var file multipart.File
				file, _, err = r.FormFile("uploadfile")
				if err == nil {
					var size int64
					if sizeInterface, ok := file.(Size); ok {
						size = sizeInterface.Size()
						b := make([]byte, size)
						file.Read(b)
						md5 := md5.Sum(b)
						fmt.Fprintf(w, "%x", md5)
						return
					}
					err = errors.New("couldn't get uploaded file size")
				}
			}
			if err != nil {
				utils.Infof("Error receiving upload: %#v", err)
			}
		}
		io.WriteString(w, `<html><body><form action="/demo/upload" method="post" enctype="multipart/form-data">
				<input type="file" name="uploadfile"><br>
				<input type="submit">
			</form></body></html>`)
	})

	http.HandleFunc("/demo/download", func(w http.ResponseWriter, r *http.Request) {
		data := make([]byte, 1024)
		mw := multipart.NewWriter(w)
		w.Header().Set("Content-Type", mw.FormDataContentType())
		for i := 0; i < 12800; i++ {
			fw, err := mw.CreateFormField("value")

			if err != nil {
				panic("failed creating form field")
				return
			}

			if size, err := fw.Write(data); size < 1024 || err != nil {
				panic("failed writing data")
				return
			}
		}
	})
}

func getBuildDir() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("Failed to get current frame")
	}

	return path.Dir(filename)
}

func main() {
	// defer profile.Start().Stop()
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()
	// runtime.SetBlockProfileRate(1)

	verbose := flag.Bool("v", false, "verbose")
	bs := binds{}
	flag.Var(&bs, "bind", "bind to")
	certPath := flag.String("certpath", getBuildDir(), "certificate directory")
	www := flag.String("www", "/var/www", "www data")
	tcp := flag.Bool("tcp", false, "also listen on TCP")
	fecSchemeFlag := flag.String("fecScheme", "rs", "rs, rlc or rlc")
	multipath := flag.Bool("multipath", false, "uses multiple paths")
	flag.Parse()

	if *verbose {
		utils.SetLogLevel(utils.LogLevelDebug)
	} else {
		utils.SetLogLevel(utils.LogLevelInfo)
	}
	utils.SetLogTimeFormat("")

	var maxPathID uint8

	var fecSchemeArg string

	fecSchemeArg = *fecSchemeFlag
	maxPathID = 0
	if *multipath {
		maxPathID = 2
	}

	var fs quic.FECSchemeID
	if fecSchemeArg == "rs" {
		fs = quic.ReedSolomonFECScheme
	} else if fecSchemeArg == "xor" {
		fs = quic.XORFECScheme
		NUMBER_OF_INTERLEAVED_BLOCKS = NUMBER_OF_REPAIR_SYMBOLS
		NUMBER_OF_SOURCE_SYMBOLS /= NUMBER_OF_REPAIR_SYMBOLS
		NUMBER_OF_REPAIR_SYMBOLS = 1
	} else {
		fs = quic.RLCFECScheme
	}

	certFile := *certPath + "/fullchain.pem"
	keyFile := *certPath + "/privkey.pem"

	http.Handle("/", http.FileServer(http.Dir(*www)))

	if len(bs) == 0 {
		bs = binds{"localhost:6121"}
	}

	var wg sync.WaitGroup
	wg.Add(len(bs))
	for _, b := range bs {
		bCap := b
		go func() {
			var err error
			if *tcp {
				err = h2quic.ListenAndServe(bCap, certFile, keyFile, nil)
			} else {
				//err = h2quic.ListenAndServeQUIC(bCap, certFile, keyFile, nil)
				var config = quic.Config{
					Versions:         []quic.VersionNumber{protocol.VersionMP},
					MaxPathID:        maxPathID,
					FECScheme:        fs,
					SchedulingScheme: protocol.SchedRR,
				}
				err = h2quic.ListenAndServeQUICWIthConfig(bCap, certFile, keyFile, nil, &config)
			}
			if err != nil {
				fmt.Println(err)
			}
			wg.Done()
		}()
	}
	wg.Wait()
}
