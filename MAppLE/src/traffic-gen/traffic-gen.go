package main

import (
	"bytes"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"math"

	"sync"

	// "errors"
	"flag"
	"fmt"
	"io"
	"log"

	// "math"
	"math/big"
	"math/rand"
	"net"

	// "net/http"
	// "encoding/base64"
	"os"
	"strconv"
	"strings"
	"time"

	prob "github.com/atgjack/prob"
	quic "github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/fec"
	"github.com/lucas-clemente/quic-go/logger"

	// "github.com/lucas-clemente/quic-go/h2quic"
	// "github.com/lucas-clemente/quic-go/internal/testdata"
	// "github.com/lucas-clemente/quic-go/internal/protocol"
	quic_protocol "github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"

	// "quic-go"
	//	"io/ioutil"
	"container/list"
)

type binds []string

type ServerLogRecord struct {
	generated_time uint
	sent_time      uint
	messageSize    quic_protocol.ByteCount
}

type ServerLog struct {
	data map[uint]ServerLogRecord
	lock sync.RWMutex
}

type TrafficGenConfig struct {
	Mode              string
	RunTime           uint
	StartDelay        uint
	CsizeDistro       string
	CsizeValue        float64
	ArrDistro         string
	ArrValue          float64
	Unlimited         bool
	Address           string
	Protocol          string
	LogFolder         string
	IsMultipath       bool
	IsMultiStream     bool
	Sched             string
	Debug             bool
	CongestionControl string
	IsBlockCall       bool
	IsReverse         bool
	FECScheme         string
	multiplexer       string
}

func (b binds) String() string {
	return strings.Join(b, ",")
}

func (b *binds) Set(v string) error {
	*b = strings.Split(v, ",")
	return nil
}

type MessageList struct {
	mess_list *list.List
	// isSending bool
	mutex sync.RWMutex
}

const BASE_SEQ_NO uint = 2147483648 // 0x80000000
var LOG_PREFIX string = ""

const SERVER_ADDRESS string = "10.1.1.2"
const SERVER_TCP_PORT int = 2121
const SERVER_QUIC_PORT int = 4343
const STREAM_TIMEOUT int = 5
const MB = 1 << 20
const MAX_SEND_BUFFER_SIZE = 4194304

type ClientManager struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
}

type Client struct {
	socket net.Conn
	data   chan []byte
}

func (manager *ClientManager) start() {
	for {
		select {
		case connection := <-manager.register:
			manager.clients[connection] = true
			fmt.Println("Added new connection!")
		case connection := <-manager.unregister:
			if _, ok := manager.clients[connection]; ok {
				close(connection.data)
				delete(manager.clients, connection)
				fmt.Println("A connection has terminated!")
			}
		case message := <-manager.broadcast:
			for connection := range manager.clients {
				select {
				case connection.data <- message:
				default:
					close(connection.data)
					delete(manager.clients, connection)
				}
			}
		}
	}
}

func (manager *ClientManager) communicate(client *Client, config *TrafficGenConfig) {
	if !config.IsReverse {
		receiveTCP(client.socket)
	} else {
		if config.Protocol == "tcp" {
			tcp_connection := client.socket.(*net.TCPConn)
			send(nil, tcp_connection, config)
		} else {

		}
	}
	manager.unregister <- client
}

func receiveTCP(socket net.Conn) {
	timeStamps := make(map[uint]ServerLogRecord)
	buffer := make([]byte, 0)
	for {
		message := make([]byte, 65536)
		length, err := socket.Read(message)
		if err != nil {
			log.Println(err)

			socket.Close()
			break
		}
		if length > 0 {
			message = message[0:length]
			// utils.Debugf("\n RECEIVED: %x \n", message)
			// manager.broadcast <- message
			eoc_byte_index := bytes.Index(message, intToBytes(uint(BASE_SEQ_NO-1), 4))
			// log.Println(eoc_byte_index)

			for eoc_byte_index != -1 {
				data_chunk := append(buffer, message[0:eoc_byte_index+4]...)
				//				seq_no := message[eoc_byte_index-4:eoc_byte_index]
				// utils.Debugf("\n CHUNK: %x \n  length %d \n", data_chunk, len(data_chunk))
				// Get data chunk ID and record receive timestampt
				seq_no := data_chunk[0:4]
				seq_no_int := bytesToInt(seq_no)
				timeStamps[seq_no_int] = ServerLogRecord{
					generated_time: uint(time.Now().UnixNano()),
					messageSize:    quic_protocol.ByteCount(eoc_byte_index + 4),
				}
				//				buffer.Write(message[eoc_byte_index:length])

				// Cut out recorded chunk
				message = message[eoc_byte_index+4:]
				buffer = make([]byte, 0)
				eoc_byte_index = bytes.Index(message, intToBytes(uint(BASE_SEQ_NO-1), 4))
			}
			buffer = append(buffer, message...)
		}
	}

	writeToFile(LOG_PREFIX+"receiver-timestamp.log", timeStamps)
}

