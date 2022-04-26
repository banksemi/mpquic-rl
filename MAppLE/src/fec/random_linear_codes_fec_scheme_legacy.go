package fec

import (
	"errors"
	"fmt"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"math/big"
)

type randomLinearFECSchemeLegacy struct {
	repairKey                   uint16
	densityThreshold            uint8
	system                      [][]big.Rat
	unknownVariables            map[protocol.FECEncodingSymbolID]int
	reverseIndexes              []protocol.FECEncodingSymbolID
	unknownVariablesPerEquation []int // for each equation, tells the number of unknown variables (non-zero coefficients)
	highestReceivedEquation     *Equation
}

func NewRandomLinearFECSchemeLegacy() ConvolutionalFECScheme {
	return &randomLinearFECSchemeLegacy{
		repairKey:        1,
		densityThreshold: 15,
		unknownVariables: make(map[protocol.FECEncodingSymbolID]int),
	}
}

var _ ConvolutionalFECScheme = &randomLinearFECSchemeLegacy{}

func (f *randomLinearFECSchemeLegacy) getFECSchemeSpecific() FECSchemeSpecific {
	rk := f.getAndIncrementRepairKey()
	DT := f.densityThreshold
	return FECSchemeSpecific((uint32(rk) << 4) + uint32(DT))
}

func (f *randomLinearFECSchemeLegacy) getAndIncrementRepairKey() uint16 {
	retVal := f.repairKey
	f.repairKey++
	if f.repairKey == 0 {
		f.repairKey++
	}
	return retVal
}

func (f *randomLinearFECSchemeLegacy) GetRepairSymbols(fecContainer FECContainer, numberOfSymbols uint, id protocol.FECEncodingSymbolID) ([]*RepairSymbol, error) {
	if numberOfSymbols > 0xFF {
		return nil, errors.New(fmt.Sprintf("impossible to generate %d repair symbols with RLC (max %d allowed)", numberOfSymbols, 0xFF))
	}
	var rs []*RepairSymbol
	for i := uint(0); i < numberOfSymbols; i++ {
		fecSchemeSpecific := f.getFECSchemeSpecific()
		coefficients, err := getEquationCoefficients(fecSchemeSpecific, fecContainer.CurrentNumberOfPackets())
		if err != nil {
			return nil, err
		}
		equationConstantTerm := &big.Rat{}
		for i, p := range fecContainer.GetPackets() {
			packet := (&big.Rat{}).SetInt((&big.Int{}).SetBytes(p))
			equationConstantTerm.Add(equationConstantTerm, packet.Mul(packet, &coefficients[i]))
		}
		rs = append(rs, &RepairSymbol{
			FECSchemeSpecific:     uint32(fecSchemeSpecific),
			Data:                  equationConstantTerm.Num().Bytes(),
			NumberOfPackets:       uint8(fecContainer.CurrentNumberOfPackets()),
			NumberOfRepairSymbols: uint8(numberOfSymbols),
			Convolutional:         true,
			EncodingSymbolID:      id,
			SymbolNumber:          uint8(i),
		})
	}
	return rs, nil
}

func (f *randomLinearFECSchemeLegacy) CanRecoverPackets() bool {
	if len(f.system) == 0 {
		return false
	}
	if len(f.system) == len(f.system[0])-1 {
		// the system is square (it has as many unknown variables as equations), so there is only a small chance that
		// the equations are colinear
		return true
	}
	// search for a square subsystem in this system nxm
	// the square subsystem can't have more than m unknown variables or it won't be square, because there are m equations
	if len(f.equationsWithLessOrEqualUnknowns(1)) >= 1 {
		// there are equations with only one unknown
		return true
	}
	for i := 2; i <= len(f.system); i++ {
		// get all equations with <= i unknown variables
		candidateEquations := f.equationsWithLessOrEqualUnknowns(i)
		if len(candidateEquations) < i {
			// there are not enough equations, this can't be a subsystem
			continue
		}
		for j := 0; j < len(candidateEquations); j++ {
			numberOfEquationsConcerningTheSameVariables := 1
			for k := j + 1; k < len(candidateEquations); k++ {
				if f.concernTheSameVariables(candidateEquations[j], candidateEquations[k]) {
					numberOfEquationsConcerningTheSameVariables++
				}
				if numberOfEquationsConcerningTheSameVariables >= i {
					// we found an ixi subsystem
					return true
				}
				remainingCandidates := len(candidateEquations) - (k + 1)
				if i-numberOfEquationsConcerningTheSameVariables > remainingCandidates {
					// not enough remaining candidates to find a subsystem with j
					break
				}
			}
		}
	}
	return false
}

