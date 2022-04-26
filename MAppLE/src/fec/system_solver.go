package fec

// Modified from https://github.com/alex-ant/gomath

import (
	"errors"
	"fmt"

	"math/big"
)

// SolveGaussian solves the system of linear equations via The Gaussian Elimination.
func SolveGaussianBigInt(eqM [][]big.Rat) (res [][]*big.Rat, resultEqM [][]big.Rat, solvedEquations []bool, err error) {
	if len(eqM) > len(eqM[0])-1 {
		err = errors.New("the number of equations can not be greater than the number of variables")
		return
	}

	var zero big.Rat

	dl, i, j := containsDuplicatesLinesBigInt(eqM)
	if dl {
		err = fmt.Errorf("provided matrix contains duplicate lines (%d and %d)", i+1, j+1)
		return
	}

	for i := 0; i < len(eqM)-1; i++ {
		eqM = sortMatrixBigInt(eqM, i, true)

		var varC big.Rat
		varCZero := false
		for k := i; k < len(eqM); k++ {
			if k == i {
				varC = *new(big.Rat).Set(&eqM[k][i])
				varCZero = (&varC).Cmp(&zero) == 0
			} else {
				multipliedLine := make([]big.Rat, len(eqM[i]))
				for z, zv := range eqM[i] {
					newVar := *new(big.Rat)
					// hack: 0/0 == 1 to stick with the implementation of rationals by gomath
					if varCZero && (&eqM[k][i]).Cmp(&zero) == 0 {
						// num and denum are 0, so it gives 1
						newVar.SetInt64(1)
					} else {
						newVar.Quo(&eqM[k][i], &varC)
					}
					multipliedLine[z] = *newVar.Neg(newVar.Mul(&zv, &newVar))
				}
				newLine := make([]big.Rat, len(eqM[k]))
				for z, zv := range eqM[k] {
					newVar := *new(big.Rat)
					newLine[z] = *(newVar.Add(&zv, &multipliedLine[z]))
				}
				eqM[k] = newLine
			}
		}
	}

	// Removing empty lines and inverting the matrix.
	for i := len(eqM) - 1; i >= 0; i-- {
		if !containsOnlyZero(eqM[i]) {
			resultEqM = append(resultEqM, eqM[i])
		}
	}

	getFirstNonZeroIndex := func(sl []big.Rat) (index int) {
		zero := &big.Rat{}
		for i, v := range sl {
			if (&v).Cmp(zero) != 0 {
				index = i
				return
			}

		}
		return -1
	}

	// Back substitution.
	for z := 0; z < len(resultEqM)-1; z++ {
		var processIndex int
		var firstLine []big.Rat
		for i := z; i < len(resultEqM); i++ {
			v := resultEqM[i]
			if i == z {
				processIndex = getFirstNonZeroIndex(v)
				firstLine = v
			} else if processIndex != -1 {
				newVar := *new(big.Rat)
				mult := safeQuotient(&newVar, &v[processIndex], &firstLine[processIndex], &zero).Neg(&newVar)
				// mult := newVar.Quo(&v[processIndex], &firstLine[processIndex]).Neg(&newVar)
				for j, jv := range v {
					newVar2 := *new(big.Rat)
					resultEqM[i][j] = *newVar2.Mul(&firstLine[j], mult).Add(&newVar2, &jv)
				}
			}
		}
	}

	resultEqM = sortMatrixBigInt(resultEqM, 0, true)

	newSystem := make([][]big.Rat, 0, len(resultEqM))
	// Removing empty lines and inverting the matrix.
	for i, _ := range resultEqM {
		if !containsOnlyZero(resultEqM[i]) {
			newSystem = append(newSystem, resultEqM[i])
		}
	}

	resultEqM = newSystem

	solvedEquations = make([]bool, len(resultEqM))

	// Calculating variables.
	res = make([][]*big.Rat, len(eqM[0])-1)
	// the system must be square to have a full recovery
	if len(resultEqM) == len(resultEqM[0])-1 && getFirstNonZeroIndex(resultEqM[0]) == len(resultEqM[0])-2 {
		// All the variables have been found.
		// we want to handle malformed systems like this 1x2 system (the 1st coeff is 0):
		// 0 a c
		// so we replaced &iv[len(resultEqM)-1-i] by &iv[len(iv)-1-i] in quotient at line assigning res[index]
		for i, iv := range resultEqM {
			index := len(res) - 1 - i
			newVar := *new(big.Rat)
			res[index] = append(res[index], safeQuotient(&newVar, &iv[len(iv)-1], &iv[len(iv)-2-i], &zero))
			// res[index] = append(res[index], newVar.Quo(&iv[len(iv)-1], &iv[len(resultEqM)-1-i]))
			solvedEquations[i] = true
		}
	} else {
		// Some variables remained unknown.
		var unknownStart, unknownEnd int
		var unknownIndexes []int
		lastFnz := 0
		for i, iv := range resultEqM {
			fnz := getFirstNonZeroIndex(iv)
			if fnz != -1 {
				var firstRes []*big.Rat
				newVar := *new(big.Rat)
				// set the constant part of the result
				firstRes = append(firstRes, safeQuotient(&newVar, &iv[len(iv)-1], &iv[fnz], &zero))
				// firstRes = append(firstRes, newVar.Quo(&iv[len(iv)-1], &iv[fnz]))
				if i == 0 {
					unknownStart = fnz + 1
					unknownEnd = len(iv) - 2
					onlyZeroes := true
					// as it is the first row, all variables with an index > fnz are unknown
					for j := unknownEnd; j >= unknownStart; j-- {
						res[j] = []*big.Rat{nil} // nil means undetermined
						unknownIndexes = append(unknownIndexes, j)
						newVar2 := *new(big.Rat)
						firstRes = append(firstRes, safeQuotient(&newVar2, &iv[j], &iv[fnz], &zero))
						if (&newVar2).Cmp(&zero) != 0 {
							// there is a non zero coefficient in the relation
							onlyZeroes = false
						}
						// firstRes = append(firstRes, newVar2.Quo(&iv[j], &iv[fnz]))
					}
					if onlyZeroes {
						// if there are only zero coefs in the relation, then the variable is not unknown
						firstRes = []*big.Rat{firstRes[0]}
					}
				} else {
					// if we enter in this loop, we have skipped some variables: this is because the matrix is not square
					for k := lastFnz + 1; k < fnz; k++ {
						unknownIndexes = append(unknownIndexes, k)
						res[k] = []*big.Rat{nil}
					}
					onlyZeroes := true
					for _, j := range unknownIndexes {
						newVar2 := *new(big.Rat)
						firstRes = append(firstRes, safeQuotient(&newVar2, &iv[j], &iv[fnz], &zero))
						if (&newVar2).Cmp(&zero) != 0 {
							// there is a non zero coefficient in the relation
							onlyZeroes = false
						}
					}
					if onlyZeroes {
						// if there are only zero coefs in the relation, then the variable is not unknown
						firstRes = []*big.Rat{firstRes[0]}
					}
				}
				res[fnz] = firstRes
				if len(firstRes) == 1 {
					solvedEquations[i] = true
				}
				lastFnz = fnz
			}

		}
	}

	return
}

