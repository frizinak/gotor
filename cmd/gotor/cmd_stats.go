package main

import (
	stdbytes "bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/frizinak/gotor/api"
)

type statsFlags struct {
	*cmdFlags
	watch float64
}

func (f statsFlags) Parse(uc userConfig, o io.Writer) statsConfig {
	var conf statsConfig
	conf.cmdConfig = f.cmdFlags.Parse(uc, o)
	conf.sleep = (time.Second * time.Duration((f.watch)*1e6)) / 1e6

	return conf
}

type statsConfig struct {
	cmdConfig
	sleep time.Duration
}

func cmdStats(ctx context.Context, conf statsConfig, c api.Client) error {
	type res struct {
		r   api.Stats
		err error
		d   time.Time
	}
	ch := make(chan res)

	var gerr error
	var lastRun time.Time
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
		var res res
		for {
			s, err := c.Stats(ctx)
			lastRun = time.Now()
			res.d = lastRun
			res.r = s
			res.err = err
			ch <- res
			if doSleep(lastRun) != nil {
				break
			}
		}
	}()

	r := <-ch

	buf := stdbytes.NewBuffer(make([]byte, 0, 1024))
	var ok bool
	for {
		buf.Reset()
		buf.WriteString("\033[40D\033[K")

		var since string
		if conf.sleep != 0 {
			_since := time.Since(r.d) - conf.sleep
			if _since < 0 {
				_since = 0
			}
			_since = _since.Round(time.Second)
			if _since != 0 {
				since = _since.String()
			}
		}

		fmt.Fprintf(
			buf,
			"%10s %10s %10s",
			r.r.UploadSpeed.Human().Format("%6.2f %-3s"),
			r.r.DownloadSpeed.Human().Format("%6.2f %-3s"),
			since,
		)

		buf.WriteTo(conf.output)
		if conf.sleep == 0 {
			break
		}

		select {
		case r, ok = <-ch:
			if !ok {
				break
			}
		case <-time.After(time.Millisecond * 100):
		}
	}
	fmt.Fprintln(conf.output)

	if r.err != nil {
		return r.err
	}

	return gerr
}