// returns the indexes of equations that have a number of unknown variables less or equal to n
func (f *randomLinearFECSchemeLegacy) equationsWithLessOrEqualUnknowns(n int) []int {
	var retVal []int
	for i, nUnknowns := range f.unknownVariablesPerEquation {
		if nUnknowns > 0 && nUnknowns <= n {
			retVal = append(retVal, i)
		}
	}
	return retVal
}

// returns true if equation i concerns the same variables as equation j
// it is the case as soon as all the non-zero coefficients variables of one of the two equations have non-zero coefficients in the other equation
func (f *randomLinearFECSchemeLegacy) concernTheSameVariables(i int, j int) bool {
	var biggest, smallest int
	if f.unknownVariablesPerEquation[i] > f.unknownVariablesPerEquation[j] {
		biggest, smallest = i, j
	} else {
		biggest, smallest = j, i
	}
	zero := big.Rat{}
	for variableIndex, variableCoef := range f.system[smallest] {
		if (&variableCoef).Cmp(&zero) != 0 && (&f.system[biggest][variableIndex]).Cmp(&zero) == 0 {
			// there is a non-zero coefficient in the smallest equation that is not non-zero in the biggest one
			return false
		}
	}
	// the non-zero coefs of smallest are all also non-zero in biggest
	return true
}

func (f *randomLinearFECSchemeLegacy) RemoveTrailingZeroes() {
	if len(f.system) == 0 {
		return
	}
	nTrailingZeroes := 0
	defer func() {
		if nTrailingZeroes > 0 {
			newSystem := make([][]big.Rat, len(f.system))
			for i, eq := range f.system {
				newSystem[i] = eq[nTrailingZeroes:]
			}
			for _, id := range f.reverseIndexes[:nTrailingZeroes] {
				delete(f.unknownVariables, id)
			}
			for id, v := range f.unknownVariables {
				f.unknownVariables[id] = v - nTrailingZeroes
			}
			f.reverseIndexes = f.reverseIndexes[nTrailingZeroes:]

			f.system = newSystem
		}
	}()
	zero := &big.Rat{}
	for i := range f.system[0] {
		for j := 0; j < len(f.system); j++ {
			if f.system[j][i].Cmp(zero) != 0 {
				return
			}
		}
		nTrailingZeroes++
	}
}

func (f *randomLinearFECSchemeLegacy) eqFullOfZeros(eq []big.Rat) bool {
	zero := &big.Rat{}
	for _, v := range eq {
		if v.Cmp(zero) != 0 {
			return false
		}
	}
	return true
}