func sortMatrixBigInt(m [][]big.Rat, initRow int, descending bool) (m2 [][]big.Rat) {
	indexed := make(map[int]bool)

	for i := 0; i < initRow; i++ {
		m2 = append(m2, m[i])
		indexed[i] = true
	}

	greaterThanMax := func(rr1, rr2 []big.Rat) (greater bool) {
		for i := 0; i < len(rr1); i++ {
			newVar := *new(big.Rat)
			newVar2 := *new(big.Rat)
			cmp := newVar.Abs(&rr1[i]).Cmp(newVar2.Abs(&rr2[i]))

			if cmp > 0 {
				// greater == true if descending, false otherwise
				greater = descending
				return
			} else if cmp < 0 {
				greater = !descending
				return
			}
		}
		return
	}

	type maxStruct struct {
		index   int
		element []big.Rat
	}

	for i := initRow; i < len(m); i++ {
		max := maxStruct{-1, make([]big.Rat, len(m[i]))}
		var firstNotIndexed int = -1
		foundOne := false
		for k, kv := range m {
			if !indexed[k] {
				firstNotIndexed = k
				if !foundOne || greaterThanMax(kv, max.element) {
					max.index = k
					max.element = kv
					foundOne = true
				}
			}
		}
		if max.index != -1 {
			m2 = append(m2, max.element)
			indexed[max.index] = true
		} else {
			m2 = append(m2, m[firstNotIndexed])
			indexed[firstNotIndexed] = true
		}
	}

	return
}

func containsDuplicatesLinesBigInt(eqM [][]big.Rat) (contains bool, l1, l2 int) {
	for i := 0; i < len(eqM); i++ {
		for j := i + 1; j < len(eqM); j++ {
			var equalElements int
			for k := 0; k < len(eqM[i]); k++ {
				if eqM[i][k].Cmp(&eqM[j][k]) == 0 {
					equalElements++
				} else {
					break
				}
			}
			if equalElements == len(eqM[i]) {
				contains = true
				l1 = i
				l2 = j
				return
			}
		}
	}
	return
}

func safeQuotient(receiver *big.Rat, rat *big.Rat, rat2 *big.Rat, zero *big.Rat) *big.Rat {
	numZero := rat.Cmp(zero) == 0
	denomZero := rat2.Cmp(zero) == 0
	if numZero && denomZero {
		//receiver.SetInt64(1)
		return nil
	}
	return receiver.Quo(rat, rat2)
}

func containsOnlyZero(arr []big.Rat) bool {
	zero := &big.Rat{}
	for _, bi := range arr {
		if bi.Cmp(zero) != 0 {
			return false
		}
	}
	return true
}
