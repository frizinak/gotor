package api

import (
	"context"
	"fmt"
	"time"

	"github.com/frizinak/gotor/bytes"
)

var clients = map[string]Factory{}

func Register(name string, f Factory) {
	clients[name] = f
}

func GetClient(name, auth string) (Client, error) {
	c, ok := clients[name]
	if !ok {
		return nil, fmt.Errorf("no such client: '%s'", name)
	}
	return c.New(auth)
}

type TorrentCallback func(Torrent) error
type RawTorrentCallback func(interface{}, Torrent) error

type Factory interface {
	New(auth string) (Client, error)
}

type Client interface {
	Add(ctx context.Context, labels []string, dir string, torrent []string, cb TorrentCallback) error
	Remove(ctx context.Context, ids []string, deleteData bool) error
	List(ctx context.Context, cb TorrentCallback) error
	Details(ctx context.Context, ids []string, cb RawTorrentCallback) error
	Stats(ctx context.Context) (Stats, error)
	Info(ctx context.Context) (Info, error)
	Announce(ctx context.Context, ids []string) error
	Verify(ctx context.Context, ids []string) error
	Start(ctx context.Context, ids []string) error
	Stop(ctx context.Context, ids []string) error
	Move(ctx context.Context, ids []string, dir string) error
	Limit(ctx context.Context, up, down bytes.Bytes) error
}

type ErrNoSuchTorrent struct {
	ID string
}

func (e ErrNoSuchTorrent) Error() string {
	return fmt.Sprintf("torrent with id '%s' does not exist", e.ID)
}

type Status uint16

type Evaluation uint8

const (
	Good Evaluation = iota
	Bad
	Neutral
)

func (s Status) Evaluate() (string, Evaluation) {
	switch {
	case s.And(StatusError | StatusVerify):
		return statusNames[StatusVerify], Bad
	case s.And(StatusError | StatusChecking):
		return statusNames[StatusChecking], Bad
	case s.And(StatusVerify):
		return statusNames[StatusVerify], Good
	case s.And(StatusChecking):
		return statusNames[StatusChecking], Good
	case s.And(StatusError):
		return statusNames[StatusError], Bad
	case s.And(StatusFinished):
		return statusNames[StatusFinished], Good
	case s.And(StatusStopped | StatusDownloaded):
		return statusNames[StatusStopped], Neutral
	case s.And(StatusStopped):
		return statusNames[StatusStopped], Bad
	case s.And(StatusStalled | StatusDownloaded):
		return "peer", Neutral
	case s.And(StatusStalled):
		return statusNames[StatusStalled], Bad
	case s.And(StatusSeeding):
		return statusNames[StatusSeeding], Good
	case s.And(StatusDownloading):
		return statusNames[StatusDownloading], Good
	}

	for _, v := range statusOrder {
		if s&v != 0 {
			return statusNames[v], Bad
		}
	}
	return "UNK", Bad
}

func (s Status) And(status Status) bool {
	return s&status == status
}

func (s Status) Or(status Status) bool {
	return s&status != 0
}

const (
	StatusError Status = 1 << iota
	StatusStalled
	StatusIdle
	StatusStopped
	StatusMeta
	StatusChecking
	StatusDownloadQueue
	StatusSeedQueue
	StatusSeeding
	StatusDownloading
	StatusDownloaded
	StatusFinished
	StatusVerify
)

var statusNames = map[Status]string{
	StatusStalled:       "stall",
	StatusIdle:          "idle",
	StatusStopped:       "stop",
	StatusChecking:      "check",
	StatusMeta:          "meta",
	StatusDownloadQueue: "down q",
	StatusSeedQueue:     "seed q",
	StatusDownloading:   "down",
	StatusDownloaded:    "have",
	StatusSeeding:       "seed",
	StatusFinished:      "done",
	StatusError:         "error",
	StatusVerify:        "verify",
}

var statusOrder = []Status{
	StatusError,
	StatusVerify,
	StatusFinished,
	StatusStopped,
	StatusDownloadQueue,
	StatusSeedQueue,
	StatusIdle,
	StatusChecking,
	StatusMeta,
	StatusDownloading,
	StatusDownloaded,
	StatusSeeding,
	StatusStalled,
}

type Torrent struct {
	ID     string
	SortID string

	Name   string
	Path   string
	Magnet string
	Labels []string

	ETA     time.Duration
	Have    bytes.Bytes
	Total   bytes.Bytes
	Done    float64
	Recheck float64
	Sent    bytes.Bytes
	Ratio   float64

	Status        Status
	Error         string
	DownloadSpeed bytes.Bytes
	DownloadPeers int
	UploadSpeed   bytes.Bytes
	UploadPeers   int

	Updated time.Time
	Added   time.Time
}

var nilTime time.Time

func (t Torrent) Date() time.Time {
	if t.Updated != nilTime {
		return t.Updated.Local()
	}
	return t.Added.Local()
}

func (t Torrent) String() string {
	var size, dn, up string
	if t.Total.Value != 0 {
		total := t.Total.Human()
		size = fmt.Sprintf("%6.2f %10s",
			t.Have.Convert(total.Unit()).Value,
			total.Format("%6.2f %3s"),
		)
	}

	if t.DownloadSpeed.Value != 0 {
		dn = t.DownloadSpeed.Convert(bytes.MiB).Format("%6.2f %3s")
	}
	if t.UploadSpeed.Value != 0 {
		up = t.UploadSpeed.Convert(bytes.MiB).Format("%6.2f %3s")
	}

	status, _ := t.Status.Evaluate()
	return fmt.Sprintf(
		"%6s %5.1f %17s - %10s %10s - %5.2f %19s %s",
		t.ID,
		t.Done*100,
		size,
		up, dn,
		t.Ratio,
		status,
		t.Name,
	)
}

type Stats struct {
	DownloadSpeed bytes.Bytes
	UploadSpeed   bytes.Bytes
}

type Info struct {
	DefaultPath       string
	DownloadLimit     *bytes.Bytes
	UploadLimit       *bytes.Bytes
	SeedingLimit      *time.Duration
	SeedingRatioLimit *float64
}
