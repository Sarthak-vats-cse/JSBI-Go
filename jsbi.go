// Package jsbi is a Go port of the JSBI TypeScript/JavaScript arbitrary
// precision integer library (https://github.com/GoogleChromeLabs/jsbi),
// originally Copyright 2018 Google Inc., licensed under the Apache License,
// Version 2.0 (http://www.apache.org/licenses/LICENSE-2.0).
//
// JSBI exists to polyfill JavaScript's native BigInt using arrays of 30-bit
// "digits" plus a sign bit, hand-implementing addition, multiplication,
// Knuth Algorithm D division, two's-complement bitwise operators, and
// IEEE-754 conversion, purely because JavaScript has no native arbitrary
// precision integer type to build on.
//
// Go does have one: math/big.Int, in the standard library. Its Add/Sub/Mul,
// Quo/Rem (truncating division, matching JS BigInt's `/` and `%`),
// Lsh/Rsh (matching JS BigInt's `<<`/`>>`, including arithmetic-shift
// semantics for negative operands), And/Or/Xor/Not (implemented as
// infinite-precision two's complement, matching the ECMAScript BigInt
// bitwise spec exactly), and Mod (Euclidean, non-negative result, exactly
// what asUintN/asIntN need) already provide the exact semantics that JSBI's
// digit-array code exists to emulate. So this port keeps JSBI's public API
// surface and edge-case error behavior, and implements it on top of
// math/big instead of re-deriving 30-bit-limb arithmetic by hand.
package jsbi

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------
// Errors. JSBI throws RangeError / SyntaxError / TypeError for various
// conditions; we mirror that with distinct error types so callers can
// type-switch/errors.As the same way calling code would catch by class.
// ---------------------------------------------------------------------

// RangeError mirrors JavaScript's RangeError, as thrown by the original
// JSBI implementation (e.g. "Division by zero", invalid radix, exponent
// too large, invalid asIntN/asUintN width, etc.)
type RangeError struct{ Msg string }

func (e *RangeError) Error() string { return e.Msg }

// SyntaxErr mirrors JavaScript's SyntaxError, as thrown by JSBI.BigInt()
// when a string cannot be parsed as a BigInt.
type SyntaxErr struct{ Msg string }

func (e *SyntaxErr) Error() string { return e.Msg }

// TypeErr mirrors JavaScript's TypeError, as thrown for invalid conversions
// and for JSBI.unsignedRightShift (BigInts have no >>> operator).
type TypeErr struct{ Msg string }

func (e *TypeErr) Error() string { return e.Msg }

func newRangeError(msg string) error  { return &RangeError{Msg: msg} }
func newSyntaxError(msg string) error { return &SyntaxErr{Msg: msg} }
func newTypeError(msg string) error   { return &TypeErr{Msg: msg} }

// ---------------------------------------------------------------------
// The JSBI type itself.
// ---------------------------------------------------------------------

// JSBI is an arbitrary precision signed integer, equivalent to a
// JavaScript BigInt / an instance of the original JSBI class.
type JSBI struct {
	v *big.Int
}

func fromBig(b *big.Int) *JSBI { return &JSBI{v: b} }

// Zero returns the JSBI value 0, equivalent to JSBI.__zero().
func Zero() *JSBI { return &JSBI{v: big.NewInt(0)} }

// Big returns the underlying *big.Int. The returned value must not be
// mutated in place; treat JSBI values as immutable, exactly as the
// original JSBI type is documented to be.
func (x *JSBI) Big() *big.Int { return new(big.Int).Set(x.v) }

// Sign returns -1, 0, or +1, matching x.v.Sign(). Provided as a convenience;
// there is no direct JSBI equivalent (JS code inspects x.sign / x.length).
func (x *JSBI) Sign() int { return x.v.Sign() }

// IsZero reports whether x is zero (equivalent to checking x.length === 0
// in the original implementation).
func (x *JSBI) IsZero() bool { return x.v.Sign() == 0 }

// ---------------------------------------------------------------------
// JSBI.BigInt(arg) — construction from number/string/boolean/object.
// ---------------------------------------------------------------------

