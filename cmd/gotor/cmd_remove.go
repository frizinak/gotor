package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/frizinak/gotor/api"
)

type removeFlags struct {
	cmdFlags
	filterFlags
	downloadDirFlags
	deleteData bool
	yes        bool
}

func (f removeFlags) Parse(uc userConfig, o io.Writer) (removeConfig, error) {
	var conf removeConfig
	var err error
	conf.cmdConfig = f.cmdFlags.Parse(uc, o)
	conf.downloadDirConfig = f.downloadDirFlags.Parse(uc, o)
	conf.filters, err = f.filterFlags.Parse(uc, o)
	conf.print = newPrinter(80, !f.noColor, false)
	conf.deleteData = f.deleteData
	conf.yes = f.yes
	return conf, err
}

type removeConfig struct {
	cmdConfig
	filters filterConfig
	downloadDirConfig
	prompter   prompter
	print      *printer
	deleteData bool
	yes        bool
}

func cmdRemove(ctx context.Context, conf removeConfig, c api.Client) error {

	ids := make([]string, 0, 10)
	err := c.List(ctx, func(t api.Torrent) error {
		yes, ok := cmdRemoveTorrentCheck(conf, t)
		if !ok {
			return errors.New("aborted")
		}
		if yes {
			ids = append(ids, t.ID)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(ids) == 0 {
		fmt.Fprintln(conf.output, "no torrents were harmed.")
		return nil
	}

	if err = c.Remove(ctx, ids, conf.deleteData); err != nil {
		return err
	}

	if len(ids) == 1 {
		fmt.Fprintln(conf.output, "1 torrent was removed.")
		return nil
	}

	fmt.Fprintf(conf.output, "%d torrents were removed.\n", len(ids))
	return nil
}

func cmdRemoveTorrentCheck(conf removeConfig, t api.Torrent) (yes, ok bool) {
	ok = true
	if !conf.filters.Match(t, conf.downloadDirectory) {
		return
	}

	conf.print.width, _ = termSize()
	conf.print.writer = conf.output
	conf.print.print(false, t)
	if conf.yes {
		yes = true
		return
	}
	yes, ok = conf.prompter.YN("Remove?", false)
	return
}
