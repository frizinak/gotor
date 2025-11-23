package main

import (
	"context"
	"io"

	"github.com/frizinak/gotor/api"
)

type moveFlags struct {
	simpleFlags
	baseDirFlags
}

func (f moveFlags) Parse(uc userConfig, o io.Writer) (moveConfig, error) {
	var conf moveConfig
	var err error
	conf.simpleConfig, err = f.simpleFlags.Parse(uc, o)
	conf.baseDirConfig = f.baseDirFlags.Parse(uc, o)
	return conf, err
}

type moveConfig struct {
	simpleConfig
	baseDirConfig
	dir string
}

func cmdMove(ctx context.Context, conf moveConfig, c api.Client) error {
	dir, err := abs(ctx, conf.baseDir, conf.dir, c)
	if err != nil {
		return err
	}

	return cmdSimple(ctx, conf.simpleConfig, c, func(ctx context.Context, ids []string) error {
		return c.Move(ctx, ids, dir)
	})
}