// FromInt64 constructs a JSBI from an int64. Equivalent to
// JSBI.BigInt(arg) for an already-integral number.
func FromInt64(n int64) *JSBI { return &JSBI{v: big.NewInt(n)} }

// FromUint64 constructs a JSBI from a uint64.
func FromUint64(n uint64) *JSBI { return &JSBI{v: new(big.Int).SetUint64(n)} }

// FromFloat64 mirrors JSBI.BigInt(number) for a float64 argument: it
// throws (returns) a RangeError if the value is not a finite integer,
// exactly like the original:
//
//	if (!Number.isFinite(arg) || Math.floor(arg) !== arg) {
//	  throw new RangeError(...);
//	}
func FromFloat64(arg float64) (*JSBI, error) {
	if math.IsInf(arg, 0) || math.IsNaN(arg) || math.Floor(arg) != arg {
		return nil, newRangeError(fmt.Sprintf(
			"The number %v cannot be converted to a BigInt because it is not an integer", arg))
	}
	bf := new(big.Float).SetFloat64(arg)
	bi, _ := bf.Int(nil)
	return &JSBI{v: bi}, nil
}

// FromBool mirrors JSBI.BigInt(boolean): true -> 1n, false -> 0n.
func FromBool(b bool) *JSBI {
	if b {
		return FromInt64(1)
	}
	return Zero()
}

// isJSWhitespace mirrors JSBI's __isWhitespace, which follows the
// ECMAScript definition of StrWhiteSpaceChar / LineTerminator.
func isJSWhitespace(r rune) bool {
	switch r {
	case 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x20, 0xA0, 0x1680, 0xFEFF,
		0x2028, 0x2029, 0x202F, 0x205F, 0x3000:
		return true
	}
	if r >= 0x2000 && r <= 0x200A {
		return true
	}
	return false
}

// parseBigIntString mirrors JSBI.__fromString(string, radix). It returns
// (value, true) on success, or (nil, false) if the string cannot be parsed
// (equivalent to __fromString returning null).
func parseBigIntString(s string, radix int) (*big.Int, bool) {
	runes := []rune(s)
	n := len(runes)
	i := 0

	if n == 0 {
		return big.NewInt(0), true
	}

	for i < n && isJSWhitespace(runes[i]) {
		i++
	}
	if i == n {
		return big.NewInt(0), true
	}

	sign := 1
	switch runes[i] {
	case '+':
		i++
		sign = 1
		if i == n {
			return nil, false
		}
	case '-':
		i++
		sign = -1
		if i == n {
			return nil, false
		}
	}

	base := radix
	if base == 0 {
		base = 10
		if runes[i] == '0' && i+1 < n {
			switch runes[i+1] {
			case 'x', 'X':
				base = 16
				i += 2
			case 'o', 'O':
				base = 8
				i += 2
			case 'b', 'B':
				base = 2
				i += 2
			}
		}
	} else if base == 16 {
		if runes[i] == '0' && i+1 < n && (runes[i+1] == 'x' || runes[i+1] == 'X') {
			i += 2
		}
	}
	if i == n {
		// A prefix like "0x" with nothing following is invalid.
		return nil, false
	}
	if sign != 1 && base != 10 {
		// Only decimal literals may carry an explicit sign (mirrors
		// "if (sign !== 0 && radix !== 10) return null;" — note sign
		// defaults to 0/"no sign seen" in the original; here sign==1
		// covers both "no sign" and "explicit +", which is fine since
		// '+' is likewise only valid for radix 10 in the original).
		if !(sign == 1 && runes[0] != '+') {
			return nil, false
		}
	}

	digitsStart := i
	for i < n {
		c := runes[i]
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'z':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'Z':
			d = int(c-'A') + 10
		default:
			d = -1
		}
		if d < 0 || d >= base {
			break
		}
		i++
	}
	if i == digitsStart {
		return nil, false
	}
	digitStr := string(runes[digitsStart:i])

	for i < n && isJSWhitespace(runes[i]) {
		i++
	}
	if i != n {
		return nil, false
	}

	bi, ok := new(big.Int).SetString(digitStr, base)
	if !ok {
		return nil, false
	}
	if sign == -1 {
		bi.Neg(bi)
	}
	return bi, true
}

