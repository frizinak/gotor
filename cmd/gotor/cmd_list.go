package main

import (
	stdbytes "bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/frizinak/gotor/api"
	"github.com/frizinak/gotor/bytes"
	"github.com/mattn/go-runewidth"
)

const pathRoot = "/"

var statusFilterMap = map[string]api.Status{
	"stall": api.StatusStalled,
	"stop":  api.StatusStopped,
	"queue": api.StatusDownloadQueue,
	"down":  api.StatusDownloading,
	"seed":  api.StatusSeeding,
	"done":  api.StatusFinished,
	"check": api.StatusChecking,
	"100%":  api.StatusDownloaded,
	"have":  api.StatusDownloaded,
	"meta":  api.StatusMeta,
	"error": api.StatusError,
}

type color uint8

const (
	clrRow color = iota
	clrGood
	clrDone
	clrBad
	clrTitle
	clrNone
	clrAmount
)

type c func(c color, zebra bool) string

type printer struct {
	writer    io.Writer
	color     c
	width     int
	showError bool
}

func newPrinter(width int, showColor, showError bool) *printer {
	var p printer

	p.width = width
	colors := [clrAmount][2]string{
		clrRow:   {"\033[48:5:234m\033[38:5:253m", "\033[48:5:235m\033[38:5:253m"},
		clrGood:  {"\033[48:5:77m\033[38:5:233m"},
		clrDone:  {"\033[48:5:253m\033[38:5:234m", "\033[48:5:254m\033[38:5:235m"},
		clrBad:   {"\033[48:5:217m\033[38:5:233m"},
		clrTitle: {"\033[1m\033[48:5:25m\033[38:5:253m"},
		clrNone:  {"\033[0m"},
	}

	p.color = func(c color, zebra bool) string {
		clr := colors[c]
		if !zebra || clr[1] == "" {
			return clr[0]
		}
		return clr[1]
	}

	if !showColor {
		p.color = func(color, bool) string {
			return ""
		}
	}

	p.showError = showError
	return &p

}

func (p *printer) print(zebra bool, t api.Torrent) {
	var size, dn, up string
	if t.Total.Value != 0 {
		total := t.Total.Human()
		size = fmt.Sprintf("%6.2f %10s",
			t.Have.Convert(total.Unit()).Value,
			total.Format("%6.2f %3s"),
		)
	}

	upPeers, dnPeers := t.UploadPeers, t.DownloadPeers
	if upPeers > maxPeers {
		upPeers = maxPeers
	}
	if dnPeers > maxPeers {
		dnPeers = maxPeers
	}

	if t.DownloadSpeed.Value != 0 || dnPeers != 0 {
		dn = t.DownloadSpeed.Human().Format("%6.2f %-3s")
	}
	if t.UploadSpeed.Value != 0 || upPeers != 0 {
		up = t.UploadSpeed.Human().Format("%6.2f %-3s")
	}

	name := t.Name
	if p.width > 0 {
		const s = 1
		const b = 1
		cw := 6 + s + 3 + s + b + 17 + b + s + s + 5 + s + 11 + s + b + 2 + s + 2 + b + s + b + 10 + s + 10 + b
		rem := p.width - 1 - cw
		if rem < 2 {
			name = ""
		}
		name = runewidth.FillRight(runewidth.Truncate(name, rem, "…"), p.width-cw)
	}

	sclr := ""
	pclr := ""
	rclr := p.color(clrRow, zebra)
	status, ev := t.Status.Evaluate()
	switch ev {
	case api.Good:
		sclr = p.color(clrGood, zebra)
	case api.Bad:
		sclr = p.color(clrBad, zebra)
	}
	if t.Status.And(api.StatusDownloaded) {
		pclr = p.color(clrDone, zebra)
	}

	status = pad(status, 11)
	done := int(t.Done * 100)

	fmt.Fprintf(
		p.writer,
		"%s%6s %s%3d%s [%17s] %s %5.2f %s%s%s [%-2d %2d] [%10s %10s]%s\n",
		rclr,
		t.ID,
		pclr,
		done,
		rclr,
		size,
		name,
		t.Ratio,
		sclr,
		status,
		rclr,
		upPeers, dnPeers,
		up, dn,
		p.color(clrNone, zebra),
	)

	if p.showError && t.Error != "" {
		e := strings.Split(runewidth.Wrap(t.Error, 80-8), "\n")
		for _, l := range e {
			fmt.Fprintf(
				p.writer,
				"%s%6s %s %s\n",
				p.color(clrBad, zebra),
				"",
				runewidth.FillRight(l, p.width-8),
				p.color(clrNone, zebra),
			)
		}
	}
}

const maxPeers = 99

type filterFlags struct {
	id      string
	name    string
	path    string
	status  flagStrs
	added   [2]string
	updated [2]string
}