func (f *randomLinearFECSchemeLegacy) RecoverPackets(ignoreBelow protocol.FECEncodingSymbolID) ([][]byte, error) {
	if len(f.system) == 0 {
		// nothing to do
		return nil, nil
	}

	f.RemoveTrailingZeroes()
	system := f.system
	res, echelonSystem, solvedEquations, err := SolveGaussianBigInt(system)
	if err != nil {
		return nil, err
	}

	// the echelon system has the shape (if it was a 3x4 system) :
	//  0  0  0  0  x4 x5 v0
	//	0  0  0  x3  0  0 v1
	//  0  0  x2 0   0  0 v2
	//  x0 x1 0  0   0  0 v3
	//
	//	so we can remove the equations 1 and 2 and the variables 2 and 3 from the variables of equation 1
	// so after elimination we have the following ultra simple system:
	//
	// x0 x1 v3
	//

	// update the system with its echelon version
	var recoveredIndexes = make([]bool, len(system[0]))
	nRecoveredVariables := 0
	var retVal [][]byte
	// for each recovered variable, we must pass it to the constant term side of each equation.
	for i, r := range res {
		if len(r) == 1 && r[0] != nil {
			oldSys := f.system
			f.system = echelonSystem
			if f.reverseIndexes[i] >= ignoreBelow {
				retVal = append(retVal, r[0].Num().Bytes())
				f.AddKnownVariable(&Variable{
					Packet: retVal[len(retVal)-1],
					ID:     f.reverseIndexes[i],
				})
			}
			f.system = oldSys
			// this variable has been recovered
			recoveredIndexes[i] = true
			nRecoveredVariables++
			// not unknown anymore
			//delete(f.unknownVariables, f.reverseIndexes[i])
		}
	}
	if len(f.unknownVariables) == 0 {
		// everything has been recovered !
		// this should be the majority of the cases
		// flush the system
		f.system = nil
		f.unknownVariablesPerEquation = nil
		// flush the unknownVariables
		// flush the reverse indexes
		f.reverseIndexes = nil
		return retVal, nil
	}
	nUnknownVariables := len(f.unknownVariables)
	newUnknownVariables := make(map[protocol.FECEncodingSymbolID]int)
	if nRecoveredVariables == 0 {
		f.system = echelonSystem
		f.computeNumberOfUnknownVariablesPerEquation()
		return nil, nil
	}
	// There are remaining unknown variables :'(
	// As we have now the system in its echelon form, we know that we can discard the equations that have a non-zero value at the index of a recovered variable
	// now, let's transform out current system into the easy system :
	// recompute the new indexes of the variables, without taking the solved ones into account (as their coefficient will now be zero)
	newRevIndexes := make([]protocol.FECEncodingSymbolID, 0, nUnknownVariables)
	for i, varID := range f.reverseIndexes {
		if _, ok := f.unknownVariables[varID]; ok && !recoveredIndexes[i] {
			// this variable is still unknown, change its index in the unknown vars and set the new reverseIndexes structure
			newUnknownVariables[varID] = len(newRevIndexes)
			newRevIndexes = append(newRevIndexes, varID)
		}
	}
	f.unknownVariables = newUnknownVariables
	var newSystem [][]big.Rat
	for i, eq := range echelonSystem {
		// if this equation is still not solved
		if !solvedEquations[i] && !f.eqFullOfZeros(eq) {
			newEq := make([]big.Rat, len(f.unknownVariables)+1)
			for j, value := range eq[:len(eq)-1] {
				// get the varID of this variable in the old indexing system
				varID := f.reverseIndexes[j]
				// if the variable is still unknown, get its new index and put it in the equation
				if newIndex, ok := f.unknownVariables[varID]; ok {
					newEq[newIndex] = value
				}
			}
			newEq[len(newEq)-1] = eq[len(eq)-1]
			// add the new, cleaned equation to the system
			newSystem = append(newSystem, newEq)
		}
	}
	// update our system with the new, cleaned system, shorter
	f.system = newSystem

	// update our reverseIndexes structure
	f.reverseIndexes = newRevIndexes
	f.RemoveTrailingZeroes()

	f.computeNumberOfUnknownVariablesPerEquation()
	return retVal, nil
}

