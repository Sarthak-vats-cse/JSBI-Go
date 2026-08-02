package jsbi

import (
	"testing"
)

func mustBig(t *testing.T, s string) *JSBI {
	t.Helper()
	b, err := FromString(s)
	if err != nil {
		t.Fatalf("FromString(%q) error: %v", s, err)
	}
	return b
}

func TestBasicArithmetic(t *testing.T) {
	a := mustBig(t, "123456789012345678901234567890")
	b := mustBig(t, "987654321098765432109876543210")
	sum := Add(a, b)
	if sum.String() != "1111111110111111111011111111100" {
		t.Fatalf("Add: got %s", sum.String())
	}
	prod := Multiply(a, b)
	want := "121932631137021795226185032733622923332237463801111263526900"
	if prod.String() != want {
		t.Fatalf("Multiply: got %s want %s", prod.String(), want)
	}
}

func TestDivisionByZero(t *testing.T) {
	a := FromInt64(10)
	z := Zero()
	if _, err := Divide(a, z); err == nil {
		t.Fatal("expected RangeError on division by zero")
	} else if _, ok := err.(*RangeError); !ok {
		t.Fatalf("expected *RangeError, got %T", err)
	}
	if _, err := Remainder(a, z); err == nil {
		t.Fatal("expected RangeError on remainder by zero")
	}
}

func TestTruncatingDivision(t *testing.T) {
	// JS: -7n / 2n === -3n ; -7n % 2n === -1n
	x := FromInt64(-7)
	y := FromInt64(2)
	q, _ := Divide(x, y)
	if q.String() != "-3" {
		t.Fatalf("Divide(-7,2): got %s want -3", q.String())
	}
	r, _ := Remainder(x, y)
	if r.String() != "-1" {
		t.Fatalf("Remainder(-7,2): got %s want -1", r.String())
	}
}

func TestShifts(t *testing.T) {
	x := FromInt64(-5)
	// JS: -5n >> 1n === -3n  (floor division by 2, matches Rsh)
	y := FromInt64(1)
	r, err := SignedRightShift(x, y)
	if err != nil || r.String() != "-3" {
		t.Fatalf("SignedRightShift(-5,1): got %s err %v want -3", r, err)
	}
	l, err := LeftShift(FromInt64(3), FromInt64(4))
	if err != nil || l.String() != "48" {
		t.Fatalf("LeftShift(3,4): got %s err %v want 48", l, err)
	}
	// negative shift amount flips direction
	l2, err := LeftShift(x, FromInt64(-1))
	if err != nil || l2.String() != "-3" {
		t.Fatalf("LeftShift(-5,-1): got %s err %v want -3", l2, err)
	}
}

func TestBitwiseTwoComplement(t *testing.T) {
	// JS: (-1n) & 5n === 5n ; (-1n) | 0n === -1n ; 5n ^ -1n === -6n
	negOne := FromInt64(-1)
	five := FromInt64(5)
	if r := BitwiseAnd(negOne, five); r.String() != "5" {
		t.Fatalf("-1 & 5: got %s want 5", r)
	}
	if r := BitwiseOr(negOne, Zero()); r.String() != "-1" {
		t.Fatalf("-1 | 0: got %s want -1", r)
	}
	if r := BitwiseXor(five, negOne); r.String() != "-6" {
		t.Fatalf("5 ^ -1: got %s want -6", r)
	}
	if r := BitwiseNot(Zero()); r.String() != "-1" {
		t.Fatalf("~0: got %s want -1", r)
	}
}

func TestAsIntNAsUintN(t *testing.T) {
	// JS: BigInt.asIntN(8, 255n) === -1n ; BigInt.asUintN(8, -1n) === 255n
	v255 := FromInt64(255)
	r, err := AsIntN(8, v255)
	if err != nil || r.String() != "-1" {
		t.Fatalf("asIntN(8,255): got %s err %v want -1", r, err)
	}
	negOne := FromInt64(-1)
	u, err := AsUintN(8, negOne)
	if err != nil || u.String() != "255" {
		t.Fatalf("asUintN(8,-1): got %s err %v want 255", u, err)
	}
}

