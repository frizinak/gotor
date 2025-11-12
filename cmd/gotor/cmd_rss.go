package main

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/frizinak/gotor/api"
	"github.com/frizinak/gotor/rss"
)

type rssFlags struct {
	*addFlags
	*watchFlags
}

func (f rssFlags) Parse(uc userConfig, o io.Writer) rssConfig {
	var c rssConfig
	c.addConfig = f.addFlags.Parse(uc, o)
	c.rss = uc.RSS
	c.rssFilters = uc.RSSFilters
	c.cacheDir = filepath.Join(uc.CacheDirectory, "rss")
	c.sleep = time.Duration(f.watch)

	return c
}

type rssConfig struct {
	addConfig

	cacheDir   string
	rss        map[string]rssFeed
	rssFilters []rssFilter
	sleep      time.Duration
}

func rssFilename(dir, name, ext string) string {
	return filepath.Join(dir, fmt.Sprintf("%s%s", name, ext))
}

func rssLast(dir, name string) (time.Time, error) {
	r, err := os.ReadFile(rssFilename(dir, name, ".stamp"))
	if err != nil {
		if os.IsNotExist(err) {
			err = nil
		}
		return time.Time{}, err
	}
	v, err := strconv.ParseInt(string(r), 10, 64)
	if err != nil {
		return time.Time{}, err
	}

	return time.Unix(v, 0), nil
}

func rssLastUpdate(dir, name string) error {
	fp := filepath.Join(rssFilename(dir, name, ".stamp"))
	tmp := tmpFile(fp, ".tmp")
	d := []byte(strconv.FormatInt(time.Now().Unix(), 10))
	if err := os.WriteFile(tmp, d, 0644); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, fp)
}