// FromString mirrors JSBI.BigInt(string) via JSBI.__fromString(arg),
// throwing (returning) a SyntaxError on failure:
//
//	const result = JSBI.__fromString(arg);
//	if (result === null) {
//	  throw new SyntaxError('Cannot convert ' + arg + ' to a BigInt');
//	}
func FromString(s string) (*JSBI, error) {
	bi, ok := parseBigIntString(s, 0)
	if !ok {
		return nil, newSyntaxError(fmt.Sprintf("Cannot convert %s to a BigInt", s))
	}
	return &JSBI{v: bi}, nil
}

// BigInt mirrors the JSBI.BigInt(arg) static factory, dispatching on the
// dynamic type of arg the way the original dispatches on JS's typeof.
// Supported types: *JSBI/JSBI (returned as-is), bool, string, and any of
// Go's built-in integer/float types (treated as "number").
func BigInt(arg interface{}) (*JSBI, error) {
	switch v := arg.(type) {
	case *JSBI:
		return v, nil
	case JSBI:
		return &v, nil
	case bool:
		return FromBool(v), nil
	case string:
		return FromString(v)
	case int:
		return FromInt64(int64(v)), nil
	case int8:
		return FromInt64(int64(v)), nil
	case int16:
		return FromInt64(int64(v)), nil
	case int32:
		return FromInt64(int64(v)), nil
	case int64:
		return FromInt64(v), nil
	case uint:
		return FromUint64(uint64(v)), nil
	case uint8:
		return FromUint64(uint64(v)), nil
	case uint16:
		return FromUint64(uint64(v)), nil
	case uint32:
		return FromUint64(uint64(v)), nil
	case uint64:
		return FromUint64(v), nil
	case float32:
		return FromFloat64(float64(v))
	case float64:
		return FromFloat64(v)
	default:
		return nil, newTypeError(fmt.Sprintf("Cannot convert %v to a BigInt", arg))
	}
}

// ---------------------------------------------------------------------
// toString / toDebugString / toNumber
// ---------------------------------------------------------------------

// ToString mirrors JSBI.prototype.toString(radix). radix defaults to 10
// when 0 is passed. Throws (returns) RangeError for radix outside [2, 36]:
//
//	if (radix < 2 || radix > 36) {
//	  throw new RangeError('toString() radix argument must be between 2 and 36');
//	}
func (x *JSBI) ToString(radix int) (string, error) {
	if radix == 0 {
		radix = 10
	}
	if radix < 2 || radix > 36 {
		return "", newRangeError("toString() radix argument must be between 2 and 36")
	}
	if x.v.Sign() == 0 {
		return "0", nil
	}
	return x.v.Text(radix), nil
}

// String implements fmt.Stringer using base 10, equivalent to calling
// toString() with no arguments.
func (x *JSBI) String() string {
	s, _ := x.ToString(10)
	return s
}

// ToDebugString mirrors JSBI.prototype.toDebugString(), which prints each
// internal 30-bit digit in hex. Since this Go port has no digit array,
// this instead prints hex chunks of the absolute value's big-endian byte
// representation, prefixed the same way, for debugging purposes.
func (x *JSBI) ToDebugString() string {
	var b strings.Builder
	b.WriteString("BigInt[")
	abs := new(big.Int).Abs(x.v)
	if abs.Sign() == 0 {
		b.WriteString("0, ")
	} else {
		hex := abs.Text(16)
		b.WriteString(hex)
		b.WriteString(", ")
	}
	b.WriteString("]")
	return b.String()
}

