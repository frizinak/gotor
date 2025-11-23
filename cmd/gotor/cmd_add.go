package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/frizinak/gotor/api"
	"github.com/jackpal/bencode-go"
)

type addFlags struct {
	*cmdFlags
	baseDirFlags
}

func (f addFlags) Parse(uc userConfig, o io.Writer) addConfig {
	var c addConfig
	c.cmdConfig = f.cmdFlags.Parse(uc, o)
	c.baseDirConfig = f.baseDirFlags.Parse(uc, o)
	return c
}

type addConfig struct {
	cmdConfig
	baseDirConfig

	dir    string
	labels []string
}

var b64buf = bytes.NewBuffer(nil)

func parseAdd(ctx context.Context, str string) (string, error) {
	if strings.HasPrefix(str, "magnet:") {
		return str, nil
	}

	b64buf.Reset()
	b64 := base64.NewEncoder(base64.StdEncoding, b64buf)

	var reader io.ReadCloser
	if strings.HasPrefix(str, "http:") || strings.HasPrefix(str, "https:") {
		var redirMagnet string
		c := &http.Client{
			Timeout: time.Second * 60,
			CheckRedirect: func(next *http.Request, via []*http.Request) error {
				if next.URL.Scheme == "magnet" {
					redirMagnet = next.URL.String()
					return errors.New("magnet redirect")
				}
				if len(via) >= 8 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		}

		res, err := httpGet(ctx, c, str, nil)
		if redirMagnet != "" {
			if res != nil {
				res.Body.Close()
			}
			return redirMagnet, nil
		}

		if err != nil {
			return "", err
		}

		reader = res.Body
	} else {
		f, err := os.Open(str)
		if err != nil {
			return "", err
		}
		reader = f
	}

	r := io.TeeReader(reader, b64)
	var l interface{}
	err := bencode.Unmarshal(r, &l)
	reader.Close()
	b64.Close()
	if err != nil {
		return "", err
	}

	return b64buf.String(), nil
}

func cmdAdd(ctx context.Context, conf addConfig, c api.Client, items []string) error {
	d, err := abs(ctx, conf.baseDir, conf.dir, c)
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

func add(ctx context.Context, conf addConfig, c api.Client, dir, item string) (api.Torrent, error) {
	clean, err := parseAdd(ctx, item)
	if err != nil {
		const m = 40
		if len(item) > m+3 {
			item = item[:m/2] + "..." + item[len(item)-m/2:]
		}

		return api.Torrent{}, fmt.Errorf("invalid torrent/magnet: '%s': %w", item, err)
	}

	var torrent api.Torrent
	err = c.Add(ctx, conf.labels, dir, []string{clean}, func(t api.Torrent) error {
		torrent = t
		return nil
	})

	return torrent, err
}
