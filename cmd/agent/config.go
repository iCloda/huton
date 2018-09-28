package agent

import (
	"crypto/x509"
	"encoding/base64"
	"flag"
	"fmt"
	"io/ioutil"

	"github.com/huton-io/huton/cmd/flags"
	"github.com/huton-io/huton/pkg"
)

type config struct {
	http            string
	bindAddr        string
	bindPort        int
	bootstrap       bool
	bootstrapExpect int
	encryptionKey   string
	certFile        string
	keyFile         string
	caFile          string
	peers           []string
}

func (c config) parse() (huton.Config, error) {
	hutonConfig := huton.Config{
		BindHost:  c.bindAddr,
		BindPort:  c.bindPort,
		Bootstrap: c.bootstrap,
		Expect:    c.bootstrapExpect,
	}
	if c.encryptionKey != "" {
		b, err := base64.StdEncoding.DecodeString(c.encryptionKey)
		if err != nil {
			return hutonConfig, err
		}
		hutonConfig.SerfEncryptionKey = b
	}
	return hutonConfig, nil
}

func addFlags(fs *flag.FlagSet) *config {
	var c config
	fs.StringVar(&c.bindAddr, "bindAddr", "127.0.0.1", "address to bind serf to")
	fs.StringVar(&c.http, "http", "127.0.0.1:8080", "http address")
	fs.IntVar(&c.bindPort, "bindPort", -1, "port to bind serf to")
	fs.BoolVar(&c.bootstrap, "bootstrap", false, "bootstrap mode")
	fs.IntVar(&c.bootstrapExpect, "expect", 3, "bootstrap expect")
	fs.StringVar(&c.encryptionKey, "encrypt", "", "base64 encoded encryption key")
	fs.Var((*flags.StringSlice)(&c.peers), "peers", "peer list")
	return &c
}

func loadCAFile(cacert string) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	pem, err := ioutil.ReadFile(cacert)
	if err != nil {
		return nil, fmt.Errorf("Failed reading CA file: %s", err)
	}
	if ok := pool.AppendCertsFromPEM(pem); !ok {
		return nil, fmt.Errorf("Failed to parse PEM for CA cert: %s", cacert)
	}
	return pool, nil
}
