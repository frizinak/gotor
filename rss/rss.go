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

func ParseDiff(original, update io.Reader, cache io.Writer, cacheAmount int) ([]*Item, error) {
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
	uniq2 := make(map[string]*Item, len(i.Items))
	for _, item := range u.Items {
		uniq[item.Title] = item
		uniq2[item.Title] = item
	}

	var oldest *Item
	for _, item := range i.Items {
		if oldest == nil || item.Time().Before(oldest.Time()) {
			oldest = item
		}
		delete(uniq, item.Title)
	}
	for _, item := range uniq {
		if item.Time().Before(oldest.Time()) {
			delete(uniq, item.Title)
		}
	}

	if cacheAmount > 0 {
		for _, item := range i.Items {
			uniq2[item.Title] = item
		}
	}

	items := make([]*Item, 0, len(uniq))
	items2 := make([]*Item, 0, len(uniq2))
	for _, item := range uniq {
		items = append(items, item)
	}
	for _, item := range uniq2 {
		items2 = append(items2, item)
	}

	s := func(a, b *Item) int { return b.Time().Compare(a.Time()) }
	slices.SortFunc(items, s)
	slices.SortFunc(items2, s)

	if cacheAmount < len(u.Items) {
		cacheAmount = len(u.Items)
	}
	if len(items2) > cacheAmount {
		items2 = items2[:cacheAmount]
	}

	u.Items = items2

	return items, do.Encode(u)
}

func Parse(r io.Reader) ([]*Item, error) {
	i, di := &rss{}, xml.NewDecoder(r)
	err := di.Decode(i)
	return i.Items, err
}