func TestExponentiate(t *testing.T) {
	r, err := Exponentiate(FromInt64(2), FromInt64(10))
	if err != nil || r.String() != "1024" {
		t.Fatalf("2**10: got %s err %v want 1024", r, err)
	}
	_, err = Exponentiate(FromInt64(2), FromInt64(-1))
	if err == nil {
		t.Fatal("expected RangeError for negative exponent")
	}
}

func TestToStringRadix(t *testing.T) {
	x := FromInt64(255)
	s, err := x.ToString(16)
	if err != nil || s != "ff" {
		t.Fatalf("255 in hex: got %q err %v want ff", s, err)
	}
	neg, _ := FromString("-255")
	s2, _ := neg.ToString(16)
	if s2 != "-ff" {
		t.Fatalf("-255 in hex: got %q want -ff", s2)
	}
	if _, err := x.ToString(1); err == nil {
		t.Fatal("expected RangeError for radix 1")
	}
	if _, err := x.ToString(37); err == nil {
		t.Fatal("expected RangeError for radix 37")
	}
}

func TestFromStringParsing(t *testing.T) {
	cases := map[string]string{
		"":          "0",
		"   ":       "0",
		"0x1F":      "31",
		"0o17":      "15",
		"0b101":     "5",
		"  42  ":    "42",
		"+42":       "42",
		"-42":       "-42",
		"000123":    "123",
	}
	for in, want := range cases {
		b, err := FromString(in)
		if err != nil {
			t.Fatalf("FromString(%q) unexpected error: %v", in, err)
		}
		if b.String() != want {
			t.Fatalf("FromString(%q): got %s want %s", in, b.String(), want)
		}
	}
	invalid := []string{"abc", "0x", "1.5", "12a", "-0x1"}
	for _, in := range invalid {
		if _, err := FromString(in); err == nil {
			t.Fatalf("FromString(%q): expected SyntaxErr, got none", in)
		}
	}
}

func TestFromFloat64(t *testing.T) {
	if _, err := FromFloat64(1.5); err == nil {
		t.Fatal("expected RangeError for non-integer float")
	}
	b, err := FromFloat64(1e20)
	if err != nil {
		t.Fatalf("FromFloat64(1e20) error: %v", err)
	}
	if b.String() != "100000000000000000000" {
		t.Fatalf("FromFloat64(1e20): got %s", b.String())
	}
}

func TestToNumber(t *testing.T) {
	b := FromInt64(1024)
	if ToNumber(b) != 1024 {
		t.Fatalf("ToNumber(1024): got %v", ToNumber(b))
	}
	huge, _ := FromString("100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")
	got := ToNumber(huge)
	if got <= 0 {
		t.Fatalf("ToNumber(huge): expected large positive/Inf value, got %v", got)
	}
}

func TestDataView(t *testing.T) {
	buf := make([]byte, 8)
	v := FromInt64(1234567890123)
	if err := DataViewSetBigUint64(buf, 0, v, false); err != nil {
		t.Fatalf("SetBigUint64 error: %v", err)
	}
	got, err := DataViewGetBigUint64(buf, 0, false)
	if err != nil || got.String() != "1234567890123" {
		t.Fatalf("GetBigUint64 round-trip: got %v err %v", got, err)
	}
	neg := FromInt64(-5)
	if err := DataViewSetBigInt64(buf, 0, neg, true); err != nil {
		t.Fatalf("SetBigInt64 error: %v", err)
	}
	gotNeg, err := DataViewGetBigInt64(buf, 0, true)
	if err != nil || gotNeg.String() != "-5" {
		t.Fatalf("GetBigInt64 round-trip: got %v err %v", gotNeg, err)
	}
}

func TestMixedOperators(t *testing.T) {
	a := FromInt64(5)
	if !LT(a, 10.0) {
		t.Fatal("expected 5n < 10")
	}
	if !EQ(a, "5") {
		t.Fatal("expected 5n == '5'")
	}
	sum, err := ADD(a, a)
	if err != nil {
		t.Fatalf("ADD error: %v", err)
	}
	if s, ok := sum.(*JSBI); !ok || s.String() != "10" {
		t.Fatalf("ADD(5n,5n): got %v", sum)
	}
	if _, err := ADD(a, 5.0); err == nil {
		t.Fatal("expected TypeError mixing BigInt and number")
	}
	concat, err := ADD(a, "x")
	if err != nil || concat != "5x" {
		t.Fatalf("ADD(5n,'x'): got %v err %v", concat, err)
	}
}
