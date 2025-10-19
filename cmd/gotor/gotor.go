package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frizinak/gotor/api"
	_ "github.com/frizinak/gotor/api/transmission"
	"github.com/frizinak/gotor/flags"
)

type cmdFlags struct {
	config  string
	noColor bool
}

func (f cmdFlags) Parse(uc userConfig, o io.Writer) cmdConfig {
	return cmdConfig{color: !f.noColor, output: o}
}

type cmdConfig struct {
	color  bool
	output io.Writer
}

type downloadDirFlags struct{}

func (f downloadDirFlags) Parse(uc userConfig, o io.Writer) downloadDirConfig {
	return downloadDirConfig{strings.TrimRight(uc.DownloadDirectory, "/\\")}
}

type downloadDirConfig struct {
	downloadDirectory string
}

type userConfig struct {
	TLS               bool        `json:"tls"`
	Host              string      `json:"host"`
	Port              interface{} `json:"port"`
	User              string      `json:"username"`
	Password          string      `json:"password"`
	RPC               string      `json:"rpc-path"`
	DownloadDirectory string      `json:"download-dir"`
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
	dec := json.NewDecoder(f)
	err = dec.Decode(&c)
	f.Close()
	return c, err
}

func saveUserConfig(path string, c userConfig) error {
	c.Port = fmt.Sprintf("%v", c.Port)

	tmp := path + "." + time.Now().Format("150405.999999999") + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "    ")
	err = enc.Encode(c)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, path)
}