func rssDo(ctx context.Context, id string, conf rssConfig) ([]*rss.Item, error) {
	def := conf.rss[id]
	last, err := rssLast(conf.cacheDir, id)
	if err != nil {
		return nil, err
	}

	if time.Since(last) < time.Duration(def.Interval) {
		return nil, nil
	}

	s := time.Now()
	if conf.verbose != 0 {
		fmt.Fprintf(conf.output, "Fetching RSS feed '%s'\n", id)
	}

	res, err := httpGet(ctx, def.URL, time.Duration(def.Timeout), func(r *http.Request) {
		r.Header.Set("Accept", "application/xml")
	})
	if conf.verbose != 0 {
		fmt.Fprintf(
			conf.output,
			"Fetched RSS feed '%s' in %s\n",
			id,
			time.Since(s).Round(time.Millisecond*10),
		)
	}
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()
	var body io.ReadCloser = res.Body
	if res.Header.Get("Content-Encoding") == "gzip" {
		body, err = gzip.NewReader(body)
		if err != nil {
			return nil, err
		}
		defer body.Close()
	}

	cacheFile := rssFilename(conf.cacheDir, id, ".xml")

	tmp := tmpFile(cacheFile, ".tmp")
	opf, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}

	ipff, err := os.Open(cacheFile)
	cleanup := func(err error) error {
		opf.Close()
		if ipff != nil {
			ipff.Close()
		}

		if err != nil {
			os.Remove(tmp)
			return err
		}

		err = os.Rename(tmp, cacheFile)
		if err == nil {
			err = rssLastUpdate(conf.cacheDir, id)
		}
		return err
	}

	var ipf io.Reader
	ipf = ipff
	if os.IsNotExist(err) {
		err = nil
		ipf = strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
</rss>`)
	}
	if err != nil {
		return nil, cleanup(err)
	}

	items, err := rss.Parse(ipf, body, opf)
	return items, cleanup(err)
}

func cmdRSS(ctx context.Context, conf rssConfig, c api.Client) error {
	_ = os.MkdirAll(conf.cacheDir, 0755)
	tofetch := make(map[string]struct{}, len(conf.rss))
	filters := make(map[string][]rssFilter, len(conf.rss))

	feedsByTags := make(map[string][]string, len(conf.rss))
	for id, feed := range conf.rss {
		for _, tag := range feed.Tags {
			feedsByTags[tag] = append(feedsByTags[tag], id)
		}
	}

	for _, f := range conf.rssFilters {
		if f.Disabled {
			continue
		}

		if err := f.Compile(); err != nil {
			return err
		}

		if conf.verbose > 1 {
			fmt.Fprintf(
				conf.output,
				"Active filter: match '%s' exclude '%s'\n",
				f.Match,
				f.Exclude,
			)
		}

		for _, tag := range f.Tags {
			for _, id := range feedsByTags[tag] {
				if _, ok := conf.rss[id]; !ok {
					return fmt.Errorf("rss feed '%s' is not defined", id)
				}
				tofetch[id] = struct{}{}
				filters[id] = append(filters[id], f)
			}
		}
	}

	type result struct {
		id   string
		err  error
		list []*rss.Item
	}

	for {
		ch := make(chan result, 1)
		rl := make(map[string]*sync.Mutex)
		var wg sync.WaitGroup
		for id := range tofetch {
			u, _ := url.Parse(conf.rss[id].URL)
			if rl[u.Host] == nil {
				var l sync.Mutex
				rl[u.Host] = &l
			}
			wg.Add(1)
			go func(id string, sem *sync.Mutex) {
				sem.Lock()
				r, err := rssDo(ctx, id, conf)
				sem.Unlock()
				ch <- result{id: id, err: err, list: r}
				wg.Done()
			}(id, rl[u.Host])
		}

		go func() {
			wg.Wait()
			close(ch)
		}()

		type match struct {
			*rss.Item
			filter rssFilter
		}
		errs := make([]string, 0)
		matches := make(map[string]match)
		for r := range ch {
			if r.err != nil {
				errs = append(
					errs,
					fmt.Sprintf(
						"error during rss fetch of %s: %s",
						r.id,
						r.err.Error(),
					),
				)
				continue
			}

			if conf.verbose > 1 && len(r.list) != 0 {
				v := "items"
				if len(r.list) == 1 {
					v = "item"
				}
				fmt.Fprintf(
					conf.output,
					"RSS feed '%s' produced %d new %s\n",
					r.id,
					len(r.list),
					v,
				)
			}

			for _, item := range r.list {
				if _, ok := matches[item.Title]; ok {
					continue
				}
				if conf.verbose > 2 {
					fmt.Fprintf(
						conf.output,
						"New RSS entry: '%s'\n",
						item.Title,
					)
				}
				for _, f := range filters[r.id] {
					if f.Test(item.Title, item.Size) {
						fmt.Fprintf(
							conf.output,
							"Found '%s' which matches filter '%s' on rss feed '%s'\n",
							item.Title,
							f.Match,
							r.id,
						)

						matches[item.Title] = match{Item: item, filter: f}
					}
				}
			}
		}

		if conf.verbose != 0 && len(matches) != 0 {
			fmt.Fprintln(conf.output, "Adding matches to torrent client")
		}

		for _, match := range matches {
			addConfig := conf.addConfig
			addConfig.downloadPath = match.filter.DownloadDirectory
			addConfig.labels = match.filter.Labels
			addConfig.labels = append(addConfig.labels, "gotor")
			err := cmdAdd(ctx, addConfig, c, []string{match.Link})
			if err != nil {
				errs = append(
					errs,
					fmt.Sprintf(
						"could not add torrent '%s': %s",
						match.Title,
						err.Error(),
					),
				)
			}
		}

		if len(errs) != 0 {
			e := strings.Join(errs, "\n")
			if conf.sleep == 0 {
				return errors.New(e)
			}
			fmt.Fprintln(conf.output, e)
		}

		if conf.sleep == 0 {
			break
		}
		time.Sleep(conf.sleep)
	}

	return nil
}