// pre: len(unknownVariables) > 0 && unknownVariables is sorted in ascending order
func (f *randomLinearFECSchemeLegacy) AddEquation(eq *Equation, unknownVariables []protocol.FECEncodingSymbolID, variables []*Variable) {
	numberOfNewUnknownVariables := 0
	smallestUnknownVariableID := unknownVariables[0]
	var highestVariableInSystem protocol.FECEncodingSymbolID
	for id := range f.unknownVariables {
		if id > highestVariableInSystem {
			highestVariableInSystem = id
		}
	}
	if len(f.system) > 0 && f.highestReceivedEquation != nil && smallestUnknownVariableID > highestVariableInSystem {
		// flush the system: the equations that we will receive won't help for this system anymore.
		// TODO: with jitter, this is not necessarily true that we won't receive help for this system
		f.system = nil
		f.unknownVariablesPerEquation = nil
		f.reverseIndexes = nil
		f.unknownVariables = make(map[protocol.FECEncodingSymbolID]int)
	}
	for _, varID := range unknownVariables {
		if _, ok := f.unknownVariables[varID]; !ok {
			// this is an unknown variable that we didn't have in our system
			f.unknownVariables[varID] = len(f.unknownVariables)
			f.reverseIndexes = append(f.reverseIndexes, varID)
			numberOfNewUnknownVariables++
		}
	}
	if numberOfNewUnknownVariables == 0 && len(f.system) == len(f.unknownVariables) {
		// should we really quit ? It is useless to consider an equation if we already have a full system, excepted if some
		// equations are colinear, which is not likely but could happen.
		return
	} else {
		if f.highestReceivedEquation == nil {
			f.highestReceivedEquation = eq
		}
		if eq.lastVarID > f.highestReceivedEquation.lastVarID {
			f.highestReceivedEquation = eq
		}

		// either we have at least one new unknown variable, either or system is not square

		// go from a system like :
		// a*x1 + b*x2  + 0*x3 - c = 0
		// 0*x1 + e*x2  + f*x3 - g = 0
		//
		// to a system like :
		// a*x1 + b*x2 + 0*x3 + 0*x4 - c = 0
		// 0*x1 + e*x2 + f*x3 + 0*x4 - g = 0
		// 0*x1 + 0*x2 + h*x3 + i*x4 - k = 0
		//
		// or better, if we are at the end of a loss burst, a system like :
		// a*x1 + b*x2  + 0*x3 - c = 0
		// 0*x1 + e*x2  + f*x3 - g = 0
		// 0*x1 + 0*x2  + h*x3 - 1 = 0
		// that we will be able to solve

		// here, if we have at lease one new unknown variable, we have to make place for it at the end of the equations
		if numberOfNewUnknownVariables > 0 {
			for i := range f.system {
				// remember the constant term of this equation
				constantTerm := f.system[i][len(f.system[i])-1]
				// set 0 at the previous place of the constant term
				f.system[i][len(f.system[i])-1] = big.Rat{}
				for j := 0; j < numberOfNewUnknownVariables-1; j++ {
					// set 0 to the place of the new unknown variable in the old equation
					f.system[i] = append(f.system[i], big.Rat{})
				}
				// re-put the constant term at the end
				f.system[i] = append(f.system[i], constantTerm)
			}
		}
	}

	// here, we have one new equation (eq), with potentially new variables
	equationConstantTerm := (&big.Rat{}).SetInt((&big.Int{}).SetBytes(eq.repairSymbol.Data))
	fecSchemeSpecific := FECSchemeSpecific(eq.fecPayloadID.GetFECSchemeSpecific())
	coefficients, err := getEquationCoefficients(fecSchemeSpecific, eq.numberOfVars)
	if err != nil {
		return
	}

	length := len(f.reverseIndexes)
	currentEquation := make([]big.Rat, length, length+1) // let one additional place for the constant term
	begin, end := eq.Bounds()
	for i := begin; i <= end; i++ {
		candidate := variables[i%protocol.FECEncodingSymbolID(len(variables))]
		coef := coefficients[i-begin]
		// we assume that it is faster to do this than using the hashmap to determine if the variable is known or not. This may not necessarily be the case.
		if candidate != nil && candidate.ID == i {
			// the variable is not unknown: consider it as a constant
			value := (&big.Rat{}).SetInt((&big.Int{}).SetBytes(candidate.Packet))
			value = value.Mul(value, &coef)
			equationConstantTerm = equationConstantTerm.Sub(equationConstantTerm, value) // we pass the variable to the constant term side
			// We do not need to set something in the currentEquation, as it is a constant term
		} else {
			// we set the variable at the right place in the equation
			currentEquation[f.unknownVariables[i]] = coef
		}
	}
	currentEquation = append(currentEquation, *equationConstantTerm)
	// add the equation to the system
	f.system = append(f.system, currentEquation)
	// set the number of unknown variables of the system
	//f.unknownVariablesPerEquation = append(f.unknownVariablesPerEquation, numberOfNewUnknownVariables)
	f.computeNumberOfUnknownVariablesPerEquation()
}

