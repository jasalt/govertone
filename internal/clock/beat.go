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
	if d < 0 {
		if n == math.MinInt64 || d == math.MinInt64 {
			return Beat{}, errors.New("beat overflow")
		}
		n, d = -n, -d
	}
	g := gcd(abs(n), d)
	return Beat{n / g, d / g}, nil
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