func main() {
	// TODO global stats (limits, ...)
	// TODO cache downloaddir? and the above... perhaps
	// TODO sorts
	// TODO info
	// TODO move
	// TODO verify
	// TODO remove

	me := os.Args[0]
	out := os.Stdout

	var defaultConfPath string
	client := func(flags cmdFlags) (api.Client, userConfig, error) {
		confPath := defaultConfPath
		if flags.config != "" {
			confPath = flags.config
		}
		conf, err := loadUserConfig(confPath)
		if os.IsNotExist(err) {
			return nil, conf, fmt.Errorf(
				"No config file found at '%s'.\nYou can use the `%s config create` to create one",
				confPath,
				me,
			)
		}
		if err != nil {
			return nil, conf, err
		}

		c, err := api.GetClient("transmission", conf.URL())
		return c, conf, err
	}

	flagsDefault := func(f *flag.FlagSet, flags *cmdFlags) {
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
		defaultConfPath = filepath.Join(dir, "gotor", "config.json")

		f.StringVar(&flags.config, "c", defaultConfPath, "path to the config file")
	}

	flagsColor := func(f *flag.FlagSet, flags *cmdFlags) {
		f.BoolVar(&flags.noColor, "C", false, "Disable colors")
	}

	flagsFilters := func(f *flag.FlagSet, flags *filterFlags) {
		f.StringVar(&flags.added[0], "added-since", "",
			`Filter added since this date.
Format "YYYY-MM-DD hh:mm:ss"
where either the date or the time and/or the seconds / year can be omitted.`)
		f.StringVar(&flags.added[1], "added-until", "",
			`Filter added before this date. See -added-since.`)
		f.StringVar(&flags.updated[0], "updated-since", "",
			`Filter updated since this date. See -added-since.`)
		f.StringVar(&flags.updated[1], "updated-until", "",
			`Filter updtaed until this date. See -added-since.`)

		f.Var(&flags.status, "s",
			`Filter statuses.
Use any of stall, stop, queue, down, seed, done, check, have, meta and error.
Match multiple statuses by separating them with a comma. Negate with ^ or !.
Specify this flag multiple times to filter multiple statuses.`)
		f.StringVar(&flags.id, "i", "",
			`Filter id.
Separate multiple values with a comma and specify ranges with a dash`)
		f.StringVar(&flags.path, "p", "",
			`Filter paths.
Separete with a comma to match multiple.`)
		f.StringVar(&flags.name, "n", "", "Filter names with a perl regex")
	}

	var listFlags listFlags
	defaultDefine := func(f *flag.FlagSet) {
		c := &listFlags.cmdFlags
		flagsDefault(f, c)
		flagsColor(f, c)
		flagsFilters(f, &listFlags.filters)

		f.BoolVar(&listFlags.hideErrors, "E", false, "Hide error messages")
		f.Float64Var(&listFlags.watch, "w", 0, "Continuously query at the given interval in seconds")
	}
	defaultHandler := func(set *flags.Set, args []string) error {
		if len(args) != 0 {
			set.Usage(1)
		}

		c, userConf, err := client(listFlags.cmdFlags)
		if err != nil {
			return err
		}
		conf, err := listFlags.Parse(userConf, out)
		if err != nil {
			return err
		}
		return cmdList(context.Background(), conf, c)
	}

	fr := flags.NewRoot(out).Define(defaultDefine).Handler(defaultHandler)

	fr.Add("list").Description("list torrents").
		Define(defaultDefine).
		Handler(defaultHandler)

	var detailFlags detailFlags
	fr.Add("info").Description("get torrent details").
		Help(func(w io.Writer) {
			fmt.Fprintln(w, "- argument 1: the torrent id")
		}).
		Define(func(f *flag.FlagSet) {
			c := &detailFlags.cmdFlags
			flagsDefault(f, c)
			flagsColor(f, c)
			f.BoolVar(&detailFlags.verbose, "v", false, "Be verbose")
		}).
		Handler(func(set *flags.Set, args []string) error {
			if len(args) != 1 {
				set.Usage(1)
			}

			c, userConf, err := client(detailFlags.cmdFlags)
			if err != nil {
				return err
			}
			conf := detailFlags.Parse(userConf, out)

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
		})

	var removeFlags removeFlags
	fr.Add("remove", "delete").Description("remove torrents").
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, &removeFlags.cmdFlags)
			flagsFilters(f, &removeFlags.filterFlags)
			f.BoolVar(&removeFlags.deleteData, "delete", false, "also delete data")
			f.BoolVar(&removeFlags.yes, "yes", false, "answer yes to all dheletion prompts")
		}).
		Handler(func(set *flags.Set, args []string) error {
			if len(args) != 0 {
				set.Usage(1)
			}

			c, userConf, err := client(removeFlags.cmdFlags)
			if err != nil {
				return err
			}

			conf, err := removeFlags.Parse(userConf, out)
			if err != nil {
				return err
			}
			conf.prompter = prompter{output: out}

			return cmdRemove(
				context.Background(),
				conf,
				c,
			)
		})

	var statsFlags statsFlags
	fr.Add("stats").Description("monitor stats").
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, &statsFlags.cmdFlags)
			f.Float64Var(&statsFlags.watch, "w", 0, "Continuously query at the given interval in seconds")
		}).
		Handler(func(set *flags.Set, args []string) error {
			if len(args) != 0 {
				set.Usage(1)
			}

			c, userConf, err := client(statsFlags.cmdFlags)
			if err != nil {
				return err
			}
			conf := statsFlags.Parse(userConf, out)
			return cmdStats(context.Background(), conf, c)
		})

	var addFlags addFlags
	fr.Add("add").Description("add torrents").
		Help(func(w io.Writer) {
			fmt.Fprintln(w, "- argument 1:   the relative destination / label.")
			fmt.Fprintln(w, "- argument 2-n: torrent files or magnet URIs.")
		}).
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, &addFlags.cmdFlags)
		}).
		Handler(func(set *flags.Set, args []string) error {
			if len(args) < 2 {
				set.Usage(1)
			}

			c, userConf, err := client(addFlags.cmdFlags)
			if err != nil {
				return err
			}

			conf := addFlags.Parse(userConf, out)

			cat := args[0]
			if strings.HasPrefix(cat, "magnet:") {
				return fmt.Errorf("first argument looks like a magnet URI")
			}
			if strings.HasSuffix(cat, ".torrent") {
				if stat, _ := os.Stat(cat); stat != nil && !stat.IsDir() {
					return fmt.Errorf("first argument looks like a torrent file")
				}
			}
			if cat != "/" {
				cat = strings.Trim(args[0], "/\\")
			}

			conf.downloadPath = cat
			conf.labels = []string{cat, "gotor"}

			return cmdAdd(context.Background(), conf, c, args[1:])
		})

	var configCreateFlags cmdFlags
	fr.Add("config").Add("create").Description("create default config file").
		Define(func(f *flag.FlagSet) {
			flagsDefault(f, &configCreateFlags)
		}).
		Handler(func(set *flags.Set, args []string) error {
			if len(args) != 0 {
				set.Usage(1)
			}

			confPath := defaultConfPath
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
			os.MkdirAll(dir, 0750)

			c := userConfig{
				Host:     "localhost",
				Port:     "9091",
				User:     "",
				Password: "",
				RPC:      "/transmission/rpc",
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
