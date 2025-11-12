package transmission

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/frizinak/gotor/api"
	"github.com/frizinak/gotor/bytes"
	rpc "github.com/frizinak/transmissionrpc"
)

var fields [maxEndpoints][]string

func init() {
	api.Register("transmission", Factory{})

	for ep := range fields {
		fields[ep] = make([]string, 0, len(_fields))
	}
	for name, def := range _fields {
		for ep, enabled := range def {
			if enabled == x {
				fields[ep] = append(fields[ep], name)
			}
		}
	}
}

type Client struct {
	rpc *rpc.Client
}

type Factory struct{}

func (f Factory) New(auth string) (api.Client, error) { return New(auth) }

func New(auth string) (*Client, error) {
	u, err := url.Parse(auth)
	if err != nil {
		return nil, err
	}
	c, err := rpc.New(u, nil)
	if err != nil {
		return nil, err
	}

	return &Client{rpc: c}, nil
}

func idint64(ids []string) ([]int64, error) {
	idns := make([]int64, len(ids))
	for i, id := range ids {
		idn, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return idns, fmt.Errorf("could not parse '%s' as a torrent id", id)
		}
		idns[i] = idn
	}
	return idns, nil
}

func (c *Client) Remove(ctx context.Context, ids []string, deleteData bool) error {
	idns, err := idint64(ids)
	if err != nil {
		return err
	}
	return c.remove(ctx, idns, deleteData)
}

func (c *Client) remove(ctx context.Context, ids []int64, deleteData bool) error {
	return c.rpc.TorrentRemove(
		ctx,
		rpc.TorrentRemovePayload{IDs: ids, DeleteLocalData: deleteData},
	)
}