// ToNumber mirrors JSBI.toNumber(x), converting to the nearest float64
// with round-to-nearest-even, saturating to ±Inf on overflow — exactly
// the rounding behavior the original hand-implements via mantissa bit
// manipulation. big.Float's SetInt + Float64 already implement correctly
// rounded (round-to-nearest, ties-to-even) conversion with the same
// overflow-to-Inf behavior.
func ToNumber(x *JSBI) float64 {
	f := new(big.Float).SetInt(x.v)
	r, _ := f.Float64()
	return r
}

// ---------------------------------------------------------------------
// Arithmetic operations.
// ---------------------------------------------------------------------

// UnaryMinus mirrors JSBI.unaryMinus(x).
func UnaryMinus(x *JSBI) *JSBI {
	if x.v.Sign() == 0 {
		return x
	}
	return &JSBI{v: new(big.Int).Neg(x.v)}
}

// BitwiseNot mirrors JSBI.bitwiseNot(x): ~x == -x-1.
func BitwiseNot(x *JSBI) *JSBI {
	return &JSBI{v: new(big.Int).Not(x.v)}
}

// Exponentiate mirrors JSBI.exponentiate(x, y). Throws (returns)
// RangeError if y is negative:
//
//	if (y.sign) { throw new RangeError('Exponent must be positive'); }
func Exponentiate(x, y *JSBI) (*JSBI, error) {
	if y.v.Sign() < 0 {
		return nil, newRangeError("Exponent must be positive")
	}
	if y.v.Sign() == 0 {
		return FromInt64(1), nil
	}
	if x.v.Sign() == 0 {
		return x, nil
	}
	return &JSBI{v: new(big.Int).Exp(x.v, y.v, nil)}, nil
}

// Multiply mirrors JSBI.multiply(x, y).
func Multiply(x, y *JSBI) *JSBI {
	if x.v.Sign() == 0 {
		return x
	}
	if y.v.Sign() == 0 {
		return y
	}
	return &JSBI{v: new(big.Int).Mul(x.v, y.v)}
}

// Divide mirrors JSBI.divide(x, y): truncating division toward zero
// (matching JS BigInt's `/`, i.e. T-division). Throws (returns)
// RangeError on division by zero:
//
//	if (y.length === 0) throw new RangeError('Division by zero');
func Divide(x, y *JSBI) (*JSBI, error) {
	if y.v.Sign() == 0 {
		return nil, newRangeError("Division by zero")
	}
	return &JSBI{v: new(big.Int).Quo(x.v, y.v)}, nil
}

// Remainder mirrors JSBI.remainder(x, y): truncating remainder (matching
// JS BigInt's `%`, i.e. T-division remainder, sign follows the dividend).
// Throws (returns) RangeError on division by zero.
func Remainder(x, y *JSBI) (*JSBI, error) {
	if y.v.Sign() == 0 {
		return nil, newRangeError("Division by zero")
	}
	return &JSBI{v: new(big.Int).Rem(x.v, y.v)}, nil
}

// Add mirrors JSBI.add(x, y).
func Add(x, y *JSBI) *JSBI { return &JSBI{v: new(big.Int).Add(x.v, y.v)} }

// Subtract mirrors JSBI.subtract(x, y).
func Subtract(x, y *JSBI) *JSBI { return &JSBI{v: new(big.Int).Sub(x.v, y.v)} }

// ---------------------------------------------------------------------
// Shifts.
// ---------------------------------------------------------------------

// maxShiftBits mirrors JSBI's practical shift-amount ceiling
// (__kMaxLengthBits), beyond which shifting is defined as "too big" /
// "shift by the maximum" rather than actually materializing the result.
const maxShiftBits = 1 << 30

func shiftLeft(x *JSBI, amount *big.Int) (*JSBI, error) {
	if amount.Sign() == 0 || x.v.Sign() == 0 {
		return x, nil
	}
	if !amount.IsUint64() || amount.Uint64() > maxShiftBits {
		return nil, newRangeError("BigInt too big")
	}
	return &JSBI{v: new(big.Int).Lsh(x.v, uint(amount.Uint64()))}, nil
}