func (manager *ClientManager) send(client *Client) {
	defer client.socket.Close()
	for {
		select {
		case message, ok := <-client.data:
			if !ok {
				return
			}
			client.socket.Write(message)
		}
	}
}

func startServerMode(config *TrafficGenConfig) {
	fmt.Println("Starting server...")
	var listener net.Listener
	var err error
	manager := ClientManager{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
	go manager.start()

	switch config.Protocol {
	case "tcp":

		listener, err = net.Listen("tcp", config.Address)
		if err != nil {
			log.Println(err)
		}
		log.Println("TCP Listen ...")
		for {
			connection, _ := listener.Accept()
			tcp_connection := connection.(*net.TCPConn)
			tcp_connection.SetNoDelay(true)
			if err != nil {
				log.Println(err)
			}
			client := &Client{socket: tcp_connection, data: make(chan []byte)}
			manager.register <- client
			go manager.communicate(client, config)
			//		go manager.send(client)
		}
	case "quic":
		startQUICServer(config.Address, config.IsMultipath, config.IsMultiStream, config.FECScheme, config.Sched, config.CongestionControl)

	}

}

func startClientMode(config *TrafficGenConfig) {
	fmt.Println("Starting client...")

	// var stream quic.Stream
	var quic_session quic.Session
	var connection *net.TCPConn
	var err error

	if config.Protocol == "quic" {
		addresses := []string{config.Address}
		quic_session, err = startQUICSession(addresses, config.Sched, config.IsMultipath, config.FECScheme, config.CongestionControl)
		if err != nil {
			panic(err)
		}
		// defer stream.Close()
		defer quic_session.Close(nil)

	} else if config.Protocol == "tcp" {
		tcp_address := strings.Split(config.Address, ":")
		ip_add := net.ParseIP(tcp_address[0]).To4()
		port, _ := strconv.Atoi(tcp_address[1])
		connection, err = net.DialTCP("tcp", nil, &net.TCPAddr{IP: ip_add, Port: port})
		connection.SetNoDelay(true)
		defer connection.Close()

	}
	//	addr,_:=net.ResolveTCPAddr("tcp", address+":443")
	//	connection, error := net.DialTCP("tcp", nil, addr)

	if err != nil {
		log.Println(err)
	}
	if !config.IsReverse {
		send(quic_session, connection, config)
	} else {
		if config.Protocol == "tcp" {
			receiveTCP(connection)
		} else {

		}
	}

}

func send(quic_session quic.Session, connection *net.TCPConn, config *TrafficGenConfig) {
	sendingDone := make(chan bool)
	generatingDone := make(chan bool)
	var err error
	var run_time_duration time.Duration
	run_time_duration, err = time.ParseDuration(strconv.Itoa(int(config.RunTime)) + "ms")
	startDelay, err := time.ParseDuration(strconv.Itoa(int(config.StartDelay)) + "ms")
	if err != nil {
		log.Println(err)
	}

	startTime := time.Now().Add(startDelay)
	endTime := startTime.Add(run_time_duration)
	timeStamps := make(map[uint]ServerLogRecord)

	// writeTime := make(map[uint]uint)
	send_queue := MessageList{mess_list: list.New()}
	gen_finished := false

	// This Go routine generate messages
	go func() {
		gen_counter := 0
		gen_bytes := 0

		for i := 1; time.Now().Before(endTime); i++ {
			send_queue_size := 0
			for e := send_queue.mess_list.Front(); e != nil && e.Value != nil; e = e.Next() {
				send_queue_size += len(e.Value.([]byte))
			}

			if ((config.Protocol == "tcp" || config.IsBlockCall) && (send_queue.mess_list.Len() > 0 || !quic.AllStreamsAreEmpty(quic_session))) || send_queue_size > MAX_SEND_BUFFER_SIZE {
				time.Sleep(time.Millisecond)
				continue
			}
			message, seq := generateMessage(uint(gen_counter), config.CsizeDistro, config.CsizeValue)
			gen_counter++
			gen_bytes += len(message)

			// if !isBlockingCall {
			var wait_time uint
			if time.Now().Before(startTime) {
				wait_time = uint(math.Pow10(9)) // if startDelay, wait 1s  until then
			} else {
				wait_time = uint(math.Pow10(9)/getRandom(config.ArrDistro, config.ArrValue)) - (uint(time.Now().UnixNano()) - timeStamps[seq-1].generated_time)
			}

			if !config.Unlimited && wait_time > 0 {
				wait(wait_time)
			}
			// }

			// if !isBlockingCall {
			// Get time at the moment message generated
			timeStamps[seq] = ServerLogRecord{
				generated_time: uint(time.Now().UnixNano()),
				messageSize:    quic_protocol.ByteCount(len(message)),
			}

			// }
			send_queue.mutex.Lock()
			send_queue.mess_list.PushBack(message)
			send_queue.mutex.Unlock()

		}
		utils.Infof("Generation done after %d ms", run_time_duration.Nanoseconds()/int64(math.Pow10(6)))
		utils.Infof("Generate total: %d messages, %d bytes", gen_counter, gen_bytes)
		gen_finished = true
		generatingDone <- true
	}()

	// This Go routine send message
	go func() {
		sent_counter := 0
		//var current_stream quic.Stream
		var wg sync.WaitGroup

		go quic.StartMultiplexer(quic_session)
		for !(gen_finished && send_queue.mess_list.Len() == 0) {
			wait(1000) // Wait for 1 microsecond
			if send_queue.mess_list.Len() == 0 {
				continue
			}
			queue_font := send_queue.mess_list.Front()
			message, _ := queue_font.Value.([]byte)
			// beforesent := time.Now()
			// if isBlockingCall {
			// 	// Get time at the moment message put in stream
			// 	timeStamps[bytesToInt(message[0:4])] = uint(time.Now().UnixNano())
			// }

			// send_queue.mutex.Lock()
			// send_queue.isSending = true
			// send_queue.mutex.Unlock()

			utils.Debugf("Message in queue: %d at %d \n", send_queue.mess_list.Len(), uint(time.Now().UnixNano()))
			if config.Protocol == "quic" {
				// Use  Multiplexter
				if config.IsBlockCall {

					if quic.MultiplexData(quic_session, config.multiplexer, message) {
						sent_counter++
						utils.Debugf("\n Send %d message successfully: %d", sent_counter, message[0:4])
					} else {
						utils.Debugf("\n Failed to send message: %d", message[0:4])
					}
				} else {

					go func() {
						wg.Add(1)
						defer wg.Done()
						if quic.MultiplexData(quic_session, config.multiplexer, message) {
							sent_counter++
							utils.Debugf("\n Send %d message successfully: %d", sent_counter, message[0:4])
						} else {
							utils.Debugf("\n Failed to send message: %d", message[0:4])
						}
					}()
				}
				// DEPRECATED: Not use multliplexer
				//go quic.TestMultiplexer(quic_session, message)
				/*
				 *                                if config.IsMultiStream {
				 *                                        go startQUICClientStream(quic_session, message)
				 *                                } else {
				 *                                        if current_stream == nil || current_stream.StreamID() == 3 || current_stream.StreamID() == 1 {
				 *                                                current_stream, err = quic_session.OpenStreamSync()
				 *                                                if err != nil {
				 *                                                        utils.Debugf("Error OpenStreamSync:", err)
				 *                                                        return
				 *                                                }
				 *
				 *                                                defer current_stream.Close()
				 *                                        }
				 *                                        utils.Debugf("OpenStream count: %d, message %d", quic_session.GetOpenStreamNo(), bytesToInt(message[0:4]))
				 *                                         beforeWrite := time.Now()
				 *                                        current_stream.Write(message)
				 *                                         utils.Debugf("StreamID: %d write %d", current_stream.StreamID(), time.Now().Sub(beforeWrite).Nanoseconds())
				 *
				 *                                }
				 */

			} else if config.Protocol == "tcp" {
				go connection.Write(message)
				sent_counter++

			}

			//sent_counter++
			send_queue.mutex.Lock()
			// send_queue.isSending = false
			send_queue.mess_list.Remove(queue_font)
			send_queue.mutex.Unlock()

			// if isBlockingCall {
			// 	// utils.Debugf("Sent time: %d ", time.Now().Sub(beforesent).Nanoseconds())
			// 	var wait_time uint
			// 	if time.Now().Before(startTime) {
			// 		wait_time = 1000000000
			// 	} else {
			// 		wait_time = uint(1000000000/getRandom(arrival_distro, arrival_value)) - (uint(time.Now().UnixNano()) - timeStamps[bytesToInt(message[0:4])])
			// 		// wait_time = uint(1000000000/getRandom(arrival_distro, arrival_value)) - (uint(time.Now().UnixNano()) - timeStamps[seq-1])

			// 	}
			// 	if wait_time > 0 {
			// 		wait(wait_time)
			// 	}
			// }

		}
		wg.Wait()
		utils.Infof("Sent total: %d messages", sent_counter)
		sendingDone <- true

	}()

	// Wait for finishing generating and sending
	<-generatingDone
	<-sendingDone

	// Wait until all data is sent
	for !quic.AllStreamsAreEmpty(quic_session) {
		wait(uint(math.Pow10(9))) // wait 1 second
	}

	wait(3 * uint(math.Pow10(9))) // wait for abit longer. TODO: find out when the transmission is actually finished
	quic.CloseAllOutgoingStream(quic_session)

	// Write timestamp to log
	writeToFile(LOG_PREFIX+"sender-timestamp.log", timeStamps)
	// writeToFile(LOG_PREFIX+"write-timegap.log", writeTime)
	os.Rename("sender-frame.log", LOG_PREFIX+"sender-frame.log")
	os.Rename("receiver-frame.log", LOG_PREFIX+"receiver-frame.log")

	// }()
}

func startQUICClientStream(quic_session quic.Session, message []byte) {
	beforeOpen := time.Now()
	stream, err := quic_session.OpenStreamSync()
	if err != nil {
		utils.Debugf("Error OpenStreamSync:", err)
		return
	}
	defer stream.Close()
	utils.Debugf("OpenStream count: %d at %d", quic_session.GetOpenStreamNo(), time.Now().UnixNano())
	beforeWrite := time.Now()
	stream.Write(message)
	utils.Debugf("StreamID: %d open %d write %d", stream.StreamID(), beforeWrite.Sub(beforeOpen).Nanoseconds(), time.Now().Sub(beforeWrite).Nanoseconds())
	quic_session.RemoveStream(stream.StreamID())
}

func startQUICServer(addr string, isMultipath bool, isMultiStream bool, fecScheme string, scheduler string, congestionControl string) error {
	var maxPathID uint8
	if isMultipath {
		maxPathID = 2
	}

	var useFEC bool
	var fecID quic.FECSchemeID
	var fecReCrller fec.RedundancyController
	if fecScheme != "" && fecScheme != "nofec" {
		useFEC = true
		fecID, fecReCrller = quic.FECConfigFromString(fecScheme)
	} else {
		useFEC = false
	}
	listener, err := quic.ListenAddr(addr, generateTLSConfig(), &quic.Config{
		MaxPathID:                   maxPathID,
		SchedulingSchemeName:        scheduler,
		ProtectReliableStreamFrames: useFEC,
		FECScheme:                   fecID,
		RedundancyController:        fecReCrller,
		CongestionControl:           quic_protocol.ParseCongestionControl(congestionControl),
		CongestionControlName:       congestionControl,
		//SchedulingScheme:     schedNameConvert("quic", scheduler),
		// MaxReceiveStreamFlowControlWindow:     uint64(protocol.ByteCount(math.Floor(100 * MB))),
		// MaxReceiveConnectionFlowControlWindow: uint64(protocol.ByteCount(math.Floor(100 * MB))),
	})
	if err != nil {
		return err
	}
	sess, err := listener.Accept()
	if err != nil {
		return err
	}
	defer sess.Close(err)

	serverlog := ServerLog{
		data: make(map[uint]ServerLogRecord),
		lock: sync.RWMutex{},
	}

	// previous := BASE_SEQ_NO

	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			utils.Errorf("AcceptStream: ", err)
			break
		}
		// defer stream.Close()
		go receiveQUICStream(sess, stream, isMultiStream, &serverlog)

	}

	writeToFile(LOG_PREFIX+"server-timestamp.log", serverlog.data)
	utils.Infof("\nFinish receive: %d messages \n", len(serverlog.data))
	return err
}