func (c *Client) Add(
	ctx context.Context,
	labels []string,
	dir string,
	torrents []string,
	cb api.TorrentCallback,
) error {
	for _, t := range torrents {
		if err := c.add(ctx, labels, dir, t, cb); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) add(
	ctx context.Context,
	labels []string,
	dir,
	torrent string,
	cb api.TorrentCallback,
) error {
	pl := rpc.TorrentAddPayload{}
	if dir != "" {
		pl.DownloadDir = &dir
	}
	pl.Labels = labels

	prop := &pl.MetaInfo
	if strings.HasPrefix(torrent, "magnet:") {
		prop = &pl.Filename
	}

	*prop = &torrent
	t, err := c.rpc.TorrentAdd(ctx, pl)
	if err == nil {
		return cb(raw2Torrent(t))
	}

	return err
}

func (c *Client) Info(ctx context.Context) (api.Info, error) {
	var a api.Info
	fields := []string{
		"download-dir",
		"speed-limit-down-enabled",
		"speed-limit-down",
		"speed-limit-up-enabled",
		"speed-limit-up",
		"idle-seeding-limit-enabled",
		"idle-seeding-limit",
		"seedRatioLimited",
		"seedRatioLimit",
	}

	r, err := c.rpc.SessionArgumentsGet(ctx, fields)
	if err != nil {
		return a, err
	}

	a.DefaultPath = filepath.Clean(pv(r.DownloadDir))
	if pv(r.SpeedLimitDownEnabled) {
		v := bytes.New(float64(pv(r.SpeedLimitDown)), bytes.KiB)
		a.DownloadLimit = &v
	}
	if pv(r.SpeedLimitUpEnabled) {
		v := bytes.New(float64(pv(r.SpeedLimitUp)), bytes.KiB)
		a.UploadLimit = &v
	}
	if pv(r.IdleSeedingLimitEnabled) {
		v := time.Duration(pv(r.IdleSeedingLimit)) * time.Minute
		a.SeedingLimit = &v
	}
	if pv(r.SeedRatioLimited) {
		v := pv(r.SeedRatioLimit)
		a.SeedingRatioLimit = &v
	}
	return a, nil
}

func (c *Client) Stats(ctx context.Context) (api.Stats, error) {
	var s api.Stats
	r, err := c.rpc.SessionStats(ctx)
	s.DownloadSpeed = bytes.New(float64(r.DownloadSpeed), bytes.B)
	s.UploadSpeed = bytes.New(float64(r.UploadSpeed), bytes.B)
	return s, err
}

type TorrentGetParams struct {
	Fields []string `json:"fields"`
	IDs    []int64  `json:"ids,omitempty"`
}

type KV struct {
	K, V string
}

func (c *Client) Details(ctx context.Context, ids []string, cb api.RawTorrentCallback) error {
	idns, err := idint64(ids)
	if err != nil {
		return err
	}
	rts, err := c.details(ctx, idns)
	if err != nil {
		return err
	}
	for _, rt := range rts {
		if err = cb(rt, raw2Torrent(rt)); err != nil {
			return err
		}
	}

	return err
}

func (c *Client) details(ctx context.Context, ids []int64) ([]rpc.Torrent, error) {
	res, err := c.rpc.TorrentGet(ctx, fields[detail], ids)
	if err != nil {
		return res, err
	}
	m := make(map[int64]struct{}, len(res))
	for _, t := range res {
		m[pv(t.ID)] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := m[id]; !ok {
			return res, api.ErrNoSuchTorrent{strconv.FormatInt(id, 10)}
		}
	}
	return res, nil

}

func (c *Client) List(ctx context.Context, cb api.TorrentCallback) error {
	r, err := c.rpc.TorrentGet(ctx, fields[list], nil)
	if err != nil {
		return err
	}

	for _, t := range r {
		if err = cb(raw2Torrent(t)); err != nil {
			return err
		}
	}

	return nil
}

const x = 1

const (
	detail = iota
	list
	maxEndpoints
)

var _fields = map[string][maxEndpoints]int{
	"activityDate":            {detail: x, list: x},
	"addedDate":               {detail: x, list: x},
	"availability":            {detail: 0, list: 0},
	"bandwidthPriority":       {detail: x, list: 0},
	"comment":                 {detail: x, list: 0},
	"corruptEver":             {detail: x, list: 0},
	"creator":                 {detail: x, list: 0},
	"dateCreated":             {detail: x, list: x},
	"desiredAvailable":        {detail: x, list: 0},
	"doneDate":                {detail: x, list: 0},
	"downloadDir":             {detail: x, list: x},
	"downloadedEver":          {detail: x, list: 0},
	"downloadLimit":           {detail: x, list: 0},
	"downloadLimited":         {detail: x, list: 0},
	"editDate":                {detail: x, list: 0},
	"error":                   {detail: x, list: x},
	"errorString":             {detail: x, list: x},
	"eta":                     {detail: x, list: x},
	"etaIdle":                 {detail: x, list: 0},
	"file-count":              {detail: x, list: 0},
	"files":                   {detail: x, list: 0},
	"fileStats":               {detail: x, list: 0},
	"group":                   {detail: x, list: 0},
	"hashString":              {detail: x, list: 0},
	"haveUnchecked":           {detail: x, list: 0},
	"haveValid":               {detail: x, list: x},
	"honorsSessionLimits":     {detail: x, list: 0},
	"id":                      {detail: x, list: x},
	"isFinished":              {detail: x, list: x},
	"isPrivate":               {detail: x, list: x},
	"isStalled":               {detail: x, list: x},
	"labels":                  {detail: x, list: x},
	"leftUntilDone":           {detail: x, list: 0},
	"magnetLink":              {detail: x, list: x},
	"manualAnnounceTime":      {detail: x, list: 0},
	"maxConnectedPeers":       {detail: x, list: 0},
	"metadataPercentComplete": {detail: x, list: x},
	"name":                    {detail: x, list: x},
	"peer-limit":              {detail: x, list: 0},
	"peers":                   {detail: x, list: 0},
	"peersConnected":          {detail: x, list: 0},
	"peersFrom":               {detail: x, list: 0},
	"peersGettingFromUs":      {detail: x, list: x},
	"peersSendingToUs":        {detail: x, list: x},
	"percentComplete":         {detail: x, list: 0},
	"percentDone":             {detail: x, list: x},
	"pieces":                  {detail: x, list: 0},
	"pieceCount":              {detail: x, list: 0},
	"pieceSize":               {detail: x, list: 0},
	"priorities":              {detail: x, list: 0},
	"primary-mime-type":       {detail: x, list: 0},
	"queuePosition":           {detail: x, list: 0},
	"rateDownload":            {detail: x, list: x},
	"rateUpload":              {detail: x, list: x},
	"recheckProgress":         {detail: x, list: 0},
	"secondsDownloading":      {detail: x, list: 0},
	"secondsSeeding":          {detail: x, list: 0},
	"seedIdleLimit":           {detail: x, list: 0},
	"seedIdleMode":            {detail: x, list: 0},
	"seedRatioLimit":          {detail: x, list: 0},
	"seedRatioMode":           {detail: x, list: 0},
	"sizeWhenDone":            {detail: x, list: x},
	"startDate":               {detail: x, list: 0},
	"status":                  {detail: x, list: x},
	"trackers":                {detail: x, list: 0},
	"trackerList":             {detail: x, list: 0},
	"trackerStats":            {detail: x, list: 0},
	"totalSize":               {detail: x, list: 0},
	"torrentFile":             {detail: x, list: 0},
	"uploadedEver":            {detail: x, list: x},
	"uploadLimit":             {detail: x, list: 0},
	"uploadLimited":           {detail: x, list: 0},
	"uploadRatio":             {detail: x, list: x},
	"wanted":                  {detail: x, list: 0},
	"webseeds":                {detail: x, list: 0},
	"webseedsSendingToUs":     {detail: x, list: 0},
}

func raw2Torrent(t rpc.Torrent) api.Torrent {
	meta := pv(t.MetadataPercentComplete)
	ratio := pv(t.UploadRatio)
	if ratio < 0 {
		ratio = 0
	}
	haveValid := pv(t.HaveValid)
	sizeWhenDone := pv(t.SizeWhenDone)
	ti := api.Torrent{
		ID:     strconv.FormatInt(pv(t.ID), 10),
		SortID: fmt.Sprintf("%06d", pv(t.ID)),
		Name:   pv(t.Name),
		Path:   filepath.Clean(pv(t.DownloadDir)),
		Magnet: pv(t.MagnetLink),
		Labels: t.Labels,

		ETA:   time.Second * time.Duration(pv(t.ETA)),
		Have:  bytes.New(float64(haveValid/1024), bytes.KiB),
		Total: bytes.New(float64(sizeWhenDone/1024), bytes.KiB),
		Done:  meta/100 + pv(t.PercentDone)*.99,
		Sent:  bytes.New(float64(pv(t.UploadedEver)/1024), bytes.KiB),
		Ratio: ratio,

		Error: pv(t.ErrorString),

		DownloadSpeed: bytes.New(float64(pv(t.RateDownload)), bytes.B),
		DownloadPeers: int(pv(t.PeersSendingToUs)),
		UploadSpeed:   bytes.New(float64(pv(t.RateUpload)), bytes.B),
		UploadPeers:   int(pv(t.PeersGettingFromUs)),

		Updated: pv(t.ActivityDate),
		Added:   pv(t.AddedDate),
	}
	if pv(t.IsStalled) {
		ti.Status |= api.StatusStalled
	}

	if meta < 1.0 {
		ti.Status |= api.StatusMeta
	}

	if sizeWhenDone != 0 && haveValid == sizeWhenDone {
		ti.Status |= api.StatusDownloaded
	}

	switch pv(t.Status) {
	case rpc.TorrentStatusStopped:
		ti.Status |= api.StatusStopped
	case rpc.TorrentStatusCheckWait:
		ti.Status |= api.StatusChecking
	case rpc.TorrentStatusCheck:
		ti.Status |= api.StatusChecking
	case rpc.TorrentStatusDownloadWait:
		ti.Status |= api.StatusDownloadQueue
	case rpc.TorrentStatusDownload:
		ti.Status |= api.StatusDownloading
	case rpc.TorrentStatusSeedWait:
		ti.Status |= api.StatusSeedQueue
	case rpc.TorrentStatusSeed:
		ti.Status |= api.StatusSeeding
	case rpc.TorrentStatusIsolated:
		ti.Status |= api.StatusIdle
	}
	if pv(t.IsFinished) {
		ti.Status = api.StatusFinished | api.StatusDownloaded
	}
	if t.Error != nil && *t.Error != 0 {
		ti.Status |= api.StatusError
	}

	return ti
}