func (f filterFlags) Parse(uc userConfig, o io.Writer) (filterConfig, error) {
	var conf filterConfig
	if f, ok := intRange(f.id); ok {
		conf.id = make(map[string]struct{}, len(f))
		for _, v := range f {
			conf.id[strconv.Itoa(v)] = struct{}{}
		}
	}

	if f.name != "" {
		var err error
		conf.name, err = regexp.Compile("(?i)" + f.name)
		if err != nil {
			return conf, err
		}
	}

	dflags := []string{
		f.added[0],
		f.added[1],
		f.updated[0],
		f.updated[1],
	}

	dvalues := []**time.Time{
		&conf.added[0],
		&conf.added[1],
		&conf.updated[0],
		&conf.updated[1],
	}

	for i, f := range dflags {
		if f == "" {
			continue
		}

		dt, err := parseUserTime(f, i%2 != 0)
		if err != nil {
			return conf, err
		}
		*dvalues[i] = &dt
	}

	conf.status = make([][]string, 0, len(f.status))
	for _, t := range f.status {
		_ors := strings.Split(t, ",")
		ors := make([]string, 0, len(_ors))
		for _, ot := range _ors {
			ot = strings.TrimSpace(ot)
			if ot == "" {
				continue
			}
			ors = append(ors, ot)
		}

		if len(ors) != 0 {
			conf.status = append(conf.status, ors)
		}
	}

	conf.path = make([]string, 0, len(f.path))
	for _, p := range strings.Split(f.path, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			conf.path = append(conf.path, p)
		}
	}

	return conf, nil
}

type listFlags struct {
	cmdFlags
	downloadDirFlags
	filters    filterFlags
	watch      float64
	hideErrors bool
}

func (f listFlags) Parse(uc userConfig, o io.Writer) (listConfig, error) {
	var conf listConfig
	var err error
	conf.cmdConfig = f.cmdFlags.Parse(uc, o)
	conf.downloadDirConfig = f.downloadDirFlags.Parse(uc, o)
	conf.filters, err = f.filters.Parse(uc, o)
	if err != nil {
		return conf, err
	}

	conf.sleep = (time.Second * time.Duration((f.watch)*1e6)) / 1e6

	conf.print = newPrinter(80, !f.noColor, !f.hideErrors)
	return conf, nil
}

func relativeTorrentPath(base, p string) string {
	p = strings.TrimRight(p, "/\\")
	if base == "" {
		return p
	}
	if p == base {
		return pathRoot
	}
	if len(p) <= len(base) {
		return p
	}
	if p[:len(base)] == base && p[len(base)] == '/' {
		return p[len(base)+1:]
	}

	return p
}

type filterConfig struct {
	id      map[string]struct{}
	name    *regexp.Regexp
	status  [][]string
	path    []string
	added   [2]*time.Time
	updated [2]*time.Time
}

func (f filterConfig) Match(t api.Torrent, base string) bool {
	path := relativeTorrentPath(base, t.Path)
	if f.id != nil {
		if _, ok := f.id[t.ID]; !ok {
			return false
		}
	}
	if len(f.path) != 0 {
		m := false
		for _, pf := range f.path {
			if pf == path || pf == t.Path ||
				(pf == pathRoot && t.Path == base) {
				m = true
				break
			}
		}
		if !m {
			return false
		}
	}
	if f.name != nil {
		if !f.name.MatchString(t.Name) {
			return false
		}
	}
	if f.added[0] != nil && t.Added.Before(*f.added[0]) {
		return false
	}
	if f.added[1] != nil && !t.Added.Before(*f.added[1]) {
		return false
	}
	if f.updated[0] != nil && t.Updated.Before(*f.updated[0]) {
		return false
	}
	if f.updated[1] != nil && !t.Updated.Before(*f.updated[1]) {
		return false
	}
	if len(f.status) != 0 {
		for _, ors := range f.status {
			m := false
			for _, or := range ors {
				if or[0] == '!' || or[0] == '^' {
					if (^t.Status).Or(statusFilterMap[or[1:]]) {
						m = true
						break
					}
					continue
				}
				if t.Status.And(statusFilterMap[or]) {
					m = true
					break
				}
			}
			if !m {
				return false
			}
		}
	}

	return true
}

type listConfig struct {
	cmdConfig
	downloadDirConfig
	print   *printer
	filters filterConfig
	sleep   time.Duration
	errors  bool
}