func receiveQUICStream(sess quic.Session, stream quic.Stream, isMultistream bool, serverlog *ServerLog) {
	utils.Debugf("\n Get data from stream: %d \n at ", stream.StreamID(), time.Now().UnixNano())
	// beginstream := time.Now()
	message_buffer := make([]byte, 0)
	recorded_ts := time.Now().UnixNano()
	defer stream.Close()
	// prevTime := time.Now()
messageLoop:
	for {
		// readTime := time.Now()
		receive_buffer := make([]byte, 2<<20)
		length, err := stream.Read(receive_buffer)
		utils.Debugf("\t Read data from stream len  %d", length)

		if length > 0 {
			recorded_ts = time.Now().UnixNano()
			receive_buffer = receive_buffer[0:length]
			message_buffer = append(message_buffer, receive_buffer...)
			// utils.Debugf("\n after %d RECEIVED from stream %d mes_len %d buffer %d: %x...%x \n", time.Now().Sub(prevTime).Nanoseconds(), stream.StreamID(), length, len(buffer), message[0:4], message[length-4:length])
			// prevTime = time.Now()
			eoc_byte_index := bytes.Index(message_buffer, intToBytes(uint(BASE_SEQ_NO-1), 4))
			// log.Println(eoc_byte_index)

			for eoc_byte_index != -1 {
				data_chunk := message_buffer[0 : eoc_byte_index+4]
				//				seq_no := message[eoc_byte_index-4:eoc_byte_index]
				// utils.Debugf("\n CHUNK: %x...%x  \n  length %d \n", data_chunk[0:4], data_chunk[len(data_chunk)-4:len(data_chunk)], len(data_chunk))
				// Get data chunk ID and record receive timestampt
				seq_no := data_chunk[0:4]
				seq_no_int := bytesToInt(seq_no)

				// these lines to debug
				// if seq_no_int != previous+1 {
				// 	utils.Debugf("\n Unordered: %d \n", seq_no_int)
				// }
				// previous = seq_no_int
				//
				//				buffer.Write(message[eoc_byte_index:length])
				if seq_no_int >= BASE_SEQ_NO {
					utils.Debugf("\n Received message: %d on Stream %d at %s \n", seq_no, stream.StreamID(), recorded_ts)
					serverlog.lock.Lock()
					serverlog.data[seq_no_int] = ServerLogRecord{
						generated_time: uint(recorded_ts),
						//messageSize: quic_protocol.ByteCount(eoc_byte_index + 4),
						messageSize: quic_protocol.ByteCount(len(data_chunk)),
					}
					serverlog.lock.Unlock()
					/*
					 *                                        if isMultistream {
					 *                                                break messageLoop
					 *
					 *                                        }
					 */
				}

				// Cut out recorded chunk
				message_buffer = message_buffer[eoc_byte_index+4:]
				eoc_byte_index = bytes.Index(message_buffer, intToBytes(uint(BASE_SEQ_NO-1), 4))
			}
		}

		if err != nil {
			utils.Debugf("\n Error getting mess: ", err)
			//utils.Debugf("\n Data in buffer: %d \n Message: %d", receive_buffer, message_buffer)
			break messageLoop
		}
		time.Sleep(time.Microsecond)
	}
	//sess.RemoveStream(stream.StreamID())
	utils.Debugf("\n Finish receive on Stream: %d at %s \n", stream.StreamID(), time.Now())
}