func (f *randomLinearFECSchemeLegacy) AddKnownVariable(v *Variable) bool {
	if index, ok := f.unknownVariables[v.ID]; !ok {
		// this is not an unknown variable of our system, nothing to do
		return false
	} else {
		var newSystem [][]big.Rat
		zero := big.Rat{}
		// for each equation having this variable as an unknown, put it to the constant term
		for i, eq := range f.system {
			coeff := &eq[index]
			if coeff.Cmp(&zero) != 0 {
				// the coefficient is not zero: the equation wants to know the value of this variable
				constantTerm := eq[len(eq)-1]
				varValue := (&big.Rat{}).SetInt((&big.Int{}).SetBytes(v.Packet))
				(&constantTerm).Sub(&constantTerm, varValue.Mul(varValue, coeff))
				eq[len(eq)-1] = constantTerm
				// now, set the coefficient of this variable to zero, as the variable is not unknown anymore
				eq[index] = big.Rat{}
				// there is one unknown variable less in this equation
				f.unknownVariablesPerEquation[i]--
				if f.unknownVariablesPerEquation[i] > 0 {
					newSystem = append(newSystem, eq)
				}
			}
		}
		delete(f.unknownVariables, v.ID)
		f.system = newSystem
		// we don't want to update f.reverseIndexes as we didn't shrink the system, as it would take too much time
		// everything will be shrunk at the next system solving
	}

	return true
}

func (f *randomLinearFECSchemeLegacy) computeNumberOfUnknownVariablesPerEquation() {
	f.unknownVariablesPerEquation = make([]int, len(f.system))
	zero := big.Rat{}
	for i, eq := range f.system {
		for _, coef := range eq[:len(eq)-1] {
			if (&coef).Cmp(&zero) != 0 {
				// the variable is unknown
				f.unknownVariablesPerEquation[i]++
			}
		}
	}
}

