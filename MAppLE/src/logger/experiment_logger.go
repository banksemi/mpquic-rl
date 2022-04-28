package logger

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go/internal/protocol"
)

// this module records some internal information
// it can be controlled by the application using mpquic-fec

type experimentationLogger struct {
	lock          sync.Mutex
	streamGapsLog *bufio.Writer
	fecLog        *bufio.Writer
	fecConfigLog  *bufio.Writer
	packetLog     *bufio.Writer
	cwndLog       *bufio.Writer
	delayEstimatorLog *bufio.Writer
}

var experimentationLoggerSingleton *experimentationLogger = nil

func newLogger(name, heading, prefix string) *bufio.Writer {
	logger, err := os.OpenFile(prefix+"_"+name+".csv", os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		panic(err)
	}
	loggerWriter := bufio.NewWriter(logger)
	loggerWriter.WriteString(heading + "\n")

	return loggerWriter
}

func InitExperimentationLogger(prefix string) {
	gapsFileWriter := newLogger("gaps", "streamID,gaps,timestamp", prefix)
	fecFileWriter := newLogger("fec", "fecBlock,event,timestamp", prefix)
	fecConfigFileWriter := newLogger("fecConfig", "source,repair,windowStep,timestamp", prefix)
	packetFileWriter := newLogger("packet", "size,destination,fec,timestamp", prefix)
	cwndFileWriter := newLogger("cwnd", "path,cwnd,free,timestamp", prefix)
	estimatorFileWriter := newLogger("delay-estimator", "id, delay, timestamp", prefix)

	experimentationLoggerSingleton = &experimentationLogger{
		streamGapsLog: gapsFileWriter,
		fecLog:        fecFileWriter,
		fecConfigLog:  fecConfigFileWriter,
		packetLog:     packetFileWriter,
		cwndLog:       cwndFileWriter,
		delayEstimatorLog: estimatorFileWriter,
	}
}

func FlushExperimentationLogger() {
	if experimentationLoggerSingleton == nil {
		return
	}

	experimentationLoggerSingleton.lock.Lock()
	experimentationLoggerSingleton.streamGapsLog.Flush()
	experimentationLoggerSingleton.fecLog.Flush()
	experimentationLoggerSingleton.fecConfigLog.Flush()
	experimentationLoggerSingleton.packetLog.Flush()
	experimentationLoggerSingleton.cwndLog.Flush()
	experimentationLoggerSingleton.delayEstimatorLog.Flush()
	experimentationLoggerSingleton.lock.Unlock()
}

func ExpLogInsertGapsInfo(streamID protocol.StreamID, gapsCount int) {
	if experimentationLoggerSingleton == nil {
		return
	}

	timestamp := time.Now().UnixNano()

	line := fmt.Sprintf("%d,%d,%d\n", streamID, gapsCount, timestamp)

	experimentationLoggerSingleton.lock.Lock()
	experimentationLoggerSingleton.streamGapsLog.WriteString(line)
	experimentationLoggerSingleton.lock.Unlock()
}

func ExpLogInsertFECEvent(fecBlock protocol.FECBlockNumber, event string) {
	if experimentationLoggerSingleton == nil {
		return
	}

	timestamp := time.Now().UnixNano()

	line := fmt.Sprintf("%d,%s,%d\n", fecBlock, event, timestamp)

	experimentationLoggerSingleton.lock.Lock()
	experimentationLoggerSingleton.fecLog.WriteString(line)
	experimentationLoggerSingleton.lock.Unlock()
}

func ExpLogInsertFECConfig(nSourceSymbols, nRepairSymbols, windowStepSize uint) {
	if experimentationLoggerSingleton == nil {
		return
	}

	timestamp := time.Now().UnixNano()

	line := fmt.Sprintf("%d,%d,%d,%d\n", nSourceSymbols, nRepairSymbols, windowStepSize, timestamp)

	experimentationLoggerSingleton.lock.Lock()
	experimentationLoggerSingleton.fecConfigLog.WriteString(line)
	experimentationLoggerSingleton.lock.Unlock()
}

func ExpLogInsertPacket(size int, destination net.Addr, fec bool) {
	if experimentationLoggerSingleton == nil {
		return
	}

	timestamp := time.Now().UnixNano()

	line := fmt.Sprintf("%d,%s,%t,%d\n", size, destination.String(), fec, timestamp)

	experimentationLoggerSingleton.lock.Lock()
	experimentationLoggerSingleton.packetLog.WriteString(line)
	experimentationLoggerSingleton.lock.Unlock()
}

func ExpLogInsertCwnd(path net.Addr, cwnd, free protocol.ByteCount) {
	if experimentationLoggerSingleton == nil {
		return
	}

	timestamp := time.Now().UnixNano()

	line := fmt.Sprintf("%s,%d,%d,%d\n", path.String(), cwnd, free, timestamp)

	experimentationLoggerSingleton.lock.Lock()
	experimentationLoggerSingleton.cwndLog.WriteString(line)
	experimentationLoggerSingleton.lock.Unlock()
}

func ExpLogInsertDelayEstimation (messageID int, delay int) {
	if experimentationLoggerSingleton == nil {
		return
	}

	timestamp := time.Now().UnixNano()

	line := fmt.Sprintf("%d,%d,%d\n", messageID, delay, timestamp)

	experimentationLoggerSingleton.lock.Lock()
	experimentationLoggerSingleton.delayEstimatorLog.WriteString(line)
	experimentationLoggerSingleton.lock.Unlock()
}