func startQUICSession(urls []string, scheduler string, isMultipath bool, fecScheme string, congestionControl string) (sess quic.Session, err error) {
	var maxPathID uint8
	if isMultipath {
		maxPathID = 2
	}

	var useFEC bool
	var fecID quic.FECSchemeID
	var fecReCrller fec.RedundancyController
	if fecScheme != "" && fecScheme != "nofec" {
		useFEC = true
		fecID, fecReCrller = quic.FECConfigFromString(fecScheme)
	} else {
		useFEC = false
	}
	session, err := quic.DialAddr(urls[0], &tls.Config{InsecureSkipVerify: true}, &quic.Config{
		MaxPathID:                   maxPathID,
		SchedulingSchemeName:        scheduler,
		ProtectReliableStreamFrames: useFEC,
		FECScheme:                   fecID,
		RedundancyController:        fecReCrller,
		CongestionControl:           quic_protocol.ParseCongestionControl(congestionControl),
		CongestionControlName:       congestionControl,
		//SchedulingScheme:     schedNameConvert("quic", scheduler),
		// MaxReceiveStreamFlowControlWindow:     uint64(protocol.ByteCount(math.Floor(100 * MB))),
		// MaxReceiveConnectionFlowControlWindow: uint64(protocol.ByteCount(math.Floor(100 * MB))),
	})

	if err != nil {
		return nil, err
	}

	return session, nil
}

