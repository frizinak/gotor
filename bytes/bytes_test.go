package bytes

import "testing"

func TestParse(t *testing.T) {
	type tcase struct {
		in  string
		out Bytes
	}

	list := []tcase{
		{in: "5", out: Bytes{5, B}},
		{in: "5B", out: Bytes{5, B}},
		{in: "2.0MiB", out: Bytes{2, MiB}},
		{in: "2.0mib", out: Bytes{2, MiB}},
		{in: "2.3M", out: Bytes{2.3, MiB}},
		{in: "2.0m", out: Bytes{2.0, MiB}},
		{in: "2.0mb", out: Bytes{2 * 1000 * 1000, B}.Convert(MiB)},
		{in: "2.0MB", out: Bytes{2 * 1000 * 1000, B}.Convert(MiB)},
		{in: "2000.5KB", out: Bytes{2000.5 * 1000, B}.Convert(KiB)},
	}

	for i, c := range list {
		out, err := Parse(c.in)
		if err != nil {
			t.Fatal(err)
		}

		if out != c.out {
			t.Errorf("'%s' != expected '%s' (case %d)", out, c.out, i+1)
		}
	}

}
