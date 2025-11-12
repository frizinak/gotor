package bytes

import (
	"fmt"
	"strconv"
)

type Unit int

const (
	B   Unit = 1
	KiB      = B * 1024
	MiB      = KiB * 1024
	GiB      = MiB * 1024
	TiB      = GiB * 1024
	PiB      = TiB * 1024
)

func (u Unit) String() string {
	switch u {
	case B:
		return "B"
	case KiB:
		return "KiB"
	case MiB:
		return "MiB"
	case GiB:
		return "GiB"
	case TiB:
		return "TiB"
	case PiB:
		return "PiB"
	}

	return "?"
}

func (u Unit) Format() string {
	switch u {
	case B:
		return "%.0f"
	}

	return "%.2f"
}

var order = []Unit{B, KiB, MiB, GiB, TiB, PiB}

type Bytes struct {
	Value float64
	unit  Unit
}

func (b Bytes) Human() Bytes {
	if b.unit >= PiB {
		return b
	}

	n := b.Value * float64(b.unit)
	i := 0
	for n >= 1000 && order[i] < PiB {
		n /= 1024
		i++
	}

	return New(n, order[i])
}

func (b Bytes) Unit() Unit { return b.unit }

func (b Bytes) Convert(unit Unit) Bytes {
	b.Value = b.Value * (float64(b.unit) / float64(unit))
	b.unit = unit
	return b
}

func (b Bytes) String() string {
	format := fmt.Sprintf("%s %s", b.unit.Format(), b.unit.String())
	return fmt.Sprintf(format, b.Value)
}

func (b Bytes) StringNoUnit() string {
	return fmt.Sprintf(b.unit.Format(), b.Value)
}

func (b Bytes) Format(f string) string {
	return fmt.Sprintf(f, b.Value, b.unit.String())
}

func New(value float64, unit Unit) Bytes { return Bytes{value, unit} }

func Parse(str string) (Bytes, error) {
	b := Bytes{0, B}

	ustr := make([]byte, 0, 3)
	for i := len(str) - 1; i >= 0; i-- {
		if str[i] >= '0' && str[i] <= '9' || str[i] == '.' {
			break
		}

		ustr = append(ustr, str[i])
	}

	nstr := str[:len(str)-len(ustr)]

	v, err := strconv.ParseFloat(nstr, 64)
	if err != nil {
		return b, err
	}
	b.Value = v

	if len(ustr) == 0 {
		return b, nil
	}

	order := 0
	switch ustr[len(ustr)-1] {
	case 'k', 'K':
		order = 1
		b.unit = KiB
	case 'm', 'M':
		order = 2
		b.unit = MiB
	case 'g', 'G':
		order = 3
		b.unit = GiB
	case 't', 'T':
		order = 4
		b.unit = TiB
	case 'p', 'P':
		order = 5
		b.unit = PiB
	}

	if len(ustr) == 1 || ustr[len(ustr)-2] == 'i' {
		return b, nil
	}

	for i := 0; i < order; i++ {
		b.Value *= 1000
		b.Value /= 1024
	}

	return b, nil
}
