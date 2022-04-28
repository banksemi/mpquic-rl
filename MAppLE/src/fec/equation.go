package fec

import (
	"github.com/lucas-clemente/quic-go/internal/protocol"
)

type Variable struct {
	Packet []byte
	ID     protocol.FECEncodingSymbolID
}

func NewEquation(symbol *RepairSymbol) *Equation {
	return &Equation{
		fecPayloadID: protocol.NewConvolutionalRepairFECPayloadID(symbol.FECSchemeSpecific, symbol.EncodingSymbolID),
		lastVarID:    symbol.EncodingSymbolID,
		numberOfVars: int(symbol.NumberOfPackets),
		repairSymbol: symbol,
	}
}

// the variables variables of the equation are a repair symbol on one side, and packets with consecutive fec payload IDs on the other side
// R = Relation(P1, P2, P3, ..., Pn)
type Equation struct {
	fecPayloadID protocol.FECPayloadID
	lastVarID    protocol.FECEncodingSymbolID
	numberOfVars int
	repairSymbol *RepairSymbol // this is the constant term of the equation
}

// returns a tuble (a, b) where a is the FEC payload ID of the first variable of the equation, and b is the one of the last variable of the equation
func (e *Equation) Bounds() (protocol.FECEncodingSymbolID, protocol.FECEncodingSymbolID) {
	return (e.lastVarID - protocol.FECEncodingSymbolID(e.numberOfVars)) + 1, e.lastVarID
}

func (e *Equation) ID() protocol.FECEncodingSymbolID {
	return e.lastVarID
}

// pre: we can access to the variable with ID i in array vars with vars[i % len(vars)]
// post: the returned slice contains the variables in order, in a 0-indexed array.
//			 if a variable is unknown, it is set to nil
func (e *Equation) GetVariables(vars []*Variable) []*Variable {
	begin, end := e.Bounds()
	retVal := make([]*Variable, end-begin+1)
	for i := begin; i <= end; i++ {
		candidate := vars[i%protocol.FECEncodingSymbolID(len(vars))]
		// if the ID does not match, it is an old remaining value in the array: the variable is thus unknown
		if candidate != nil && candidate.ID == i {
			retVal[i-begin] = candidate
		}
	}
	return retVal
}
