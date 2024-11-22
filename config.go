package main

import (
	"time"

	"github.com/BurntSushi/toml"
	"github.com/diamondburned/arikawa/v3/discord"
)

type discordConfig struct {
	Token      string              `toml:"token"`
	Guild      discord.Snowflake   `toml:"server"`
	Channel    discord.Snowflake   `toml:"channel"`
	AdminRoles []discord.Snowflake `toml:"admin_roles"`
}

type binaryConfig struct {
	YTDLPath string `toml:"ytdlp"`
	MPVPath  string `toml:"mpv"`
}

type config struct {
	QueuePath        string        `toml:"queue_path"`
	UserLimit        int           `toml:"user_limit"`
	PlaybackTime     time.Duration `toml:"auto_play_delay"`
	DisablePing      bool          `toml:"disable_ping"`
	StartImmediately bool          `toml:"start_immediately"`
	Discord          discordConfig `toml:"discord"`
	Binary           binaryConfig  `toml:"binary"`
}

func (c *config) applyDefaults() {
	if c.QueuePath == "" {
		c.QueuePath = "queuedata"
	}
	if c.UserLimit == 0 {
		c.UserLimit = 1
	}
	if c.PlaybackTime == 0 {
		c.PlaybackTime = 30 * time.Second
	}
}

func loadConfig(path string) (cfg config, err error) {
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, err
	}
	cfg.applyDefaults()
	return
}
