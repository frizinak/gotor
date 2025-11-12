package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/console"
)

type flagStrs []string

func (i *flagStrs) String() string { return "" }
func (i *flagStrs) Set(value string) error {
	*i = append(*i, value)
	return nil
}

type flagRegexes []*regexp.Regexp

func (i *flagRegexes) String() string { return "" }
func (i *flagRegexes) Set(value string) error {
	r, err := regexp.Compile("(?i)" + value)
	if err != nil {
		return err
	}

	*i = append(*i, r)
	return nil
}

type prompter struct {
	output io.Writer
}

func (p prompter) Prompt(q string) string {
	fmt.Fprint(p.output, q, "")
	sc := bufio.NewScanner(os.Stdin)
	sc.Split(bufio.ScanLines)
	sc.Scan()
	return sc.Text()
}

func (p prompter) Byte() (byte, bool) {
	tstate := console.Current()
	_ = tstate.SetRaw()
	defer tstate.Reset()
	b := make([]byte, 1)
	n, _ := os.Stdin.Read(b)
	if n == 0 {
		return b[0], false
	}

	return b[0], true
}

func (p prompter) YN(q string, def bool) (yes, ok bool) {
	fmt.Fprint(p.output, q)
	y, n := "y", "N"
	if def {
		y, n = "Y", "n"
	}
	fmt.Fprintf(p.output, " [%s/%s] ", y, n)
	var a byte
	a, ok = p.Byte()
	if !ok {
		fmt.Fprintln(p.output)
		return
	}
	if a < 5 {
		ok = false
		fmt.Fprintln(p.output)
		return
	}

	yes = (def && a != 'n' && a != 'N') || (!def && (a == 'y' || a == 'Y'))
	if yes {
		fmt.Fprintln(p.output, "y")
		return
	}

	fmt.Fprintln(p.output, "n")
	return
}

func intRange(a string) ([]int, bool) {
	comma := strings.Split(a, ",")
	r := make([]int, 0, len(comma))
	for _, n := range comma {
		dash := strings.SplitN(n, "-", 2)
		v, err := strconv.Atoi(strings.TrimSpace(dash[0]))
		if err != nil {
			return r, false
		}
		if len(dash) != 2 {
			r = append(r, v)
			continue
		}

		if len(dash) == 2 {
			v2, err := strconv.Atoi(strings.TrimSpace(dash[1]))
			if err != nil {
				return r, false
			}
			if v2 < v {
				return r, false
			}
			for i := v; i <= v2; i++ {
				r = append(r, i)
			}
		}
	}

	return r, len(r) > 0
}
func ex(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func termSize() (int, int) {
	termsize, _ := console.Current().Size()
	return int(termsize.Width), int(termsize.Height)
}

func pad(str string, n int) string {
	t := len(str)
	if n > t {
		t = n
	}
	buf := make([]byte, t)
	for i := range buf {
		buf[i] = ' '
	}
	copy(buf[t/2-len(str)/2:], str)
	return string(buf)
}

func parseUserTime(input string, eod bool) (time.Time, error) {
	const (
		today     = "today"
		yesterday = "yesterday"
	)

	type f func(f, i string) (time.Time, error)
	parse := func(f, i string) (time.Time, error) {
		return time.ParseInLocation(f, i, time.Local)
	}
	noDate := func(f, i string) (time.Time, error) {
		const format = "2006-01-02 "
		prefix := time.Now().Format(format)
		return parse(format+f, prefix+i)
	}
	noYear := func(f, i string) (time.Time, error) {
		const format = "2006-"
		prefix := time.Now().Format(format)
		return parse(format+f, prefix+i)
	}
	pToday := func(f, i string) (time.Time, error) {
		if i != today {
			return time.Time{}, errors.New("could not parse date")
		}
		n := time.Now()
		dt := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location())
		return dt, nil
	}
	pYesterday := func(f, i string) (time.Time, error) {
		if i != yesterday {
			return time.Time{}, errors.New("could not parse date")
		}
		dt, err := pToday(f, today)
		return dt.Add(-time.Hour * 24), err
	}
	allowEOD := func(cb f) f {
		if !eod {
			return cb
		}
		return func(f, i string) (time.Time, error) {
			dt, err := cb(f, i)
			return dt.Add(time.Hour * 24), err
		}
	}

	parsers := map[string]f{
		"2006-01-02 15:04:05": parse,
		"2006-01-02 15:04":    parse,
		"01-02 15:04:05":      noYear,
		"01-02 15:04":         noYear,
		"2006-01-02":          allowEOD(parse),
		"15:04:05":            noDate,
		"15:04":               noDate,
		"01-02":               allowEOD(noYear),
		today:                 allowEOD(pToday),
		yesterday:             allowEOD(pYesterday),
	}

	var dt time.Time
	var err error
	for format, parser := range parsers {
		if len(format) == len(input) {
			dt, err = parser(format, input)
			if err == nil {
				return dt, err
			}
		}
	}
	if err == nil {
		err = fmt.Errorf("unrecognized date format '%s'", input)
	}

	return dt, err
}

func durationString(dur time.Duration) string {
	sec := dpart{dur / time.Second, "s"}
	min := dpart{sec.value / 60, "m"}
	hor := dpart{min.value / 60, "h"}
	day := dpart{hor.value / 24, "d"}

	sec.value -= min.value * 60
	min.value -= hor.value * 60
	hor.value -= day.value * 24

	items := []dpart{day, hor, min, sec}
	strs := make([]string, 2)
	i := 0
	for _, p := range items {
		if str := p.String(); str != "" {
			strs[i] = str
			if i++; i == 2 {
				break
			}
		}
	}

	if i == 0 {
		return "0s"
	}

	return fmt.Sprintf("%s%s", strs[0], strs[1])
}

func commonAncestor(paths []string) string {
	if len(paths) == 0 {
		return ""
	}

	common := paths[0]
	for i := 1; i < len(paths); i++ {
		for {
			if common == "" {
				return ""
			} else if strings.HasPrefix(paths[i], common) {
				break
			} else if common == "/" {
				return ""
			}
			common, _ = path.Split(common)
			if len(common) > 1 {
				common = common[:len(common)-1]
			}
		}
	}

	return common
}
