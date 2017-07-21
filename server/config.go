package server

import (
	"bytes"
	"io"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/containerd/containerd/log"
	"golang.org/x/net/context"
)

// Config provides containerd configuration data for the server
type Config struct {
	// Root is the path to a directory where containerd will store persistent data
	Root string `toml:"root"`
	// GRPC configuration settings
	GRPC GRPCConfig `toml:"grpc"`
	// Debug and profiling settings
	Debug Debug `toml:"debug"`
	// Metrics and monitoring settings
	Metrics MetricsConfig `toml:"metrics"`
	// Plugins provides plugin specific configuration for the initialization of a plugin
	Plugins map[string]toml.Primitive `toml:"plugins"`
	// Enable containerd as a subreaper
	Subreaper bool `toml:"subreaper"`
	// OOMScore adjust the containerd's oom score
	OOMScore int `toml:"oom_score"`
	// LogHooks provides log hook settings
	LogHooks map[string]toml.Primitive `toml:"loghooks"`

	md toml.MetaData
}

type GRPCConfig struct {
	Address string `toml:"address"`
	Uid     int    `toml:"uid"`
	Gid     int    `toml:"gid"`
}

type Debug struct {
	Address string `toml:"address"`
	Uid     int    `toml:"uid"`
	Gid     int    `toml:"gid"`
	Level   string `toml:"level"`
}

type MetricsConfig struct {
	Address string `toml:"address"`
}

// Decode unmarshals a plugin specific configuration by plugin id
func (c *Config) Decode(id string, v interface{}) (interface{}, error) {
	data, ok := c.Plugins[id]
	if !ok {
		return v, nil
	}
	if err := c.md.PrimitiveDecode(data, v); err != nil {
		return nil, err
	}
	return v, nil
}

// WriteTo marshals the config to the provided writer
func (c *Config) WriteTo(w io.Writer) (int64, error) {
	buf := bytes.NewBuffer(nil)
	e := toml.NewEncoder(buf)
	if err := e.Encode(c); err != nil {
		return 0, err
	}
	return io.Copy(w, buf)
}

// LoadConfig loads the containerd server config from the provided path
func LoadConfig(path string, v *Config) error {
	if v == nil {
		v = &Config{}
	}
	md, err := toml.DecodeFile(path, v)
	if err != nil {
		return err
	}
	v.md = md
	return nil

}

func LoadLogHooksConfig(ctx context.Context, v *Config) error {
	data, ok := v.LogHooks["sendmail"]
	if ok {
		var h log.MailHook
		err := v.md.PrimitiveDecode(data, &h)
		if err != nil {
			return err
		}
		log.G(ctx).Logger.Hooks.Add(&h)
	}

	data, ok = v.LogHooks["splitstderr"]
	if ok {
		var h log.StderrHook
		err := v.md.PrimitiveDecode(data, &h)
		if err != nil {
			return err
		}
		log.G(ctx).Logger.Out = os.Stdout
		log.G(ctx).Logger.Hooks.Add(&h)
	}
	return nil
}