func shiftRight(x *JSBI, amount *big.Int) (*JSBI, error) {
	if amount.Sign() == 0 || x.v.Sign() == 0 {
		return x, nil
	}
	if !amount.IsUint64() || amount.Uint64() > maxShiftBits {
		// __rightShiftByMaximum: shifting by "infinity" collapses to
		// -1 (all bits set) for negative x, 0 for non-negative x.
		if x.v.Sign() < 0 {
			return FromInt64(-1), nil
		}
		return Zero(), nil
	}
	return &JSBI{v: new(big.Int).Rsh(x.v, uint(amount.Uint64()))}, nil
}

// LeftShift mirrors JSBI.leftShift(x, y):
//
//	if (y.length === 0 || x.length === 0) return x;
//	if (y.sign) return JSBI.__rightShiftByAbsolute(x, y);
//	return JSBI.__leftShiftByAbsolute(x, y);
func LeftShift(x, y *JSBI) (*JSBI, error) {
	if y.v.Sign() == 0 || x.v.Sign() == 0 {
		return x, nil
	}
	if y.v.Sign() < 0 {
		return shiftRight(x, new(big.Int).Neg(y.v))
	}
	return shiftLeft(x, y.v)
}

// SignedRightShift mirrors JSBI.signedRightShift(x, y):
//
//	if (y.length === 0 || x.length === 0) return x;
//	if (y.sign) return JSBI.__leftShiftByAbsolute(x, y);
//	return JSBI.__rightShiftByAbsolute(x, y);
func SignedRightShift(x, y *JSBI) (*JSBI, error) {
	if y.v.Sign() == 0 || x.v.Sign() == 0 {
		return x, nil
	}
	if y.v.Sign() < 0 {
		return shiftLeft(x, new(big.Int).Neg(y.v))
	}
	return shiftRight(x, y.v)
}

// UnsignedRightShift mirrors JSBI.unsignedRightShift(), which always
// throws:
//
//	throw new TypeError('BigInts have no unsigned right shift; use >> instead');
func UnsignedRightShift(*JSBI, *JSBI) (*JSBI, error) {
	return nil, newTypeError("BigInts have no unsigned right shift; use >> instead")
}

// ---------------------------------------------------------------------
// Comparisons (BigInt-to-BigInt).
// ---------------------------------------------------------------------

// LessThan mirrors JSBI.lessThan(x, y).
func LessThan(x, y *JSBI) bool { return x.v.Cmp(y.v) < 0 }

// LessThanOrEqual mirrors JSBI.lessThanOrEqual(x, y).
func LessThanOrEqual(x, y *JSBI) bool { return x.v.Cmp(y.v) <= 0 }

// GreaterThan mirrors JSBI.greaterThan(x, y).
func GreaterThan(x, y *JSBI) bool { return x.v.Cmp(y.v) > 0 }

// GreaterThanOrEqual mirrors JSBI.greaterThanOrEqual(x, y).
func GreaterThanOrEqual(x, y *JSBI) bool { return x.v.Cmp(y.v) >= 0 }

// Equal mirrors JSBI.equal(x, y).
func Equal(x, y *JSBI) bool { return x.v.Cmp(y.v) == 0 }

// NotEqual mirrors JSBI.notEqual(x, y).
func NotEqual(x, y *JSBI) bool { return x.v.Cmp(y.v) != 0 }

// ---------------------------------------------------------------------
// Bitwise operations (two's complement, infinite precision — matching
// the ECMAScript BigInt bitwise operator semantics exactly).
// ---------------------------------------------------------------------

// BitwiseAnd mirrors JSBI.bitwiseAnd(x, y).
func BitwiseAnd(x, y *JSBI) *JSBI { return &JSBI{v: new(big.Int).And(x.v, y.v)} }

// BitwiseOr mirrors JSBI.bitwiseOr(x, y).
func BitwiseOr(x, y *JSBI) *JSBI { return &JSBI{v: new(big.Int).Or(x.v, y.v)} }

// BitwiseXor mirrors JSBI.bitwiseXor(x, y).
func BitwiseXor(x, y *JSBI) *JSBI { return &JSBI{v: new(big.Int).Xor(x.v, y.v)} }

