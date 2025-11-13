package main

import (
	"context"
	"io"

	"github.com/frizinak/gotor/api"
)

type torrentFlags struct {
	*cmdFlags
	*filterFlags
}

func (f torrentFlags) Parse(uc userConfig, o io.Writer) (torrentConfig, error) {
	var conf torrentConfig
	var err error
	conf.cmdConfig = f.cmdFlags.Parse(uc, o)
	conf.filters, err = f.filterFlags.Parse(uc, o)
	conf.print = newPrinter(80, !f.noColor, false)
	return conf, err
}

type torrentConfig struct {
	cmdConfig
	filters filterConfig
	print   *printer
}

func cmdTorrent(ctx context.Context, conf torrentConfig, c api.Client, op func(context.Context, []string) error) error {
	var zebra bool
	conf.print.width, _ = termSize()
	conf.print.writer = conf.output
	ids := make([]string, 0, 10)
	err := c.List(ctx, func(t api.Torrent) error {
		if conf.filters.Match(t) {
			ids = append(ids, t.ID)
			zebra = !zebra
			conf.print.print(zebra, t)
		}
		return nil
	})

	if err != nil || len(ids) == 0 {
		return err
	}

	return op(ctx, ids)
}
