package main

import (
	stdbytes "bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	stdpath "path"
	"strings"
	"time"

	"github.com/frizinak/gotor/api"
	"github.com/frizinak/gotor/bytes"
	rpc "github.com/frizinak/transmissionrpc"
	"github.com/mattn/go-runewidth"
)

func pv[X any](val *X) X {
	var def X
	if val == nil {
		return def
	}

	return *val
}

type detailFlags struct {
	*cmdFlags
}

func (f detailFlags) Parse(uc userConfig, o io.Writer) detailConfig {
	var c detailConfig
	c.cmdConfig = f.cmdFlags.Parse(uc, o)

	return c
}

type detailMode uint8

const (
	dmDefault detailMode = iota
	dmPath
	dmFiles
)

type detailConfig struct {
	cmdConfig
	mode detailMode
}

type dpart struct {
	value time.Duration
	unit  string
}

func (p dpart) String() string {
	if p.value == 0 {
		return ""
	}
	return fmt.Sprintf("%d%s", p.value, p.unit)
}

func cmdInfo(ctx context.Context, conf detailConfig, c api.Client, id string) error {
	o := stdbytes.NewBuffer(make([]byte, 0, 5000))
	p := newDetailPrinter(o, conf)
	err := c.Details(ctx, []string{id}, func(_t interface{}, _ api.Torrent) error {
		switch t := _t.(type) {
		case rpc.Torrent:
			i, err := c.Info(ctx)
			if err != nil {
				return err
			}
			return cmdInfoTransmission(conf.mode, p, i, t)
		default:
			return fmt.Errorf("unimplemeneted torrent type: %T", t)
		}
	})

	if err != nil {
		return err
	}

	o.WriteTo(conf.output)
	return nil
}

type detailPrinter struct {
	o     io.Writer
	color bool
	msg   struct {
		na, unknown, no, yes, bad string
	}

	indentBuf []byte
	bnl       []byte
	epoch     time.Time
}

