package rss

import (
	"encoding/xml"
	"io"
	"slices"
	"time"
)

type rss struct {
	Items []*Item `xml:"channel>item"`
}

type Item struct {
	Title string `xml:"title"`
	Link  string `xml:"link"`
	Size  uint64 `xml:"size"`
	Date  string `xml:"pubDate"`
	time  *time.Time
}

func (i *Item) Time() time.Time {
	if i.time != nil {
		return *i.time
	}
	t, err := time.Parse(time.RFC1123Z, i.Date)
	i.time = &t
	if err != nil {
		panic(err)
	}
	return t
}

func Parse(original, update io.Reader, cache io.Writer) ([]*Item, error) {
	i, di := &rss{}, xml.NewDecoder(original)
	u, du := &rss{}, xml.NewDecoder(update)
	do := xml.NewEncoder(cache)
	do.Indent("", "    ")

	if err := di.Decode(i); err != nil {
		return nil, err
	}

	if err := du.Decode(u); err != nil {
		return nil, err
	}

	uniq := make(map[string]*Item, len(u.Items))
	for _, item := range u.Items {
		uniq[item.Title] = item
	}
	for _, item := range i.Items {
		delete(uniq, item.Title)
	}

	items := make([]*Item, 0, len(uniq))
	for _, item := range uniq {
		items = append(items, item)
	}

	s := func(a, b *Item) int { return b.Time().Compare(a.Time()) }
	slices.SortFunc(u.Items, s)
	slices.SortFunc(items, s)

	return items, do.Encode(u)
}
