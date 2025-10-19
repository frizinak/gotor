package flags

import (
	"flag"
	"fmt"
	"io"
	"os"
)

type Set struct {
	w           io.Writer
	f           *flag.FlagSet
	name        string
	description string
	help        func(io.Writer)
	parent      *Set
	order       []string
	children    map[string]*Set
	aliases     map[string]string
	handler     Handler
}

type Definer func(*flag.FlagSet)
type Handler func(f *Set, args []string) error

func New(f *flag.FlagSet, output io.Writer) *Set {
	f.SetOutput(output)
	s := &Set{
		w:        output,
		f:        f,
		name:     f.Name(),
		children: make(map[string]*Set),
		aliases:  make(map[string]string),
	}
	s.parent = s

	s.f.Usage = func() {
		if s.description != "" {
			fmt.Fprintln(s.w, s.description)
		}

		haveFlags := false
		haveUsage := s.help != nil
		haveCmds := len(s.order) != 0

		// TODO make a testcase for this, fragile.
		s.f.VisitAll(func(*flag.Flag) { haveFlags = true })

		if !haveUsage && !haveFlags && !haveCmds {
			return
		}

		if s.description != "" {
			fmt.Fprintln(s.w)
		}
		fmt.Fprintf(s.w, "Usage of %s:\n", s.name)
		if haveUsage {
			s.help(s.w)
		}
		if haveFlags {
			fmt.Fprintln(s.w, "[flags]")
			s.f.PrintDefaults()
		}
		if haveCmds {
			fmt.Fprintln(s.w, "[commands]")
			ml := 0
			for _, cmd := range s.order {
				if n := len(cmd); n > ml {
					ml = n
				}
			}
			format := fmt.Sprintf("  - %%-%ds %%s\n", ml+2)
			for _, cmd := range s.order {
				fmt.Fprintf(s.w, format, cmd, s.children[cmd].description)
			}
		}
	}

	return s
}

func NewRoot(output io.Writer) *Set {
	return New(flag.CommandLine, output)
}

func (f *Set) Name() string { return f.name }
func (f *Set) Parent() *Set { return f.parent }

func (f *Set) Define(definer Definer) *Set {
	definer(f.f)
	return f
}

func (f *Set) Handler(h Handler) *Set { f.handler = h; return f }

func (f *Set) Add(name string, aliases ...string) *Set {
	for _, a := range aliases {
		f.aliases[a] = name
	}
	if n, ok := f.children[name]; ok {
		return n
	}

	fname := f.name + " " + name
	rf := flag.NewFlagSet(fname, flag.ExitOnError)
	rf.SetOutput(f.w)
	n := New(rf, f.w)
	f.children[name] = n
	n.parent = f
	f.order = append(f.order, name)

	return n
}

func (f *Set) Description(descr string) *Set {
	f.description = descr
	return f
}

func (f *Set) Help(h func(io.Writer)) *Set {
	f.help = h
	return f
}

func (f *Set) Usage(ex int) {
	f.f.Usage()
	os.Exit(ex)
}

func (f *Set) Args() []string { return f.f.Args() }

func (f *Set) ParseCommandline() (sub *Set, trail []string) {
	return f.Parse(os.Args[1:])
}

func (f *Set) Parse(args []string) (sub *Set, trail []string) {
	sub, trail = f.parse(args, make([]string, 0))
	return sub, trail
}

func (f *Set) parse(args, trail []string) (*Set, []string) {
	f.f.Parse(args)
	cmds := f.f.Args()
	if len(cmds) == 0 {
		if f.handler == nil {
			f.Usage(1)
		}

		return f, trail
	}

	cmd := cmds[0]
	if _cmd, ok := f.aliases[cmd]; ok {
		cmd = _cmd
	}

	if sub, ok := f.children[cmd]; ok {
		return sub.parse(cmds[1:], append(trail, cmd))
	}

	if f.handler == nil {
		f.Usage(1)
	}

	return f, trail
}

func (f *Set) Do() error {
	return f.handler(f, f.Args())
}
