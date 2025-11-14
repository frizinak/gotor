package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/frizinak/gotor/api"
	_ "github.com/frizinak/gotor/api/transmission"
	"github.com/frizinak/gotor/bytes"
	"github.com/frizinak/gotor/flags"
	"gopkg.in/yaml.v3"
)

type cmdFlags struct {
	config  string
	noColor bool
	v       bool
	vv      bool
	vvv     bool
}

func (f cmdFlags) Parse(uc userConfig, o io.Writer) cmdConfig {
	var v uint8
	switch {
	case f.vvv:
		v = 3
	case f.vv:
		v = 2
	case f.v:
		v = 1
	}
	return cmdConfig{color: !f.noColor, output: o, verbose: v}
}

type cmdConfig struct {
	color   bool
	output  io.Writer
	verbose uint8
}

type downloadDirFlags struct{}

func (f downloadDirFlags) Parse(uc userConfig, o io.Writer) downloadDirConfig {
	return downloadDirConfig{strings.TrimRight(uc.DownloadDirectory, "/\\")}
}

type downloadDirConfig struct {
	downloadDirectory string
}

type rssFeed struct {
	URL      string       `yaml:"url"`
	Interval flagDuration `yaml:"interval"`
	Timeout  flagDuration `yaml:"timeout"`
	Tags     []string     `yaml:"tags"`
	Cache    int          `yaml:"cache"`
	Disabled bool         `yaml:"disabled,omitempty"`
}

type rssFilter struct {
	Disabled          bool     `yaml:"disabled,omitempty"`
	Tags              []string `yaml:"tags"`
	Match             string   `yaml:"match"`
	Exclude           string   `yaml:"exclude"`
	Labels            []string `yaml:"labels"`
	DownloadDirectory string   `yaml:"directory"`
	MinSize           string   `yaml:"min-size"`
	MaxSize           string   `yaml:"max-size"`

	match   *regexp.Regexp
	exclude *regexp.Regexp
	size    struct{ min, max uint64 }
}

func (f *rssFilter) Compile() error {
	if strings.TrimSpace(f.Match) == "" {
		return errors.New("filters can't have an empty match definition")
	}

	var err error
	f.match, err = regexp.Compile("(?i)" + f.Match)
	if err != nil {
		return err
	}
	if strings.TrimSpace(f.Exclude) != "" {
		f.exclude, err = regexp.Compile("(?i)" + f.Exclude)
	}

	if val := strings.TrimSpace(f.MinSize); val != "" {
		s, err := bytes.Parse(val)
		if err != nil {
			return err
		}
		f.size.min = uint64(s.Convert(bytes.B).Value)
	}

	if val := strings.TrimSpace(f.MaxSize); val != "" {
		s, err := bytes.Parse(val)
		if err != nil {
			return err
		}
		f.size.max = uint64(s.Convert(bytes.B).Value)
	}

	return err
}

func (f *rssFilter) Test(item string, size uint64) bool {
	return (size == 0 || (size >= f.size.min && (f.size.max == 0 || size <= f.size.max))) &&
		f.match.MatchString(item) &&
		(f.exclude == nil || !f.exclude.MatchString(item))
}

type userConfig struct {
	TLS               bool               `yaml:"tls"`
	Host              string             `yaml:"host"`
	Port              interface{}        `yaml:"port"`
	User              string             `yaml:"username"`
	Password          string             `yaml:"password"`
	RPC               string             `yaml:"rpc-path"`
	DownloadDirectory string             `yaml:"download-dir"`
	CacheDirectory    string             `yaml:"cache-directory"`
	RSS               map[string]rssFeed `yaml:"rss-feeds"`
	RSSFilters        []rssFilter        `yaml:"rss-filters"`
}

func (c userConfig) URL() string {
	prot := "http"
	if c.TLS {
		prot = "https"
	}
	up := ""
	switch {
	case c.Password != "":
		up = fmt.Sprintf("%s:%s@", c.User, c.Password)
	case c.User != "":
		up = fmt.Sprintf("%s@", c.User)
	}
	path := ""
	t := strings.Trim(c.RPC, "/ ")
	if t != "" {
		path = "/" + t
	}
	return fmt.Sprintf("%s://%s%s:%v%s", prot, up, c.Host, c.Port, path)
}