// ---------------------------------------------------------------------
// asIntN / asUintN.
// ---------------------------------------------------------------------

// AsUintN mirrors JSBI.asUintN(n, x): truncate x to an n-bit unsigned
// two's-complement representation. Throws (returns) RangeError for n < 0.
func AsUintN(n int, x *JSBI) (*JSBI, error) {
	if n < 0 {
		return nil, newRangeError("Invalid value: not (convertible to) a safe integer")
	}
	if n == 0 || x.v.Sign() == 0 {
		return Zero(), nil
	}
	mod := new(big.Int).Lsh(big.NewInt(1), uint(n))
	// big.Int.Mod is Euclidean: the result is always in [0, mod), which is
	// exactly the n-bit unsigned truncation semantics required here.
	result := new(big.Int).Mod(x.v, mod)
	return &JSBI{v: result}, nil
}

// AsIntN mirrors JSBI.asIntN(n, x): truncate x to an n-bit signed
// two's-complement representation. Throws (returns) RangeError for n < 0.
func AsIntN(n int, x *JSBI) (*JSBI, error) {
	if n < 0 {
		return nil, newRangeError("Invalid value: not (convertible to) a safe integer")
	}
	if n == 0 || x.v.Sign() == 0 {
		return Zero(), nil
	}
	mod := new(big.Int).Lsh(big.NewInt(1), uint(n))
	half := new(big.Int).Lsh(big.NewInt(1), uint(n-1))
	r := new(big.Int).Mod(x.v, mod)
	if r.Cmp(half) >= 0 {
		r.Sub(r, mod)
	}
	return &JSBI{v: r}, nil
}

// ---------------------------------------------------------------------
// DataView-related functionality (DataViewGetBigInt64/GetBigUint64/
// SetBigInt64/SetBigUint64). JS DataViews operate on a buffer + byte
// offset; the natural Go equivalent is a []byte + offset, read/written
// via encoding/binary.
// ---------------------------------------------------------------------

func checkDataViewBounds(data []byte, byteOffset int) error {
	if byteOffset < 0 || byteOffset+8 > len(data) {
		return newRangeError("Offset is outside the bounds of the DataView")
	}
	return nil
}

// DataViewGetBigUint64 mirrors JSBI.DataViewGetBigUint64(dataview, byteOffset, littleEndian).
func DataViewGetBigUint64(data []byte, byteOffset int, littleEndian bool) (*JSBI, error) {
	if err := checkDataViewBounds(data, byteOffset); err != nil {
		return nil, err
	}
	var u uint64
	if littleEndian {
		u = binary.LittleEndian.Uint64(data[byteOffset : byteOffset+8])
	} else {
		u = binary.BigEndian.Uint64(data[byteOffset : byteOffset+8])
	}
	return FromUint64(u), nil
}

// DataViewGetBigInt64 mirrors JSBI.DataViewGetBigInt64(dataview, byteOffset, littleEndian):
//
//	return JSBI.asIntN(64, JSBI.DataViewGetBigUint64(dataview, byteOffset, littleEndian));
func DataViewGetBigInt64(data []byte, byteOffset int, littleEndian bool) (*JSBI, error) {
	u, err := DataViewGetBigUint64(data, byteOffset, littleEndian)
	if err != nil {
		return nil, err
	}
	return AsIntN(64, u)
}

// DataViewSetBigUint64 mirrors JSBI.DataViewSetBigUint64(dataview, byteOffset, value, littleEndian).
func DataViewSetBigUint64(data []byte, byteOffset int, value *JSBI, littleEndian bool) error {
	if err := checkDataViewBounds(data, byteOffset); err != nil {
		return err
	}
	v, err := AsUintN(64, value)
	if err != nil {
		return err
	}
	u := v.v.Uint64()
	if littleEndian {
		binary.LittleEndian.PutUint64(data[byteOffset:byteOffset+8], u)
	} else {
		binary.BigEndian.PutUint64(data[byteOffset:byteOffset+8], u)
	}
	return nil
}

