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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/frizinak/gotor/api"
	"github.com/frizinak/gotor/bytes"
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

type rssSearchFlags struct {
	*addFlags
	yes bool
}

func (f rssSearchFlags) Parse(uc userConfig, o io.Writer) rssSearchConfig {
	var c rssSearchConfig
	c.addConfig = f.addFlags.Parse(uc, o)
	c.rss = uc.RSS
	c.yes = f.yes
	c.cacheDir = filepath.Join(uc.CacheDirectory, "rss")
	c.prompter.output = o

	return c
}

type rssSearchConfig struct {
	addConfig

	prompter prompter

	cacheDir string
	rss      map[string]rssFeed
	yes      bool
}

func rssFilename(dir, name, ext string) string {
	return filepath.Join(dir, fmt.Sprintf("%s%s", name, ext))
}

func rssLast(dir, name string) (errCount int, update time.Time, err error) {
	var r []byte
	r, err = os.ReadFile(rssFilename(dir, name, ".state"))
	if err != nil {
		if os.IsNotExist(err) {
			err = nil
		}
		return
	}

	f := strings.Fields(string(r))
	var unix int64
	unix, err = strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		return
	}

	if len(f) > 1 && f[1] != "" {
		errCount, err = strconv.Atoi(f[1])
	}

	update = time.Unix(unix, 0)
	return
}

func rssLastUpdate(dir, name string, errCount int, last time.Time) error {
	fp := filepath.Join(rssFilename(dir, name, ".state"))
	tmp := tmpFile(fp, ".tmp")
	dt := time.Now()
	if errCount > 0 {
		dt = last
	}

	d := make([]byte, 0, 10+1+1)
	d = strconv.AppendInt(d, dt.Unix(), 10)
	d = append(d, ' ')
	d = strconv.AppendInt(d, int64(errCount), 10)
	if err := os.WriteFile(tmp, d, 0644); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, fp)
}

func rssDo(ctx context.Context, id string, conf rssConfig) ([]*rss.Item, error) {
	def := conf.rss[id]
	errCount, last, err := rssLast(conf.cacheDir, id)
	if err != nil {
		return nil, err
	}

	add := time.Duration(errCount*errCount) * time.Minute * 5
	const max = time.Hour * 10
	if add > max {
		add = max
	}
	if time.Since(last) < time.Duration(def.Interval)+add {
		return nil, nil
	}

	s := time.Now()
	res, err := httpGet(ctx, &http.Client{Timeout: time.Duration(def.Timeout)}, def.URL, func(r *http.Request) {
		r.Header.Set("Accept", "application/xml")
	})
	if conf.verbose != 0 {
		fmt.Fprintf(
			conf.output,
			"[INF] Fetched RSS feed '%s' in %s\n",
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
			rssLastUpdate(conf.cacheDir, id, errCount+1, last)
			os.Remove(tmp)
			return err
		}

		err = os.Rename(tmp, cacheFile)
		if err == nil {
			err = rssLastUpdate(conf.cacheDir, id, 0, last)
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

	items, err := rss.ParseDiff(ipf, body, opf)
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

	for i, f := range conf.rssFilters {
		if f.Disabled {
			continue
		}

		if err := f.Compile(); err != nil {
			return err
		}

		count := 0
		for _, tag := range f.Tags {
			for _, id := range feedsByTags[tag] {
				if conf.rss[id].Disabled {
					continue
				}
				count++
				tofetch[id] = struct{}{}
				filters[id] = append(filters[id], f)
			}
		}

		if conf.verbose > 0 && count == 0 {
			fmt.Fprintf(
				conf.output,
				"[WRN] Filter %d ('%s') has no matching RSS feeds\n",
				i+1,
				f.Match,
			)
		} else if conf.verbose > 1 {
			fmt.Fprintf(
				conf.output,
				"[INF] Active filter %d: match '%s' exclude '%s'\n",
				i+1,
				f.Match,
				f.Exclude,
			)
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
					"[INF] RSS feed '%s' produced %d new %s\n",
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
						"[DBG] New RSS entry: '%s'\n",
						item.Title,
					)
				}
				for _, f := range filters[r.id] {
					if f.Test(item.Title, item.Size) {
						fmt.Fprintf(
							conf.output,
							"[INF] Found '%s' which matches filter '%s' on rss feed '%s'\n",
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
			fmt.Fprintln(conf.output, "[INF] Adding matches to torrent client")
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
			if conf.sleep == 0 {
				return errors.New(strings.Join(errs, "\n"))
			}
			for _, e := range errs {
				fmt.Fprintln(conf.output, "[ERR]", e)
			}
		}

		if conf.sleep == 0 {
			break
		}
		time.Sleep(conf.sleep)
	}

	return nil
}

func cmdRSSSearch(ctx context.Context, conf rssSearchConfig, c api.Client, q *regexp.Regexp) error {
	ask := func(item *rss.Item) (yes, ok bool) {
		ok = true
		if conf.yes {
			yes = true
			return
		}
		size := "?"
		if item.Size != 0 {
			size = bytes.New(float64(item.Size/1024), bytes.KiB).Human().String()
		}
		fmt.Fprintf(conf.output, "Add '%s' [%s]\n", item.Title, size)
		yes, ok = conf.prompter.YN("Add?", true)
		return
	}

	links := make([]string, 0, 1)

	uniq := make(map[string]struct{}, 0)
	matches := 0
	for id := range conf.rss {
		f := rssFilename(conf.cacheDir, id, ".xml")
		r, err := os.Open(f)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		items, err := rss.Parse(r)
		r.Close()
		if err != nil {
			return err
		}

		for _, item := range items {
			if _, ok := uniq[item.Title]; ok {
				continue
			}
			uniq[item.Title] = struct{}{}

			if !q.MatchString(item.Title) {
				continue
			}

			matches++
			yes, ok := ask(item)
			if !ok {
				return errors.New("aborted")
			}
			if yes {
				links = append(links, item.Link)
			}
		}
	}

	if matches == 0 {
		fmt.Fprintln(conf.output, "no torrents match the given regex.")
		return nil
	}

	if len(links) == 0 {
		fmt.Fprintln(conf.output, "no torrents were added.")
		return nil
	}

	return cmdAdd(ctx, conf.addConfig, c, links)
}