func loadUserConfig(path string) (userConfig, error) {
	var c userConfig
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	dec := yaml.NewDecoder(f)
	err = dec.Decode(&c)
	f.Close()

	if c.CacheDirectory == "" {
		c.CacheDirectory = defaultCacheDirectory()
	}

	return c, err
}

func saveUserConfig(path string, c userConfig) error {
	c.Port = fmt.Sprintf("%v", c.Port)

	tmp := tmpFile(path, ".tmp")
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	err = enc.Encode(c)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, path)
}

func parseCategoryArg(cat string) (labels []string, dir string) {
	cat = strings.Trim(cat, "/\\")
	if cat == "" {
		cat = "/"
	}
	dir = cat
	labels = []string{cat, "gotor"}
	return
}

var defaultConfigDirectory func() string
var defaultCacheDirectory func() string

func defaultConfigPath() string {
	return filepath.Join(defaultConfigDirectory(), "config.yml")
}

func init() {
	var _config string
	defaultConfigDirectory = func() string {
		if _config != "" {
			return _config
		}
		dir, err := os.UserConfigDir()
		if err != nil {
			var udir string
			if udir, err = os.UserHomeDir(); err == nil {
				dir = filepath.Join(udir, ".config")
			}
		}
		if err != nil {
			dir = "./"
		}

		_config = filepath.Join(dir, "gotor")
		return _config
	}

	var _cache string
	defaultCacheDirectory = func() string {
		if _cache != "" {
			return _cache
		}
		dir, err := os.UserCacheDir()
		if err != nil {
			var udir string
			if udir, err = os.UserHomeDir(); err == nil {
				dir = filepath.Join(udir, ".cache")
			}
		}
		if err != nil {
			dir = "./"
		}

		_cache = filepath.Join(dir, "gotor")
		return _cache
	}
}