// DataViewSetBigInt64 mirrors JSBI.DataViewSetBigInt64(dataview, byteOffset, value, littleEndian),
// which just delegates to DataViewSetBigUint64 in the original.
func DataViewSetBigInt64(data []byte, byteOffset int, value *JSBI, littleEndian bool) error {
	return DataViewSetBigUint64(data, byteOffset, value, littleEndian)
}

// ---------------------------------------------------------------------
// Mixed-type operators (ADD/LT/LE/GT/GE/EQ/NE).
//
// The original JSBI.ADD/LT/LE/GT/GE/EQ/NE implement JS's dynamic operand
// coercion (__toPrimitive/__toNumeric) across BigInt, number, string,
// boolean, symbol, and arbitrary objects. Go has no equivalent implicit
// coercion machinery, so this section provides a best-effort, explicitly
// typed analogue operating over a closed set of operand kinds
// (*JSBI, and Go's built-in numeric/string/bool types) via the Value
// alias, sufficient for interop code migrated from mixed-type JS call
// sites. Pure BigInt-to-BigInt code should prefer the typed functions
// above (Add, LessThan, Equal, ...) instead.
// ---------------------------------------------------------------------

// Value is any operand accepted by the mixed-type operator functions
// below: *JSBI, or a Go bool/string/integer/float type.
type Value = interface{}

func toFloat64(v Value) (float64, error) {
	switch t := v.(type) {
	case float32:
		return float64(t), nil
	case float64:
		return t, nil
	case int:
		return float64(t), nil
	case int8:
		return float64(t), nil
	case int16:
		return float64(t), nil
	case int32:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case uint:
		return float64(t), nil
	case uint8:
		return float64(t), nil
	case uint16:
		return float64(t), nil
	case uint32:
		return float64(t), nil
	case uint64:
		return float64(t), nil
	case bool:
		if t {
			return 1, nil
		}
		return 0, nil
	case string:
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			return 0, nil
		}
		f, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return math.NaN(), nil
		}
		return f, nil
	default:
		return 0, newTypeError(fmt.Sprintf("cannot convert %v to a number", v))
	}
}

// compareBigToFloat compares a *JSBI against a float64 exactly (not via
// lossy float64 conversion of the bigint), mirroring
// JSBI.__compareToNumber/__compareToDouble. Returns -1, 0, 1, or 2 as a
// sentinel for "incomparable" (NaN involved), matching JS's `undefined`
// comparison result collapsing to false.
func compareBigToFloat(x *JSBI, y float64) int {
	if math.IsNaN(y) {
		return 2
	}
	if math.IsInf(y, 1) {
		return -1
	}
	if math.IsInf(y, -1) {
		return 1
	}
	bf := new(big.Float).SetInt(x.v)
	yf := big.NewFloat(y)
	return bf.Cmp(yf)
}

// compareValues mirrors the comparison portion of JSBI.__compare, after
// operand coercion: it returns -1/0/1, or 2 for "incomparable" (NaN).
func compareValues(x, y Value) (int, error) {
	xb, xIsBig := x.(*JSBI)
	yb, yIsBig := y.(*JSBI)

	if xs, ok := x.(string); ok {
		if ys, ok := y.(string); ok {
			switch {
			case xs < ys:
				return -1, nil
			case xs > ys:
				return 1, nil
			default:
				return 0, nil
			}
		}
	}
	if xIsBig {
		if ys, ok := y.(string); ok {
			yb2, ok := parseBigIntString(ys, 0)
			if !ok {
				return 2, nil
			}
			return xb.v.Cmp(yb2), nil
		}
	}
	if ys, ok := x.(string); ok {
		if yIsBig {
			xb2, ok := parseBigIntString(ys, 0)
			if !ok {
				return 2, nil
			}
			return xb2.Cmp(yb.v), nil
		}
	}

	if xIsBig && yIsBig {
		return xb.v.Cmp(yb.v), nil
	}
	if xIsBig {
		yf, err := toFloat64(y)
		if err != nil {
			return 0, err
		}
		return compareBigToFloat(xb, yf), nil
	}
	if yIsBig {
		xf, err := toFloat64(x)
		if err != nil {
			return 0, err
		}
		c := compareBigToFloat(yb, xf)
		if c == 2 {
			return 2, nil
		}
		return -c, nil
	}
	xf, err := toFloat64(x)
	if err != nil {
		return 0, err
	}
	yf, err := toFloat64(y)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(xf) || math.IsNaN(yf) {
		return 2, nil
	}
	switch {
	case xf < yf:
		return -1, nil
	case xf > yf:
		return 1, nil
	default:
		return 0, nil
	}
}