func cmdList(ctx context.Context, conf listConfig, c api.Client) error {
	wch := make(chan int)
	{
		go func() {
			for {
				w, _ := termSize()
				if w < 20 {
					w = 80
				}
				wch <- w
				time.Sleep(time.Millisecond * 500)
			}
		}()
	}
	conf.print.width = <-wch

	type l struct {
		d   time.Time
		err error
		l   []api.Torrent
	}
	tch := make(chan l, 0)

	var gerr error
	doSleep := func(lastRun time.Time) error {
		if gerr != nil {
			return gerr
		}
		if conf.sleep == 0 {
			return errors.New("")
		}
		for time.Since(lastRun) < conf.sleep {
			if gerr = ctx.Err(); gerr != nil {
				return gerr
			}
			time.Sleep(conf.sleep / 10)
		}
		gerr = ctx.Err()
		return gerr
	}

	go func() {
		var l l
		buf := make([]api.Torrent, 0)
		for {
			buf = buf[:0]
			l.err = c.List(ctx, func(t api.Torrent) error {
				buf = append(buf, t)
				return nil
			})
			l.l = make([]api.Torrent, len(buf))
			copy(l.l, buf)

			lastRun := time.Now()
			if l.err != nil {
				tch <- l
				if conf.sleep == 0 {
					break
				}
				if doSleep(lastRun) != nil {
					break
				}
				continue
			}

			l.d = lastRun
			slices.SortStableFunc(l.l, func(a, b api.Torrent) int {
				if n := cmp.Compare(a.Path, b.Path); n != 0 {
					return n
				}
				if n := a.Added.Compare(b.Added); n != 0 {
					return n
				}
				return cmp.Compare(a.ID, b.ID)
			})
			tch <- l
			if conf.sleep == 0 {
				break
			}

			if doSleep(lastRun) != nil {
				break
			}
		}
		close(tch)
	}()
	list := <-tch
	items := list.l

	buf := stdbytes.NewBuffer(make([]byte, 0, 1024*1024))
	conf.print.writer = buf

main:
	for {
		buf.Reset()
		if conf.sleep != 0 {
			buf.WriteString("\033[2J\033[H")
		}

		have, total := bytes.New(0, bytes.MiB), bytes.New(0, bytes.MiB)
		dn, up := bytes.New(0, bytes.KiB), bytes.New(0, bytes.KiB)
		dnPeers, upPeers := 0, 0
		var zebra bool
		lastPath := ""

		for _, t := range items {
			if !conf.filters.Match(t, conf.downloadDirectory) {
				continue
			}

			zebra = !zebra
			if t.Path != lastPath {
				fmt.Fprintf(
					conf.print.writer,
					"%s %s %s",
					conf.print.color(clrTitle, zebra),
					pad("", conf.print.width-2),
					conf.print.color(clrNone, zebra),
				)

				zebra = false
				lastPath = t.Path

				relpath := relativeTorrentPath(conf.downloadDirectory, t.Path)
				fmt.Fprintf(
					conf.print.writer,
					"%s%6s %s %s",
					conf.print.color(clrTitle, zebra),
					"",
					runewidth.FillRight(relpath, conf.print.width-8),
					conf.print.color(clrNone, zebra),
				)
				fmt.Fprintf(
					conf.print.writer,
					"%s %s %s",
					conf.print.color(clrTitle, zebra),
					pad("", conf.print.width-2),
					conf.print.color(clrNone, zebra),
				)
			}
			conf.print.print(zebra, t)
			have.Value += t.Have.Convert(bytes.MiB).Value
			total.Value += t.Total.Convert(bytes.MiB).Value
			dn.Value += t.DownloadSpeed.Convert(bytes.KiB).Value
			up.Value += t.UploadSpeed.Convert(bytes.KiB).Value
			dnPeers += t.UploadPeers
			upPeers += t.UploadPeers
		}

		if dnPeers > maxPeers {
			dnPeers = maxPeers
		}
		if upPeers > maxPeers {
			upPeers = maxPeers
		}

		total = total.Human()
		var since string
		if conf.sleep != 0 {
			_since := time.Since(list.d) - conf.sleep
			if _since < 0 {
				_since = 0
			}
			_since = _since.Round(time.Second)
			if _since != 0 {
				since = _since.String()
			}
		}
		const self = 0
		fmt.Fprintf(
			conf.print.writer,
			"%s%-10s [%6.2f %10s] %s %s [%-2d %2d] [%10s %10s]%s\n",
			conf.print.color(clrRow, !zebra),
			"Total:",
			have.Convert(total.Unit()).Value,
			total.Format("%6.2f %3s"),
			since,
			pad("", conf.print.width-10-2-6-1-10-1-1-len(since)-1-self-1-1-2-1-2-1-1-1-10-1-10-1),
			upPeers, dnPeers,
			up.Convert(bytes.MiB).Format("%6.2f %3s"),
			dn.Convert(bytes.MiB).Format("%6.2f %3s"),
			conf.print.color(clrNone, !zebra),
		)

		if list.err != nil && conf.sleep != 0 {
			fmt.Fprintln(conf.print.writer, list.err)
		}
		buf.WriteTo(conf.output)
		if conf.sleep == 0 {
			break
		}

		select {
		case conf.print.width = <-wch:
		case l, ok := <-tch:
			if !ok {
				break main
			}
			list = l
			if list.err == nil {
				items = list.l
			}
		case <-time.After(time.Millisecond * 100):
		}
	}

	if list.err != nil {
		return list.err
	}
	return gerr
}
