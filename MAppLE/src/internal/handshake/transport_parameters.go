package handshake

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/qerr"
)

// errMalformedTag is returned when the tag value cannot be read
var errMalformedTag = qerr.Error(qerr.InvalidCryptoMessageParameter, "malformed Tag value")

// TransportParameters are parameters sent to the peer during the handshake
type TransportParameters struct {
	StreamFlowControlWindow     protocol.ByteCount
	ConnectionFlowControlWindow protocol.ByteCount

	MaxStreams uint32

	OmitConnectionID bool
	IdleTimeout      time.Duration

	MaxPathID protocol.PathID

	FECScheme protocol.FECSchemeID

	CacheHandshake bool
}

// readHelloMap reads the transport parameters from the tags sent in a gQUIC handshake message
func readHelloMap(tags map[Tag][]byte) (*TransportParameters, error) {
	params := &TransportParameters{}
	if value, ok := tags[TagTCID]; ok {
		v, err := utils.LittleEndian.ReadUint32(bytes.NewBuffer(value))
		if err != nil {
			return nil, errMalformedTag
		}
		params.OmitConnectionID = (v == 0)
	}
	if value, ok := tags[TagMIDS]; ok {
		v, err := utils.LittleEndian.ReadUint32(bytes.NewBuffer(value))
		if err != nil {
			return nil, errMalformedTag
		}
		params.MaxStreams = v
	}
	if value, ok := tags[TagICSL]; ok {
		v, err := utils.LittleEndian.ReadUint32(bytes.NewBuffer(value))
		if err != nil {
			return nil, errMalformedTag
		}
		params.IdleTimeout = utils.MaxDuration(protocol.MinRemoteIdleTimeout, time.Duration(v)*time.Second)
	}
	if value, ok := tags[TagSFCW]; ok {
		v, err := utils.LittleEndian.ReadUint32(bytes.NewBuffer(value))
		if err != nil {
			return nil, errMalformedTag
		}
		params.StreamFlowControlWindow = protocol.ByteCount(v)
	}
	if value, ok := tags[TagCFCW]; ok {
		v, err := utils.LittleEndian.ReadUint32(bytes.NewBuffer(value))
		if err != nil {
			return nil, errMalformedTag
		}
		params.ConnectionFlowControlWindow = protocol.ByteCount(v)
	}
	if value, ok := tags[TagMPID]; ok {
		v, err := utils.LittleEndian.ReadUint32(bytes.NewBuffer(value))
		if err != nil {
			return nil, errMalformedTag
		}
		params.MaxPathID = protocol.PathID(v)
	}
	if value, ok := tags[TagFSOP]; ok {
		v, err := utils.LittleEndian.ReadUint32(bytes.NewBuffer(value))
		if err != nil {
			return nil, errMalformedTag
		}
		params.FECScheme = protocol.FECSchemeID(v)
	}
	return params, nil
}

// GetHelloMap gets all parameters needed for the Hello message in the gQUIC handshake.
func (p *TransportParameters) getHelloMap() map[Tag][]byte {
	sfcw := bytes.NewBuffer([]byte{})
	utils.LittleEndian.WriteUint32(sfcw, uint32(p.StreamFlowControlWindow))
	cfcw := bytes.NewBuffer([]byte{})
	utils.LittleEndian.WriteUint32(cfcw, uint32(p.ConnectionFlowControlWindow))
	mids := bytes.NewBuffer([]byte{})
	utils.LittleEndian.WriteUint32(mids, p.MaxStreams)
	icsl := bytes.NewBuffer([]byte{})
	utils.LittleEndian.WriteUint32(icsl, uint32(p.IdleTimeout/time.Second))
	mpid := bytes.NewBuffer([]byte{})
	utils.LittleEndian.WriteUint32(mpid, uint32(p.MaxPathID))
	fsopt := bytes.NewBuffer([]byte{})
	utils.LittleEndian.WriteUint32(fsopt, uint32(p.FECScheme))

	tags := map[Tag][]byte{
		TagICSL: icsl.Bytes(),
		TagMIDS: mids.Bytes(),
		TagCFCW: cfcw.Bytes(),
		TagSFCW: sfcw.Bytes(),
		TagMPID: mpid.Bytes(),
		TagFSOP: fsopt.Bytes(),
	}
	if p.OmitConnectionID {
		tags[TagTCID] = []byte{0, 0, 0, 0}
	}
	return tags
}

