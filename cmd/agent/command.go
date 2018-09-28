package agent

import (
	"bytes"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/huton-io/huton/pkg"
	"github.com/mitchellh/cli"
)

const (
	synopsis = "Runs a Huton agent"
	help     = `
	Usage: huton agent [options]

	Starts a Huton agent.
`
)

// New returns a new agent command.
func New(ui cli.Ui) cli.Command {
	c := &cmd{UI: ui}
	c.init()
	return c
}

type cmd struct {
	UI       cli.Ui
	help     string
	conf     *config
	flagSet  *flag.FlagSet
	instance *huton.Instance
}

func (c *cmd) Run(args []string) int {
	if err := c.flagSet.Parse(args); err != nil {
		c.UI.Error(err.Error())
		return 1
	}
	return c.run()
}

func (c *cmd) Synopsis() string {
	return synopsis
}

func (c *cmd) Help() string {
	return c.help
}

func (c *cmd) run() int {
	hutonConfig, err := c.conf.parse()
	if err != nil {
		c.UI.Error(fmt.Sprintf("failed to parse huton config: %s", err))
		return 1
	}
	c.instance, err = huton.NewInstance(hutonConfig)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}
	http.HandleFunc("/", handler(c.instance))
	go http.ListenAndServe(c.conf.http, nil)
	return c.handleSignals()
	// return 0
}

func (c *cmd) init() {
	c.flagSet = flag.NewFlagSet("agent", flag.ContinueOnError)
	c.conf = addFlags(c.flagSet)
	var buf bytes.Buffer
	c.flagSet.SetOutput(&buf)
	c.flagSet.Usage()
	c.help = buf.String()
}

func (c *cmd) handleSignals() int {
	signalCh := make(chan os.Signal, 3)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	select {
	case <-signalCh:
		if err := c.instance.Shutdown(); err != nil {
			return 2
		}
		return 0
	}
}

func handler(ins *huton.Instance) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cache, err := ins.Cache("foo")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			b, err := cache.Get([]byte("foo"))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Write(b)
		case http.MethodPost, http.MethodPut:
			fmt.Println("getting cache")
			cache, err := ins.Cache("foo")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			fmt.Println("setting")
			if err := cache.Set([]byte("foo"), []byte("bar")); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Write([]byte("OK"))
		}
	}
}