func newDetailPrinter(output io.Writer, conf detailConfig) *detailPrinter {
	p := &detailPrinter{}
	p.o = output
	p.color = conf.color

	p.msg.na = "\x00_n/a"
	p.msg.unknown = "\x00_unknown"
	p.msg.no = "\x00_n"
	p.msg.yes = "\x00_y"
	p.msg.bad = "\x00_bad"

	p.epoch = time.Unix(0, 0)
	p.indentBuf = make([]byte, 0, 4*8)
	p.bnl = []byte{'\n'}

	return p

}
func (p *detailPrinter) bool(b bool) string {
	if b {
		return p.msg.yes
	}
	return p.msg.no
}
func (p *detailPrinter) since(dt time.Time) string {
	s := time.Since(dt)
	if s > time.Hour*24*7 || s < 0 {
		return ""
	}

	dur := p.duration(s)
	if dur == "0s" {
		return "just now"
	}
	if dur == "" {
		return ""
	}

	return fmt.Sprintf("%s ago", dur)
}
func (p *detailPrinter) date(dt time.Time) string {
	const dtFormat = "2006-01-02 15:04:05"
	if !dt.After(p.epoch) {
		return ""
	}
	s := p.since(dt)
	d := dt.Format(dtFormat)
	if s == "" {
		return d
	}
	return fmt.Sprintf("%s (%s)", d, s)
}
func (p *detailPrinter) duration(dur time.Duration) string {
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
func (p *detailPrinter) title(level int, title string) {
	fmt.Fprintf(p.o, "%s%s\n", p.indent(level-1), title)
}
func (p *detailPrinter) prints(level int, name, text string) {
	fmt.Fprintf(p.o, "%s%s | %s\n", p.indent(level), name, p.na(text))
}
func (p *detailPrinter) printv(level int, name string, text any) {
	p.prints(level, name, fmt.Sprintf("%v", text))
}
func (p *detailPrinter) printf(level int, name, format string, args ...any) {
	p.prints(level, name, fmt.Sprintf(format, args...))
}
func (p *detailPrinter) indent(level int) string {
	w := level * 4
	if cap(p.indentBuf) < w {
		p.indentBuf = make([]byte, 0, w)
	}
	b := p.indentBuf[:w]
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
func (p *detailPrinter) na(text string) string {
	var clr, pref string
	var cut bool
	switch {
	case text == "":
		clr = "\033[48:5:243m\033[38:5:250m"
		text = "n/a"
	case strings.HasPrefix(text, p.msg.na):
		clr = "\033[48:5:243m\033[38:5:250m"
		pref = p.msg.na
	case strings.HasPrefix(text, p.msg.unknown):
		clr = "\033[48:5:224m\033[38:5:235m"
		pref = p.msg.unknown
	case strings.HasPrefix(text, p.msg.no):
		clr = "\033[48:5:160m\033[38:5:255m"
		pref = p.msg.no
	case strings.HasPrefix(text, p.msg.yes):
		clr = "\033[48:5:64m\033[38:5:255m"
		pref = p.msg.yes
	case strings.HasPrefix(text, p.msg.bad):
		clr = "\033[48:5:160m\033[38:5:255m"
		pref = p.msg.bad
		cut = true
	default:
		return text
	}

	if len(pref) != 0 {
		first := pref[2:]
		text = text[len(pref):]
		if cut && p.color {
			return fmt.Sprintf("%s %s \033[0m", clr, text)
		}
		if cut {
			return text
		}
		if p.color {
			return fmt.Sprintf("%s %s \033[0m%s", clr, first, text)
		}
		return fmt.Sprintf("%s%s", first, text)
	}

	if p.color {
		return fmt.Sprintf("%s %s \033[0m", clr, text)
	}
	return text
}
func (p *detailPrinter) nl() {
	p.o.Write(p.bnl)
}
func (p *detailPrinter) bytes(b int64, prec, unit bytes.Unit) bytes.Bytes {
	if prec < unit {
		prec = unit
	}
	return bytes.New(float64(b/int64(prec/unit)), prec).Human()
}

func cmdInfoTransmission(mode detailMode, p *detailPrinter, i api.Info, t rpc.Torrent) error {
	switch mode {
	case dmDefault:
	case dmPath:
		list := make([]string, len(t.Files))
		for i := range t.Files {
			list[i] = t.Files[i].Name
		}

		p.title(1, path.Join(pv(t.DownloadDir), commonAncestor(list)))
		return nil
	case dmFiles:
		l := 0
		for _, f := range t.Files {
			_, name := stdpath.Split(f.Name)
			if rw := runewidth.StringWidth(name); rw > l {
				l = rw
			}
		}

		format := fmt.Sprintf(
			"%%0%dd | %%s",
			int(math.Ceil(math.Log10(float64(len(t.Files))))),
		)

		var cdir string
		for i, f := range t.Files {
			dir, name := stdpath.Split("/" + f.Name)
			if dir != cdir {
				cdir = dir
				p.title(1, dir)
			}
			fs := t.FileStats[i]
			n := fmt.Sprintf(format, i, runewidth.FillRight(name, l))
			if !fs.Wanted {
				p.prints(1, n, p.msg.no)
				continue
			}

			prio := "normal"
			switch fs.Priority {
			case 1:
				prio = "high"
			case -1:
				prio = "low"
			}
			bh := p.bytes(f.BytesCompleted, bytes.B, bytes.B)
			bt := p.bytes(f.Length, bytes.B, bytes.B)
			bh = bh.Convert(bt.Unit())

			var pct int64
			if f.Length != 0 {
				pct = f.BytesCompleted * 100 / f.Length
			}
			p.printf(1, n, "%s %6s %6.2f %10s (%3d%%)", p.msg.yes, prio, bh.Value, bt.String(), pct)
		}
		p.nl()
		return nil
	default:
		return errors.New("invalid mode")
	}

	var prio string
	{
		val := pv(t.BandwidthPriority)
		switch val {
		case 1:
			prio = "high"
		case 0:
			prio = "normal"
		case -1:
			prio = "low"
		default:
			prio = fmt.Sprintf("%s (%d)", p.msg.unknown, val)
		}
	}
	eta := ""
	{
		f := func(n int64) string {
			if pv(t.IsFinished) {
				return p.msg.na
			}
			switch n {
			case -1:
				return ""
			case -2:
				return p.msg.unknown
			}

			return p.duration(time.Duration(n) * time.Second)
		}
		eta = f(pv(t.ETA))
	}
	var availablePct float64
	{
		available := float64(pv(t.DesiredAvailable) + pv(t.HaveValid) + pv(t.HaveUnchecked))
		swd := float64(pv(t.SizeWhenDone))
		if swd != 0 {
			availablePct = available / swd
		}
	}
	var honorsRateLimits string = p.msg.no
	dlLimitEnabled, upLimitEnabled := p.msg.no, p.msg.no
	var dlLimit, upLimit string
	{
		h := pv(t.HonorsSessionLimits)
		if h {
			honorsRateLimits = p.msg.yes
			if i.DownloadLimit != nil {
				dlLimitEnabled = p.msg.yes
				dlLimit = i.DownloadLimit.Human().Format("%3.0f %s/s (global)")
			}
			if i.UploadLimit != nil {
				upLimitEnabled = p.msg.yes
				upLimit = i.UploadLimit.Human().Format("%3.0f %s/s (global)")
			}
		}

		if pv(t.DownloadLimited) {
			dlLimitEnabled = p.msg.yes
			dlLimit = p.bytes(pv(t.DownloadLimit), bytes.KiB, bytes.KiB).Format("%3.0f %s/s")
		}

		if pv(t.UploadLimited) {
			upLimitEnabled = p.msg.yes
			upLimit = p.bytes(pv(t.UploadLimit), bytes.KiB, bytes.KiB).Format("%3.0f %s/s")
		}
	}
	var honorsIdleLimits string = p.msg.no
	idleLimit := "unlimited"
	{
		switch pv(t.SeedIdleMode) {
		case 0:
			honorsIdleLimits = p.msg.yes
			if i.SeedingLimit != nil {
				idleLimit = p.duration(*i.SeedingLimit)
			}
		case 1:
			idleLimit = p.duration(pv(t.SeedIdleLimit))
		}
	}
	var honorsSeedRatio string = p.msg.no
	ratioLimit := "unlimited"
	{
		switch pv(t.SeedRatioMode) {
		case 0:
			honorsSeedRatio = p.msg.yes
			if i.SeedingLimit != nil {
				ratioLimit = fmt.Sprintf("%.02f", *i.SeedingRatioLimit)
			}
		case 1:
			ratioLimit = fmt.Sprintf("%.02f", pv(t.SeedRatioLimit))
		}
	}
	availableBytes := pv(t.DesiredAvailable) + pv(t.HaveValid) + pv(t.HaveUnchecked)
	status := p.msg.unknown
	{
		s := pv(t.Status)
		if s >= 0 && s <= 7 {
			status = s.String()
		}
		switch {
		case s == rpc.TorrentStatusStopped:
		case s == rpc.TorrentStatusIsolated:
			status = fmt.Sprintf("%s%s", p.msg.bad, status)
		case pv(t.IsStalled):
			status = fmt.Sprintf("%sstalled %s", p.msg.bad, status)
		}
		if s == rpc.TorrentStatusDownload || s == rpc.TorrentStatusDownloadWait {
			if m := pv(t.MetadataPercentComplete); m < 1 {
				status = fmt.Sprintf("%s metadata", status)
			}
		}
	}

	p.title(1, "Meta")
	p.printv(1, "id       ", pv(t.ID))
	p.prints(1, "name     ", pv(t.Name))
	p.prints(1, "mime     ", pv(t.PrimaryMimeType))
	p.prints(1, "labels   ", strings.Join(t.Labels, ", "))
	p.prints(1, "priority ", prio)
	p.prints(1, "hash     ", pv(t.HashString))
	p.printv(1, "queue pos", pv(t.QueuePosition))
	p.prints(1, "creator  ", pv(t.Creator))
	p.prints(1, "comment  ", pv(t.Comment))
	p.prints(1, "group    ", pv(t.Group))
	p.printv(1, "#files   ", pv(t.FileCount))
	p.prints(1, "torrent  ", pv(t.TorrentFile))
	p.prints(1, "directory", pv(t.DownloadDir))
	p.prints(1, "private  ", p.bool(pv(t.IsPrivate)))
	p.nl()

	if t.Error != nil && *t.Error != 0 {
		p.title(1, "Error")
		p.printv(1, "code", pv(t.Error))
		p.prints(1, "msg ", pv(t.ErrorString))
		p.nl()
	}

	p.title(1, "Time")
	p.prints(1, "active     ", p.date(pv(t.ActivityDate)))
	p.prints(1, "added      ", p.date(pv(t.AddedDate)))
	p.prints(1, "started    ", p.date(pv(t.StartDate)))
	p.prints(1, "done       ", p.date(pv(t.DoneDate)))
	p.prints(1, "update     ", p.date(pv(t.EditDate)))
	//prints(1, "created", date(pv(t.DateCreated)))
	p.prints(1, "downloading", p.duration(pv(t.TimeDownloading)))
	p.prints(1, "seeding    ", p.duration(pv(t.TimeSeeding)))
	p.nl()

	p.title(1, "Status")
	p.prints(1, "finished", p.bool(pv(t.IsFinished)))
	p.prints(1, "phase   ", status)
	p.nl()

	p.title(1, "Network")
	p.printv(1, "peers    ", pv(t.PeersConnected))
	p.title(2, "download")
	p.prints(2, "rate ", p.bytes(pv(t.RateDownload), bytes.B, bytes.B).Format("%6.2f %s/s"))
	p.printf(2, "peers", "%3d", pv(t.PeersSendingToUs))
	p.title(2, "upload")
	p.prints(2, "rate ", p.bytes(pv(t.RateUpload), bytes.B, bytes.B).Format("%6.2f %s/s"))
	p.printf(2, "peers", "%3d", pv(t.PeersGettingFromUs))
	p.nl()

	p.title(1, "Progress")
	p.printf(1, "downloaded", "%5.1f%%", 100*pv(t.PercentDone))
	p.printf(1, "available ", "%5.1f%%", 100*availablePct)
	p.printf(1, "recheck   ", "%5.1f%%", 100*pv(t.RecheckProgress))
	p.prints(1, "ETA       ", eta)
	p.printf(1, "seed ratio", "%.02f", pv(t.UploadRatio))
	p.nl()

	p.title(1, "Stats")
	p.prints(1, "check     ", p.bytes(pv(t.HaveUnchecked), bytes.KiB, bytes.B).String())
	p.prints(1, "have      ", p.bytes(pv(t.HaveValid), bytes.KiB, bytes.B).String())
	p.prints(1, "available ", p.bytes(availableBytes, bytes.KiB, bytes.B).String())
	p.prints(1, "want      ", p.bytes(pv(t.SizeWhenDone), bytes.KiB, bytes.B).String())
	p.prints(1, "full      ", p.bytes(pv(t.TotalSize), bytes.KiB, bytes.B).String())
	p.prints(1, "corrupt   ", p.bytes(pv(t.CorruptEver), bytes.KiB, bytes.B).String())
	p.prints(1, "downloaded", p.bytes(pv(t.DownloadedEver), bytes.KiB, bytes.B).String())
	p.prints(1, "uploaded  ", p.bytes(pv(t.UploadedEver), bytes.KiB, bytes.B).String())
	p.nl()

	p.title(1, "Limits")
	p.printv(1, "max peers         ", pv(t.PeerLimit))
	p.title(2, "rate")
	p.prints(2, "honors globals", honorsRateLimits)
	p.printf(2, "download      ", "%s %s", dlLimitEnabled, dlLimit)
	p.printf(2, "upload        ", "%s %s", upLimitEnabled, upLimit)
	p.title(2, "seed idle")
	p.prints(2, "honors globals", honorsIdleLimits)
	p.prints(2, "limit         ", idleLimit)
	p.title(2, "seed ratio")
	p.prints(2, "honors globals", honorsSeedRatio)
	p.prints(2, "limit         ", ratioLimit)
	p.nl()

	return nil
}