// readTransportParameters reads the transport parameters sent in the QUIC TLS extension
func readTransportParamters(paramsList []transportParameter) (*TransportParameters, error) {
	params := &TransportParameters{}

	var foundInitialMaxStreamData bool
	var foundInitialMaxData bool
	var foundInitialMaxStreamID bool
	var foundIdleTimeout bool

	for _, p := range paramsList {
		switch p.Parameter {
		case initialMaxStreamDataParameterID:
			foundInitialMaxStreamData = true
			if len(p.Value) != 4 {
				return nil, fmt.Errorf("wrong length for initial_max_stream_data: %d (expected 4)", len(p.Value))
			}
			params.StreamFlowControlWindow = protocol.ByteCount(binary.BigEndian.Uint32(p.Value))
		case initialMaxDataParameterID:
			foundInitialMaxData = true
			if len(p.Value) != 4 {
				return nil, fmt.Errorf("wrong length for initial_max_data: %d (expected 4)", len(p.Value))
			}
			params.ConnectionFlowControlWindow = protocol.ByteCount(binary.BigEndian.Uint32(p.Value))
		case initialMaxStreamIDParameterID:
			foundInitialMaxStreamID = true
			if len(p.Value) != 4 {
				return nil, fmt.Errorf("wrong length for initial_max_stream_id: %d (expected 4)", len(p.Value))
			}
			// TODO: handle this value
		case idleTimeoutParameterID:
			foundIdleTimeout = true
			if len(p.Value) != 2 {
				return nil, fmt.Errorf("wrong length for idle_timeout: %d (expected 2)", len(p.Value))
			}
			params.IdleTimeout = utils.MaxDuration(protocol.MinRemoteIdleTimeout, time.Duration(binary.BigEndian.Uint16(p.Value))*time.Second)
		case omitConnectionIDParameterID:
			if len(p.Value) != 0 {
				return nil, fmt.Errorf("wrong length for omit_connection_id: %d (expected empty)", len(p.Value))
			}
			params.OmitConnectionID = true
		case maxPathIDParameterID:
			if len(p.Value) != 1 {
				return nil, fmt.Errorf("wrong length for max_path_id: %d (expected 1)", len(p.Value))
			}
			params.MaxPathID = protocol.PathID(uint8(p.Value[0]))
		case fecSchemeParameterID:
			if len(p.Value) != 1 {
				return nil, fmt.Errorf("wrong length for fec_scheme_parameter_id: %d (expected 1)", len(p.Value))
			}
			params.FECScheme = protocol.FECSchemeID(p.Value[0])
		}
	}

	if !(foundInitialMaxStreamData && foundInitialMaxData && foundInitialMaxStreamID && foundIdleTimeout) {
		return nil, errors.New("missing parameter")
	}
	return params, nil
}

// GetTransportParameters gets the parameters needed for the TLS handshake.
func (p *TransportParameters) getTransportParameters() []transportParameter {
	initialMaxStreamData := make([]byte, 4)
	binary.BigEndian.PutUint32(initialMaxStreamData, uint32(p.StreamFlowControlWindow))
	initialMaxData := make([]byte, 4)
	binary.BigEndian.PutUint32(initialMaxData, uint32(p.ConnectionFlowControlWindow))
	initialMaxStreamID := make([]byte, 4)
	// TODO: use a reasonable value here
	binary.BigEndian.PutUint32(initialMaxStreamID, math.MaxUint32)
	idleTimeout := make([]byte, 2)
	binary.BigEndian.PutUint16(idleTimeout, uint16(p.IdleTimeout/time.Second))
	maxPacketSize := make([]byte, 2)
	binary.BigEndian.PutUint16(maxPacketSize, uint16(protocol.MaxReceivePacketSize))
	maxPathID := make([]byte, 1)
	maxPathID[0] = uint8(p.MaxPathID)
	fecScheme := []byte{uint8(p.FECScheme)}
	params := []transportParameter{
		{initialMaxStreamDataParameterID, initialMaxStreamData},
		{initialMaxDataParameterID, initialMaxData},
		{initialMaxStreamIDParameterID, initialMaxStreamID},
		{idleTimeoutParameterID, idleTimeout},
		{maxPacketSizeParameterID, maxPacketSize},
		{maxPathIDParameterID, maxPathID},
		{fecSchemeParameterID, fecScheme},
	}
	if p.OmitConnectionID {
		params = append(params, transportParameter{omitConnectionIDParameterID, []byte{}})
	}
	return params
}
