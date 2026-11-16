package clock

import (
	"errors"
	"fmt"
	"math"
	"math/big"
)

// Beat is an exact, normalized musical beat position.
type Beat struct {
	Numerator   int64
	Denominator int64
}

var Zero = MustBeat(0, 1)

func NewBeat(n, d int64) (Beat, error) {
	if d == 0 {
		return Beat{}, errors.New("beat denominator cannot be zero")
	}
	if n == math.MinInt64 || d == math.MinInt64 {
		return Beat{}, errors.New("beat overflow")
	}
	if d < 0 {
		n, d = -n, -d
	}
	g := gcd(abs(n), d)
	return Beat{n / g, d / g}, nil
}

// MaxFloatBeatDenominator bounds the denominator chosen for a float beat.
// It keeps sums with other beats (transport positions use 1/1_000_000)
// inside int64, and its resolution (1e-6 beat) is far finer than one frame.
const MaxFloatBeatDenominator = 1_000_000

// BeatFromFloat returns the beat closest to f with a denominator of at most
// MaxFloatBeatDenominator. Decimal and simple fractional values convert to
// the intended ratio: 0.1 becomes 1/10 and 0.3333333333333333 becomes 1/3.
// Converting the float exactly would give a denominator up to 2^1074 (0.1 is
// 3602879701896397/36028797018963968), which overflows any later sum.
func BeatFromFloat(f float64) (Beat, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return Beat{}, fmt.Errorf("beat must be finite, got %v", f)
	}
	if math.Abs(f) >= 1<<53 {
		return Beat{}, errors.New("beat overflow")
	}
	exact := new(big.Rat).SetFloat64(f)
	r := limitDenominator(exact, MaxFloatBeatDenominator)
	return NewBeat(r.Num().Int64(), r.Denom().Int64())
}

// limitDenominator is the closest rational to x with denominator <= max,
// found from the continued-fraction convergents and semiconvergents.
func limitDenominator(x *big.Rat, max int64) *big.Rat {
	if x.Denom().Cmp(big.NewInt(max)) <= 0 {
		return new(big.Rat).Set(x)
	}
	limit := big.NewInt(max)
	p0, q0, p1, q1 := big.NewInt(0), big.NewInt(1), big.NewInt(1), big.NewInt(0)
	n, d := new(big.Int).Set(x.Num()), new(big.Int).Set(x.Denom())
	for d.Sign() != 0 {
		a, rem := new(big.Int).DivMod(n, d, new(big.Int))
		q2 := new(big.Int).Add(q0, new(big.Int).Mul(a, q1))
		if q2.Cmp(limit) > 0 {
			break
		}
		p2 := new(big.Int).Add(p0, new(big.Int).Mul(a, p1))
		p0, q0, p1, q1 = p1, q1, p2, q2
		n, d = d, rem
	}
	if d.Sign() == 0 {
		return new(big.Rat).SetFrac(p1, q1)
	}
	k := new(big.Int).Div(new(big.Int).Sub(limit, q0), q1)
	semi := new(big.Rat).SetFrac(new(big.Int).Add(p0, new(big.Int).Mul(k, p1)), new(big.Int).Add(q0, new(big.Int).Mul(k, q1)))
	conv := new(big.Rat).SetFrac(p1, q1)
	if new(big.Rat).Abs(new(big.Rat).Sub(conv, x)).Cmp(new(big.Rat).Abs(new(big.Rat).Sub(semi, x))) <= 0 {
		return conv
	}
	return semi
}

func MustBeat(n, d int64) Beat {
	b, err := NewBeat(n, d)
	if err != nil {
		panic(err)
	}
	return b
}
func (b Beat) String() string {
	if b.Denominator == 1 {
		return fmt.Sprint(b.Numerator)
	}
	return fmt.Sprintf("%d/%d", b.Numerator, b.Denominator)
}
func (b Beat) Float64() float64 { return float64(b.Numerator) / float64(b.Denominator) }
func (b Beat) Sign() int {
	if b.Numerator < 0 {
		return -1
	}
	if b.Numerator > 0 {
		return 1
	}
	return 0
}
func (b Beat) Cmp(c Beat) int {
	x := new(big.Int).Mul(big.NewInt(b.Numerator), big.NewInt(c.Denominator))
	y := new(big.Int).Mul(big.NewInt(c.Numerator), big.NewInt(b.Denominator))
	return x.Cmp(y)
}
func (b Beat) Add(c Beat) (Beat, error) {
	r := new(big.Rat).Add(new(big.Rat).SetFrac64(b.Numerator, b.Denominator), new(big.Rat).SetFrac64(c.Numerator, c.Denominator))
	if !r.Num().IsInt64() || !r.Denom().IsInt64() {
		return Beat{}, errors.New("beat overflow")
	}
	return NewBeat(r.Num().Int64(), r.Denom().Int64())
}
func (b Beat) Sub(c Beat) (Beat, error) { return b.Add(MustBeat(-c.Numerator, c.Denominator)) }
func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
func gcd(a, b int64) int64 {
	if a == 0 {
		return 1
	}
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
