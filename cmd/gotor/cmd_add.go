package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/frizinak/gotor/api"
	"github.com/jackpal/bencode-go"
)

type addFlags struct {
	cmdFlags
	downloadDirFlags
}

func (f addFlags) Parse(uc userConfig, o io.Writer) addConfig {
	var c addConfig
	c.cmdConfig = f.cmdFlags.Parse(uc, o)
	c.downloadDirConfig = f.downloadDirFlags.Parse(uc, o)
	return c
}

type addConfig struct {
	cmdConfig
	downloadDirConfig

	downloadPath string
	labels       []string
}

var b64buf = bytes.NewBuffer(nil)

func parseAdd(str string) (string, error) {
	if strings.HasPrefix(str, "magnet:") {
		return str, nil
	}

	b64buf.Reset()
	b64 := base64.NewEncoder(base64.StdEncoding, b64buf)

	f, err := os.Open(str)
	if err != nil {
		return "", err
	}

	r := io.TeeReader(f, b64)
	var l interface{}
	err = bencode.Unmarshal(r, &l)
	f.Close()
	b64.Close()
	if err != nil {
		return "", err
	}

	return b64buf.String(), nil
}

func cmdAdd(ctx context.Context, conf addConfig, c api.Client, items []string) error {
	d, err := addDir(ctx, conf, c)
	if err != nil {
		return err
	}

	for _, item := range items {
		t, err := add(ctx, conf, c, d, item)
		if err != nil {
			return err
		}

		fmt.Fprintf(conf.output, "Success: %6s %s\n", t.ID, t.Name)
	}

	return nil
}

func addDir(ctx context.Context, conf addConfig, c api.Client) (string, error) {
	base := conf.downloadDirectory
	if base == "" {
		if conf.downloadPath == "/" {
			return "", nil
		}

		info, err := c.Info(ctx)
		if err != nil {
			return "", err
		}
		base = info.DefaultPath
	}

	return strings.TrimRight(path.Join(base, conf.downloadPath), "/\\"), nil
}

func add(ctx context.Context, conf addConfig, c api.Client, dir, item string) (api.Torrent, error) {
	clean, err := parseAdd(item)
	if err != nil {
		const m = 40
		if len(item) > m+3 {
			item = item[:m/2] + "..." + item[len(item)-m/2:]
		}

		return api.Torrent{}, fmt.Errorf("invalid torrent/magnet: '%s'", item)
	}

	var torrent api.Torrent
	err = c.Add(ctx, conf.labels, dir, []string{clean}, func(t api.Torrent) error {
		torrent = t
		return nil
	})

	return torrent, err
}
