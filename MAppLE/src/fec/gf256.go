package fec

// copied and adapted from https://github.com/irtf-nwcrg/swif-codec

/*---------------------------------------------------------------------------*/

func gf256Add(a uint8, b uint8) uint8 {
	return a ^ b
}

var gf256Sub = gf256Add

func gf256Mul(a uint8, b uint8) uint8 {
	return gf256_mul_table[a][b]
}
func gf256Div(a uint8, b uint8) uint8 {
	return gf256_mul_table[a][gf256_inv_table[b]]
}

/**
 * @brief Take a symbol and add another symbol multiplied by a
 *        coefficient, e.g. performs the equivalent of: p1 += coef * p2
 * @param[in,out] p1     First symbol (to which coef*p2 will be added)
 * @param[in]     coef  Coefficient by which the second packet is multiplied
 * @param[in]     p2     Second symbol
 */
func symbolAddScaled(symbol1 []uint8, coef uint8, symbol2 []uint8) {
	length := len(symbol1)
	if len(symbol2) < length {
		length = len(symbol2)
	}
	for i := 0; i < length; i++ {
		symbol1[i] ^= gf256Mul(coef, symbol2[i])
	}
}

var symbolSubScaled = symbolAddScaled

func symbolMul(symbol []uint8, coef uint8) {
	for i := range symbol {
		symbol[i] = gf256Mul(symbol[i], coef)
	}
}

func symbolDiv(symbol []uint8, coef uint8) {
	for i := range symbol {
		symbol[i] = gf256Div(symbol[i], coef)
	}
}

func symbolCmp(symbol1 []uint8, symbol2 []uint8) int {
	if len(symbol1) > len(symbol2) {
		return 1
	} else if len(symbol1) < len(symbol2) {
		return -1
	}
	for i := range symbol1 {
		if symbol1[i] != 0 && symbol2[i] == 0 {
			return 1
		} else if symbol1[i] == 0 && symbol2[i] != 0 {
			return -1
		}
	}
	return 0
}

func symbolIsZero(symbol []uint8) bool {
	for _, elem := range symbol {
		if elem != 0 {
			return false
		}
	}
	return true
}