// LT mirrors JSBI.LT(x, y).
func LT(x, y Value) bool { c, err := compareValues(x, y); return err == nil && c != 2 && c < 0 }

// LE mirrors JSBI.LE(x, y).
func LE(x, y Value) bool { c, err := compareValues(x, y); return err == nil && c != 2 && c <= 0 }

// GT mirrors JSBI.GT(x, y).
func GT(x, y Value) bool { c, err := compareValues(x, y); return err == nil && c != 2 && c > 0 }

// GE mirrors JSBI.GE(x, y).
func GE(x, y Value) bool { c, err := compareValues(x, y); return err == nil && c != 2 && c >= 0 }

func equalToNumber(x *JSBI, y float64) bool {
	if math.IsNaN(y) || math.IsInf(y, 0) {
		return false
	}
	if math.Floor(y) != y {
		return false
	}
	yb, err := FromFloat64(y)
	if err != nil {
		return false
	}
	return Equal(x, yb)
}

// EQ mirrors JSBI.EQ(x, y).
func EQ(x, y Value) bool {
	xb, xIsBig := x.(*JSBI)
	yb, yIsBig := y.(*JSBI)
	if xIsBig && yIsBig {
		return Equal(xb, yb)
	}
	if xIsBig || yIsBig {
		var big_ *JSBI
		var other Value
		if xIsBig {
			big_, other = xb, y
		} else {
			big_, other = yb, x
		}
		switch o := other.(type) {
		case string:
			ob, ok := parseBigIntString(o, 0)
			if !ok {
				return false
			}
			return big_.v.Cmp(ob) == 0
		case bool:
			if o {
				return equalToNumber(big_, 1)
			}
			return equalToNumber(big_, 0)
		default:
			f, err := toFloat64(other)
			if err != nil {
				return false
			}
			return equalToNumber(big_, f)
		}
	}
	if xs, ok := x.(string); ok {
		if ys, ok := y.(string); ok {
			return xs == ys
		}
	}
	xf, err1 := toFloat64(x)
	yf, err2 := toFloat64(y)
	if err1 != nil || err2 != nil {
		return false
	}
	return xf == yf
}

// NE mirrors JSBI.NE(x, y).
func NE(x, y Value) bool { return !EQ(x, y) }

// ADD mirrors JSBI.ADD(x, y): string concatenation if either operand is a
// string, BigInt addition if both are *JSBI, float addition otherwise,
// and a TypeError (returned as error) if BigInt is mixed with a plain
// number:
//
//	throw new TypeError('Cannot mix BigInt and other types, use explicit conversions');
func ADD(x, y Value) (Value, error) {
	xs, xIsStr := x.(string)
	ys, yIsStr := y.(string)
	if xIsStr || yIsStr {
		left := xs
		if !xIsStr {
			left = fmt.Sprint(x)
		}
		right := ys
		if !yIsStr {
			right = fmt.Sprint(y)
		}
		return left + right, nil
	}
	xb, xIsBig := x.(*JSBI)
	yb, yIsBig := y.(*JSBI)
	if xIsBig && yIsBig {
		return Add(xb, yb), nil
	}
	if xIsBig != yIsBig {
		return nil, newTypeError("Cannot mix BigInt and other types, use explicit conversions")
	}
	xf, err := toFloat64(x)
	if err != nil {
		return nil, err
	}
	yf, err := toFloat64(y)
	if err != nil {
		return nil, err
	}
	return xf + yf, nil
}