// wait for interarrival_time nanosecond
func wait(interarrival_time uint) {
	waiting_time := time.Duration(interarrival_time) * time.Nanosecond
	// utils.Debugf("wait for %d ns \n", waiting_time.Nanoseconds())
	time.Sleep(waiting_time)
}

func getRandom(distro string, value float64) float64 {
	var retVal float64
	switch distro {
	case "c":
		retVal = value
	case "e":
		retVal = rand.ExpFloat64() * value
	case "g":
		retVal = rand.NormFloat64()*value/3 + value
		if retVal > 2*value {
			retVal = 2 * value
		} else if retVal < 0 {
			retVal = 0
		}

	case "b":

	case "wei":

	case "tld":
		dist, _ := prob.NewLogistic(0.25, 0.5)
		retVal = value * dist.Random()

		if retVal > 2*value {
			retVal = 2 * value
		} else if retVal < value/4 {
			retVal = value / 4
		}

	default:
		retVal = 1.0
	}

	return retVal
}

func generateMessage(offset_seq uint, csize_distro string, csize_value float64) ([]byte, uint) {
	//	utils.Debugf("Gen mess: %d \n", time.Now().UnixNano())
	seq_no := BASE_SEQ_NO + offset_seq
	seq_header := intToBytes(uint(seq_no), 4)
	eoc_header := intToBytes(uint(BASE_SEQ_NO-1), 4)

	csize := uint64(getRandom(csize_distro, csize_value))
	//chunk size must be a factor of 4 to avoid EOL fragmenting
	// Temporary set to a factor of 4 to match QUIC MTU 1350byte
	csize = csize - csize%2
	if csize < 12 {
		csize = 12
	}
	// utils.Debugf("Message size %d \n ", csize)
	pseudo_payload := make([]byte, (csize - 8))
	for i := 0; i < len(pseudo_payload); i++ {
		pseudo_payload[i] = 0x01
	}

	message := append(seq_header, pseudo_payload...)
	//	message = append(message, seq_header...)
	message = append(message, eoc_header...)

	return message, seq_no
}

