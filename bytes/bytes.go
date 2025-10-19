package bytes

import "fmt"

type Unit int

const (
	B   Unit = 1
	KiB      = B * 1024
	MiB      = KiB * 1024
	GiB      = MiB * 1024
	TiB      = GiB * 1024
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

var order = []Unit{B, KiB, MiB, GiB, TiB}

type Bytes struct {
	Value float64
	unit  Unit
}

func (b Bytes) Human() Bytes {
	if b.unit >= TiB {
		return b
	}

	n := b.Value * float64(b.unit)
	i := 0
	for n >= 1000 && order[i] < TiB {
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
