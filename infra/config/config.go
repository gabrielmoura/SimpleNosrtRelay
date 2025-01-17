package config

import (
	"github.com/spf13/viper"
)

var Cfg *Config

type Config struct {
	AppEnv     string `mapstructure:"app_env"`
	Info       *Info  `mapstructure:"info"`
	BasePath   string `mapstructure:"base_path"`
	Negentropy bool   `mapstructure:"negentropy"`
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

	viper.SetDefault("info.Name", "Nostr Relay Server")
	viper.SetDefault("info.Description", "Nostr Relay Server")
	viper.SetDefault("info.PubKey", "")
	viper.SetDefault("info.Contact", "")
	viper.SetDefault("info.Url", "http://localhost:3334")
	viper.SetDefault("info.Icon", "https://external-content.duckduckgo.com/iu/?u=https://public.bnbstatic.com/image/cms/crawler/COINCU_NEWS/image-495-1024x569.png")

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