func main() {
	me := os.Args[0]
	out := os.Stdout

	clientConf := func(flags cmdFlags) (userConfig, error) {
		confPath := defaultConfigPath()
		if flags.config != "" {
			confPath = flags.config
		}
		conf, err := loadUserConfig(confPath)
		if os.IsNotExist(err) {
			return conf, fmt.Errorf(
				`No config file found at '%s'.
You can use the '%s config create' to create one`,
				confPath,
				me,
			)
		}
		return conf, err
	}

	client := func(flags cmdFlags) (api.Client, userConfig, error) {
		conf, err := clientConf(flags)
		if err != nil {
			return nil, conf, err
		}

		c, err := api.GetClient("transmission", conf.URL())
		return c, conf, err
	}

	flagsDefault := func(f *flag.FlagSet, flags *cmdFlags) {
		f.StringVar(
			&flags.config,
			"c",
			defaultConfigPath(),
			"path to the config file",
		)
	}

	flagsColor := func(f *flag.FlagSet, flags *cmdFlags) {
		f.BoolVar(&flags.noColor, "C", false, "Disable colors.")
	}

	flagsVerbose := func(f *flag.FlagSet, flags *cmdFlags) {
		f.BoolVar(&flags.v, "v", false, "Be verbose.")
		f.BoolVar(&flags.vv, "vv", false, "Be more verbose.")
		f.BoolVar(&flags.vvv, "vvv", false, "Be even more verbose.")
	}

	flagsFilters := func(f *flag.FlagSet, flags *filterFlags) {
		f.StringVar(
			&flags.added[0],
			"added-since",
			"",
			`Filter added since this date.
Format "YYYY-MM-DD hh:mm:ss"
where either the date or the time and/or seconds and/or year can be omitted.`,
		)

		f.StringVar(
			&flags.added[1],
			"added-until",
			"",
			`Filter added before this date. See -added-since.`,
		)

		f.StringVar(
			&flags.updated[0],
			"updated-since",
			"",
			`Filter updated since this date. See -added-since.`,
		)

		f.StringVar(
			&flags.updated[1],
			"updated-until",
			"",
			`Filter updated until this date. See -added-since.`,
		)

		f.Var(
			&flags.status,
			"s",
			`Filter statuses.
Use any of stall, stop, queue, down, seed, done, check, have, meta and error.
Match multiple statuses by separating them with a comma. Negate with ^ or !.
Specify this flag multiple times to filter multiple statuses.`,
		)

		f.StringVar(
			&flags.id,
			"i",
			"",
			`Filter id.
Separate multiple values with a comma and specify ranges with a dash.`,
		)

		f.Var(&flags.path, "p", "Filter paths with perl regexes.")
		f.Var(&flags.label, "l", "Filter labels with perl regexes.")
		f.Var(&flags.name, "n", "Filter names with perl regexes.")
		f.Var(&flags.pathNot, "P", "Inverse-filter paths with perl regexes.")
		f.Var(&flags.labelNot, "L", "Inverse-filter labels with perl regexes.")
		f.Var(&flags.nameNot, "N", "Inverse-filter names with perl regexes.")
	}

	flagsSort := func(f *flag.FlagSet, flags *sortFlags) {
		f.BoolVar(
			&flags.noGroup,
			"G",
			false,
			"Disable grouping.",
		)

		f.StringVar(
			&flags.sort,
			"sort",
			"added",
			`Sort field.
Use any of the following separated by a comma. Reverse the sort order of any
field by prefixing it with a ^ or !.
  - id
  - name
  - added / add
  - updated / update
  - status
  - download / down
  - upload / up
  - downloaded / have
  - uploaded / seeded
  - ratio
  - done
  - size`,
		)
	}

	flagsWatch := func(f *flag.FlagSet, flags *watchFlags) {
		f.Var(
			&flags.watch,
			"w",
			"Continuously query at the given interval.",
		)
	}

	cmdFlags := &cmdFlags{}
	filterFlags := &filterFlags{}
	sortFlags := &sortFlags{}
	watchFlags := &watchFlags{}

	listFlags := listFlags{
		cmdFlags:    cmdFlags,
		filterFlags: filterFlags,
		sortFlags:   sortFlags,
		watchFlags:  watchFlags,
	}

	fr := flags.NewRoot(out).
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, listFlags.cmdFlags)
			flagsColor(f, listFlags.cmdFlags)
			flagsFilters(f, listFlags.filterFlags)
			flagsSort(f, listFlags.sortFlags)
			flagsWatch(f, listFlags.watchFlags)

			f.BoolVar(
				&listFlags.hideErrors,
				"E",
				false,
				"Hide torrent error messages.",
			)
		}).
		Handler(func(set *flags.Set, args []string) error {
			if len(args) != 0 {
				set.Usage(1)
			}

			c, userConf, err := client(*listFlags.cmdFlags)
			if err != nil {
				return err
			}
			conf, err := listFlags.Parse(userConf, out)
			if err != nil {
				return err
			}
			return cmdList(context.Background(), conf, c)
		})

	detailFlags := detailFlags{cmdFlags: cmdFlags}
	doInfo := func(set *flags.Set, args []string, mode detailMode) error {
		if len(args) != 1 {
			set.Usage(1)
		}

		c, userConf, err := client(*detailFlags.cmdFlags)
		if err != nil {
			return err
		}
		conf := detailFlags.Parse(userConf, out)
		conf.mode = mode

		err = cmdInfo(
			context.Background(),
			conf,
			c,
			args[0],
		)

		if errors.As(err, &api.ErrNoSuchTorrent{}) {
			fmt.Fprintln(os.Stderr, "no such torrent")
			os.Exit(1)
		}

		return err
	}

	info := fr.Add("info").Description("show torrent info").
		Help(func(w io.Writer) {
			fmt.Fprintln(w, "- argument 1: the torrent id")
		}).
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, cmdFlags)
			flagsColor(f, cmdFlags)
		}).
		Handler(func(set *flags.Set, args []string) error {
			return doInfo(set, args, dmDefault)
		})

	info.Add("path").Description("show torrent file path").
		Help(func(w io.Writer) {
			fmt.Fprintln(w, "- argument 1: the torrent id")
		}).
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, cmdFlags)
		}).
		Handler(func(set *flags.Set, args []string) error {
			return doInfo(set, args, dmPath)
		})

	info.Add("files").Description("show torrent file info").
		Help(func(w io.Writer) {
			fmt.Fprintln(w, "- argument 1: the torrent id")
		}).
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, cmdFlags)
			flagsColor(f, cmdFlags)
		}).
		Handler(func(set *flags.Set, args []string) error {
			return doInfo(set, args, dmFiles)
		})

	info.Add("magnet").Description("show the magnet link").
		Help(func(w io.Writer) {
			fmt.Fprintln(w, "- argument 1: the torrent id")
		}).
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, cmdFlags)
		}).
		Handler(func(set *flags.Set, args []string) error {
			return doInfo(set, args, dmMagnet)
		})

	removeFlags := removeFlags{cmdFlags: cmdFlags, filterFlags: filterFlags}
	fr.Add("remove", "delete").Description("remove torrents").
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, removeFlags.cmdFlags)
			flagsFilters(f, removeFlags.filterFlags)

			f.BoolVar(
				&removeFlags.deleteData,
				"delete",
				false,
				"also delete data",
			)

			f.BoolVar(
				&removeFlags.yes,
				"yes",
				false,
				"answer yes to all deletion prompts",
			)
		}).
		Handler(func(set *flags.Set, args []string) error {
			if len(args) != 0 {
				set.Usage(1)
			}

			c, userConf, err := client(*removeFlags.cmdFlags)
			if err != nil {
				return err
			}

			conf, err := removeFlags.Parse(userConf, out)
			if err != nil {
				return err
			}

			return cmdRemove(
				context.Background(),
				conf,
				c,
			)
		})

	torrentFlags := torrentFlags{cmdFlags: cmdFlags, filterFlags: filterFlags}
	type torrentOp func(context.Context, []string) error
	torrentHandler := func(set *flags.Set, args []string, op func(api.Client) torrentOp) error {
		if len(args) != 0 {
			set.Usage(1)
		}

		c, userConf, err := client(*torrentFlags.cmdFlags)
		if err != nil {
			return err
		}

		conf, err := torrentFlags.Parse(userConf, out)
		if err != nil {
			return err
		}

		return cmdTorrent(
			context.Background(),
			conf,
			c,
			op(c),
		)
	}

	ops := map[string]func(api.Client) torrentOp{
		"verify":   func(c api.Client) torrentOp { return c.Verify },
		"announce": func(c api.Client) torrentOp { return c.Announce },
		"start":    func(c api.Client) torrentOp { return c.Start },
		"stop":     func(c api.Client) torrentOp { return c.Stop },
	}

	for cmd, op := range ops {
		fr.Add(cmd).Description(fmt.Sprintf("%s torrents", cmd)).
			Define(func(f *flag.FlagSet) {
				flagsDefault(f, torrentFlags.cmdFlags)
				flagsFilters(f, torrentFlags.filterFlags)
			}).
			Handler(func(set *flags.Set, args []string) error {
				return torrentHandler(set, args, op)
			})
	}

	statsFlags := statsFlags{cmdFlags: cmdFlags}
	fr.Add("stats").Description("monitor stats").
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, statsFlags.cmdFlags)

			f.Float64Var(
				&statsFlags.watch,
				"w",
				0,
				"Continuously query at the given interval in seconds",
			)
		}).
		Handler(func(set *flags.Set, args []string) error {
			if len(args) != 0 {
				set.Usage(1)
			}

			c, userConf, err := client(*statsFlags.cmdFlags)
			if err != nil {
				return err
			}
			conf := statsFlags.Parse(userConf, out)
			return cmdStats(context.Background(), conf, c)
		})

	addFlags := addFlags{cmdFlags: cmdFlags}
	fr.Add("add").Description("add torrents").
		Help(func(w io.Writer) {
			fmt.Fprintln(w, "- argument 1:   the relative destination / label.")
			fmt.Fprintln(w, "- argument 2-n: torrent files or magnet URIs.")
		}).
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, addFlags.cmdFlags)
		}).
		Handler(func(set *flags.Set, args []string) error {
			if len(args) < 2 {
				set.Usage(1)
			}

			c, userConf, err := client(*addFlags.cmdFlags)
			if err != nil {
				return err
			}

			conf := addFlags.Parse(userConf, out)

			cat := args[0]
			if strings.HasPrefix(cat, "magnet:") {
				return fmt.Errorf("first argument looks like a magnet URI")
			}
			if strings.HasPrefix(cat, "http:") || strings.HasPrefix(cat, "https:") {
				return fmt.Errorf("first argument looks like a torrent url")
			}
			if strings.HasSuffix(cat, ".torrent") {
				if stat, _ := os.Stat(cat); stat != nil && !stat.IsDir() {
					return fmt.Errorf("first argument looks like a torrent file")
				}
			}

			conf.labels, conf.downloadPath = parseCategoryArg(cat)
			return cmdAdd(context.Background(), conf, c, args[1:])
		})

	rssFlags := rssFlags{addFlags: &addFlags, watchFlags: watchFlags}
	rssSearchFlags := rssSearchFlags{addFlags: &addFlags}
	fr.Add("rss").Description("RSS related operations").
		Add("filter").Description("fetch RSS feeds and add torrents based on filters").
		Help(func(w io.Writer) {
			fmt.Fprintln(w, `
Config example:
  rss-filters:
    - { match: "(?-i)^Debian.*\\.iso", tags: [linux], labels: [brr, linux], directory: linux, min-size: 5M }

  rss-feeds:
    distrowatch:
      url: https://distrowatch.com/news/torrents.xml
      interval: 1h
      timeout: 30s
      tags: [linux]
`)
		}).
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, rssFlags.cmdFlags)
			flagsVerbose(f, rssFlags.cmdFlags)
			flagsWatch(f, rssFlags.watchFlags)
		}).
		Handler(func(set *flags.Set, args []string) error {
			c, userConf, err := client(*rssFlags.cmdFlags)
			if err != nil {
				return err
			}
			conf := rssFlags.Parse(userConf, out)
			return cmdRSS(context.Background(), conf, c)
		}).
		Parent().Add("search").Description("search and add torrents within cached RSS feeds").
		Help(func(w io.Writer) {
			fmt.Fprintln(w, "- argument 1: the relative destination / label.")
			fmt.Fprintln(w, "- argument 2: the search regex.")
		}).
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, rssSearchFlags.cmdFlags)

			f.BoolVar(
				&rssSearchFlags.yes,
				"yes",
				false,
				"add all matched torrents without prompting",
			)
		}).
		Handler(func(set *flags.Set, args []string) error {
			if len(args) != 2 {
				set.Usage(1)
			}
			q, err := regexp.Compile("(?i)" + args[1])
			if err != nil {
				return err
			}
			c, userConf, err := client(*rssSearchFlags.cmdFlags)
			if err != nil {
				return err
			}
			conf := rssSearchFlags.Parse(userConf, out)
			conf.labels, conf.downloadPath = parseCategoryArg(args[0])
			return cmdRSSSearch(context.Background(), conf, c, q)
		})

	configCreateFlags := cmdFlags
	fr.Add("config").Add("create").Description("create default config file").
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, configCreateFlags)
		}).
		Handler(func(set *flags.Set, args []string) error {
			if len(args) != 0 {
				set.Usage(1)
			}

			confPath := defaultConfigPath()
			if configCreateFlags.config != "" {
				confPath = configCreateFlags.config
			}
			_, err := loadUserConfig(confPath)
			if err == nil {
				return fmt.Errorf("config '%s' already exists", confPath)
			}
			if !os.IsNotExist(err) {
				return err
			}
			dir := filepath.Dir(confPath)
			_ = os.MkdirAll(dir, 0750)

			c := userConfig{
				Host:           "localhost",
				Port:           "9091",
				User:           "",
				Password:       "",
				RPC:            "/transmission/rpc",
				CacheDirectory: defaultCacheDirectory(),
				RSS: map[string]rssFeed{
					"distrowatch": {
						URL:      "https://distrowatch.com/news/torrents.xml",
						Interval: flagDuration(time.Hour * 2),
						Timeout:  flagDuration(time.Second * 30),
						Tags:     []string{"iso"},
						Cache:    1000,
					},
				},
				RSSFilters: []rssFilter{
					{
						Disabled:          true,
						Match:             "^arch linux.*\\.iso",
						Exclude:           "",
						Labels:            []string{"rss", "linux"},
						DownloadDirectory: "linux",
						Tags:              []string{"iso"},
						MinSize:           "5M",
						MaxSize:           "5G",
					},
				},
			}

			err = saveUserConfig(confPath, c)
			if err != nil {
				return err
			}

			fmt.Fprintf(out, "created at '%s'\n", confPath)
			return nil
		})

	f, _ := fr.ParseCommandline()
	ex(f.Do())
}