// pre: the equations slice must be sorted by fecPayloadID
// FIXME: this function works but is too heavy to be used anymore
func buildSystemDeprecated(equations []*Equation, variables []*Variable, ignoreBelow protocol.FECEncodingSymbolID) ([][]big.Rat, []bool, error) {
	retVal := make([][]big.Rat, 0, len(equations))
	lowest, _ := equations[0].Bounds()
	_, highest := equations[len(equations)-1].Bounds()

	nUnknownVariables := 0
	unknownVariablesIndexes := make(map[protocol.FECEncodingSymbolID]int)
	var ignoreVariables []bool
	for i := lowest; i <= highest; i++ {
		candidate := variables[i%protocol.FECEncodingSymbolID(len(variables))]
		if candidate == nil || candidate.ID != i {
			unknownVariablesIndexes[i] = nUnknownVariables
			nUnknownVariables++
			ignoreVariables = append(ignoreVariables, i < ignoreBelow)
		}
	}

	if nUnknownVariables == 0 {
		return retVal, nil, nil
	}
	// do the loop in reverse to avoid recovering packets to ignore as much as possible
	for idx := len(equations) - 1; idx >= 0; idx-- {
		eq := equations[idx]
		equationConstantTerm := (&big.Rat{}).SetInt((&big.Int{}).SetBytes(eq.repairSymbol.Data))
		fecSchemeSpecific := FECSchemeSpecific(eq.fecPayloadID.GetFECSchemeSpecific())
		coefficients, err := getEquationCoefficients(fecSchemeSpecific, eq.numberOfVars)
		if err != nil {
			return nil, nil, err
		}
		currentEquation := make([]big.Rat, nUnknownVariables, nUnknownVariables+1) // let one additional place for the constant term
		begin, end := eq.Bounds()
		nVarKnown := 0
		for i := begin; i <= end; i++ {
			candidate := variables[i%protocol.FECEncodingSymbolID(len(variables))]
			coef := coefficients[i-begin]
			if candidate != nil && candidate.ID == i {
				// the variable is not unknown: consider it as a constant
				value := (&big.Rat{}).SetInt((&big.Int{}).SetBytes(candidate.Packet))
				value = value.Mul(value, &coef)
				equationConstantTerm = equationConstantTerm.Sub(equationConstantTerm, value) // we pass the variable to the constant term side
				//currentEquation[i-lowest] = big.Rat{} // the coefficient is thus 0 for this variable
				nVarKnown++
			} else {
				currentEquation[unknownVariablesIndexes[i]] = coef
			}
		}
		currentEquation = append(currentEquation, *equationConstantTerm)
		if nVarKnown < eq.numberOfVars {
			// there are unknown variables for this equation
			retVal = append(retVal, currentEquation)
		}
		// we cannot give a system with more equations than variables
		if len(retVal) == nUnknownVariables {
			// break
		}
	}

	return retVal, ignoreVariables, nil
}

func getEquationCoefficients(infos FECSchemeSpecific, numberOfCoefficients int) ([]big.Rat, error) {
	// TODO: implement the true PRNG as in the RLC draft
	coefs := make([]uint8, numberOfCoefficients)
	err := generate_coding_coefficients(infos.GetRepairKey(), coefs, uint16(numberOfCoefficients), infos.GetDensityThreshold())
	if err != nil {
		return nil, err
	}

	var toReturn []big.Rat
	for _, c := range coefs {
		toReturn = append(toReturn, *(&big.Rat{}).SetInt64(int64(c)))
	}
	return toReturn, nil
}

type pmms struct {
	lastState uint32
}

func (p *pmms) seed(s uint32) {
	p.lastState = s
}

// see https://tools.ietf.org/html/draft-ietf-tsvwg-rlc-fec-scheme-02 section 3.4
func (p *pmms) next(maxv uint32) uint32 {
	rawValue := p.pmms()
	scaledValue := uint32(float64(maxv) * float64(rawValue) / float64(0x7FFFFFFF))
	return scaledValue
}

// see https://tools.ietf.org/html/draft-ietf-tsvwg-rlc-fec-scheme-02 section 3.4
func (p *pmms) pmms() uint32 {
	Ij := p.lastState
	const A uint64 = 16807
	const M uint32 = 2147483647
	p.lastState = uint32((A * uint64(Ij)) % uint64(M))
	return p.lastState
}

func generate_coding_coefficients(id uint16, cc_tab []uint8, cc_nb uint16, dt uint8) error {
	const m = 8
	if dt > 15 {
		return errors.New("bad dt parameter")
	}
	if id == 0 && dt != 15 && m != 2 {
		return errors.New("bad repair_key parameter")
	}
	pmms := &pmms{}
	pmms.seed(uint32(id))
	if dt == 15 {
		/* coefficient 0 is avoided here in order to include
		 * all the source symbols */
		for i := uint16(0); i < cc_nb; i++ {
			for cc_tab[i] == 0 {
				cc_tab[i] = uint8(pmms.next(256))
			}
		}

	} else {
		/* here a certain fraction of coefficients should be 0 */
		for i := uint16(0); i < cc_nb; i++ {
			if uint8(pmms.next(16)) <= dt {
				for cc_tab[i] == 0 {
					cc_tab[i] = uint8(pmms.next(256))
				}
			} else {
				cc_tab[i] = 0
			}
		}
	}
	return nil
}
