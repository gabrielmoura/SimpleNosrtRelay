package config

import (
	"github.com/spf13/viper"
	"net/url"
)

var Cfg *Config

type BlossomConfig struct {
	Enabled      bool `mapstructure:"enabled"`
	AuthRequired bool `mapstructure:"auth_required"`
}
type StreamConfig struct {
	Relays  []string `mapstructure:"relays"`
	Enabled bool     `mapstructure:"enabled"`
}

func (sc *StreamConfig) Validate() bool {
	if sc.Enabled {
		if len(sc.Relays) == 0 {
			return false
		}
		for _, relay := range sc.Relays {
			u, err := url.Parse(relay)
			if err == nil || u.Scheme == "ws" || u.Scheme == "wss" {
				continue
			}
			return false
		}
	}
	return true
}

type Config struct {
	Info         *Info `mapstructure:"info"`
	Blossom      *BlossomConfig
	Stream       *StreamConfig
	AppEnv       string `mapstructure:"app_env"`
	BasePath     string `mapstructure:"base_path"`
	Negentropy   bool   `mapstructure:"negentropy"`
	AuthRequired bool   `mapstructure:"auth_required"`
}
type Info struct {
	Name        string `mapstructure:"name"`
	Description string `mapstructure:"description"`
	PubKey      string `mapstructure:"pub_key"`
	Contact     string `mapstructure:"contact"`
	Url         string `mapstructure:"url"`
	Icon        string `mapstructure:"icon"`
}

func InitConfig() error {
	viper.SetDefault("app_env", "production")
	viper.SetDefault("base_path", ".")
	viper.SetDefault("negentropy", true)
	viper.SetDefault("auth_required", false)

	viper.SetDefault("info.Name", "Nostr Relay Server")
	viper.SetDefault("info.Description", "Nostr Relay Server")
	viper.SetDefault("info.PubKey", "")
	viper.SetDefault("info.Contact", "")
	viper.SetDefault("info.Url", "http://localhost:3334")
	viper.SetDefault("info.Icon", "https://external-content.duckduckgo.com/iu/?u=https://public.bnbstatic.com/image/cms/crawler/COINCU_NEWS/image-495-1024x569.png")

	viper.SetDefault("blossom.enabled", true)
	viper.SetDefault("blossom.auth_required", false)
	viper.SetDefault("stream.enabled", false)

	viper.SetConfigName("nrs")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("../..")
	viper.AddConfigPath("/etc/nrs")
	if err := viper.ReadInConfig(); err != nil {
		return err
	}

	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return err
	}

	if cfg.AppEnv == "" {
		cfg.AppEnv = "production"
	}

	Cfg = cfg
	return nil
}