func intToBytes(num uint, size uint) []byte {
	bs := make([]byte, size)
	binary.BigEndian.PutUint32(bs, uint32(num))
	return bs
}

func bytesToInt(b []byte) uint {
	return uint(binary.BigEndian.Uint32(b))
}

func writeToFile(filename string, data map[uint]ServerLogRecord) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	for k, v := range data {
		line := fmt.Sprintf("%d %d %d\n", k, v.generated_time, v.messageSize)
		_, err = io.WriteString(file, line)
		if err != nil {
			return err
		}

	}

	return file.Sync()
}

type loggingWriter struct{ io.Writer }

func (w loggingWriter) Write(b []byte) (int, error) {
	fmt.Printf("Server: Got '%x'\n", b)
	return w.Writer.Write(b)
}

func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(crand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(crand.Reader, &template, &template, &key.PublicKey, key)

	if err != nil {
		panic(err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	// pkcs1 := x509.MarshalPKCS1PrivateKey(key)
	// priv := base64.StdEncoding.EncodeToString(pkcs1)
	// pub := base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PublicKey(&key.PublicKey))

	// utils.Debugf("pri key: %s", priv)
	// utils.Debugf("pub key: %s", pub)
	// key_file, _ := os.Create("id_rsa_trafficgen")

	// defer key_file.Close()
	// key_file.WriteString("-----BEGIN RSA PRIVATE KEY-----\n")
	// key_file.WriteString(priv)
	// key_file.WriteString("\n-----END RSA PRIVATE KEY-----")

	if err != nil {
		panic(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{tlsCert}}
}

/*
 *func schedNameConvert(protocol string, sched_name string) quic_protocol.SchedulingSchemeID {
 *        var scheduler_id quic_protocol.SchedulingSchemeID
 *
 *        if protocol == "quic" {
 *                switch sched_name {
 *                        case "lowrtt":
 *                                scheduler_id = quic_protocol.SchedLowLatency
 *                        case "rr":
 *                                scheduler_id = quic_protocol.SchedRR
 *                        case "opp":
 *                                scheduler_id = quic_protocol.SchedSingle
 *
 *                        case "nt":
 *                                scheduler_id = quic_protocol.SchedSingle
 *
 *                        case "re":
 *                                scheduler_id = quic_protocol.SchedSingle
 *
 *                        case "sp":
 *                                scheduler_id = quic_protocol.SchedSingle
 *                        default:
 *                                panic("no scheduler found")
 *                }
 *        }
 *
 *        return scheduler_id
 *}
 */

func main() {
	flagMode := flag.String("mode", "server", "Start in client or server mode")
	flagTime := flag.Uint("t", 10000, "Time to run (ms)")
	flagStartDelay := flag.Uint("d", 2000, "Start Delay (ms)")
	flagCsizeDistro := flag.String("csizedist", "c", "Data chunk size distribution")
	flagCsizeValue := flag.Float64("csizeval", 1000, "data chunk size value")
	flagArrDistro := flag.String("arrdist", "c", "arrival distribution")
	flagArrValue := flag.Float64("arrval", 1000, "arrival value")
	flagUnlimited := flag.Bool("unlimited", false, "don't limit sending rate")
	flagAddress := flag.String("a", "localhost:2121", "Destination address")
	flagProtocol := flag.String("p", "quic", "TCP or QUIC")
	flagLog := flag.String("log", "", "Log folder")
	flagMultipath := flag.Bool("m", false, "Enable multipath")
	flagMultiStream := flag.Bool("mstr", false, "Enable multistream")
	flagSched := flag.String("sched", "ll", "Scheduler")
	flagDebug := flag.Bool("v", false, "Debug mode")
	flagCong := flag.String("cc", "cubic", "Congestion control")
	flagBlock := flag.Bool("b", false, "Blocking call")
	flagReverse := flag.Bool("r", false, "Reverse send")
	flagFEC := flag.String("fec", "nofec", "FEC Scheme name")

	flagMultiplexer := flag.String("mplexer", "parallel", "Stream Multiplexer. Default: Parallel")

	flag.Parse()
	config := TrafficGenConfig{
		Mode:              *flagMode,
		RunTime:           *flagTime,
		CsizeDistro:       *flagCsizeDistro,
		CsizeValue:        *flagCsizeValue,
		ArrDistro:         *flagArrDistro,
		ArrValue:          *flagArrValue,
		Unlimited:         *flagUnlimited,
		Address:           *flagAddress,
		Protocol:          *flagProtocol,
		LogFolder:         *flagLog,
		IsMultipath:       *flagMultipath,
		IsMultiStream:     *flagMultiStream,
		Sched:             *flagSched,
		Debug:             *flagDebug,
		CongestionControl: *flagCong,
		IsBlockCall:       *flagBlock,
		IsReverse:         *flagReverse,
		StartDelay:        *flagStartDelay,
		FECScheme:         *flagFEC,
		multiplexer:       *flagMultiplexer,
	}
	utils.SetLogPerspective(*flagMode)
	if *flagDebug {
		utils.SetLogLevel(utils.LogLevelDebug)
		utils.SetLogTimeFormat(time.StampMilli)
	} else {
		utils.SetLogLevel(utils.LogLevelInfo)
	}

	LOG_PREFIX = *flagLog
	//quic.SetCongestionControl(*flagCong)
	//sched := schedNameConvert(*flagProtocol, *flagSched)
	logger.InitExperimentationLogger(*flagMode)
	if strings.ToLower(*flagMode) == "server" {
		//quic.SetSchedulerAlgorithm(sched)
		startServerMode(&config)
	} else {
		startClientMode(&config)
	}
	logger.FlushExperimentationLogger()
}
