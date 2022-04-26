package fec

import (
	"fmt"
	"log"
)

// returns 1 if a has its first non-zero term before b
// returns -1 if a has its first non-zero term after b
// returns 0 otherwise
func cmpEq(a []uint8, b []uint8, idx int) int { // THIS
	if a[idx] < b[idx] {
		return -1
	} else if a[idx] > b[idx] {
		return 1
	} else if a[idx] != 0 {
		return 0
	}
	return 0
}

// pre: there is no column full of zeroes
func sortSystem(a [][]uint8, constant_terms [][]uint8) { // THIS
	n_eq := len(a)
	// simple selection sort, because there should not be that much equations
	for i := 0; i < n_eq; i++ {
		max := i
		for j := i + 1; j < n_eq; j++ {
			if cmpEq(a[max], a[j], i) < 0 {
				max = j
			}
		}
		a[i], a[max] = a[max], a[i]
		constant_terms[i], constant_terms[max] = constant_terms[max], constant_terms[i]
	}
}

func printSystem(system [][]uint8) {
	for _, eq := range system {
		str := ""
		for i, val := range eq {
			str = fmt.Sprintf("%s%d", str, val)
			if i != len(eq)-1 {
				str = fmt.Sprintf("%s, ", str)
			}
		}
		log.Printf(str)
	}
}

// Inspired and adapted from https://www.bragitoff.com/2018/02/gauss-elimination-c-program/
/*******
Function that performs Gauss-Elimination and returns the Upper triangular matrix:
There are two options to do this in C.
1. Pass a matrix (a) as the parameter, and calculate and store the upperTriangular(Gauss-Eliminated Matrix) in it.
2. Use malloc and make the function of pointer type and return the pointer.
This program uses the first option.
********/
func gaussEliminationGF256(system [][]uint8, constantTerms [][]uint8) (unknowns [][]uint8, solvedEquations []bool, err error) {
	if len(system) == 0 {
		return nil, nil, nil
	}
	unknowns = make([][]uint8, len(system[0]))
	nEq := len(system)
	nUnknowns := len(unknowns)
	solvedEquations = make([]bool, nEq)
	sortSystem(system, constantTerms)
	for i := 0; i < nEq-1; i++ {
		for k := i + 1; k < nEq; k++ {
			if k > i {
				mulnum := system[k][i]
				muldenom := system[i][i]
				// term=a[k][i]/a[i][i]
				term := gf256Div(mulnum, muldenom)
				for j := 0; j < nUnknowns; j++ {
					// a[k][j] -= a[k][i]/a[i][i]*a[i][j]
					//                    // i < m-1 AND m <= n, -> i < n-1
					system[k][j] = gf256Sub(system[k][j], gf256Mul(term, system[i][j]))
				}
				// a[k][j] -= a[k][i]/a[i][i]*a[i][j] for the big, constant term
				symbolSubScaled(constantTerms[k], term, constantTerms[i])
			}
		}
	}
	candidate := nUnknowns - 1
	//Begin Back-substitution

	for i := nEq - 1; i >= 0; i-- {
		for candidate >= 0 && system[i][candidate] == 0 {
			unknowns[candidate] = nil
			candidate--
		}
		if candidate >= 0 {
			unknowns[candidate] = make([]uint8, len(constantTerms[i]))
			copy(unknowns[candidate], constantTerms[i])
			for j := 0; j < candidate; j++ {
				if system[i][j] != 0 {
					// if this variable depends on another one with a smaller index, it is undefined, as we don't know the value of the one with a smaller index
					unknowns[candidate] = nil
					break
				}
			}
			for j := candidate + 1; j < nUnknowns; j++ {
				//             x[i]=x[i]-a[i][j]*x[j];
				if system[i][j] != 0 {
					if unknowns[j] == nil {
						// if the unknown depends on an undetermined unknown, this unknown is unknowns
						unknowns[candidate] = nil
					} else {
						symbolSubScaled(unknowns[candidate], system[i][j], unknowns[j])
						system[i][j] = 0
					}
				}
			}
			// i < n_eq <= n_unknowns, so a[i][i] is small
			if unknowns[candidate] != nil && symbolIsZero(unknowns[candidate]) || system[i][candidate] == 0 {
				// this solution is unknowns
				unknowns[candidate] = nil
			} else if unknowns[candidate] != nil {
				// x[i] = x[i]/a[i][i]
				solvedEquations[i] = true
				symbolDiv(unknowns[candidate], system[i][candidate])
				system[i][candidate] = gf256Div(system[i][candidate], system[i][candidate])
			}
		}
		candidate--
	}
	// mark all the variables with an index <= candidate as unknowns
	for i := candidate; i >= 0; i-- {
		unknowns[i] = nil
	}
	return
}
