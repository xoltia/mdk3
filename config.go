package main

import (
	"fmt"
	"reflect"
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
	if c.Binary.YTDLPath == "" {
		c.Binary.YTDLPath = "yt-dlp"
	}

	if c.Binary.MPVPath == "" {
		c.Binary.MPVPath = "mpv"
	}
}

type validationErrors []error

func (e validationErrors) Error() string {
	var errs string
	for _, err := range e {
		errs += err.Error() + "\n"
	}
	return errs
}

type validationError struct {
	field  string
	reason string
}

func (e validationError) Error() string {
	return fmt.Sprintf("%s: %s", e.field, e.reason)
}

func requireNotZeroValue(field string, value any) error {
	if reflect.ValueOf(value).IsZero() {
		return validationError{field, "not set"}
	}
	return nil
}

func requireValidSnowflake(field string, value discord.Snowflake) error {
	if !value.IsValid() {
		return validationError{field, "invalid snowflake"}
	}
	return nil
}

func (c *config) validate() error {
	errs := make(validationErrors, 0)
	errs = append(errs, requireNotZeroValue("queue_path", c.QueuePath))
	errs = append(errs, requireNotZeroValue("discord.token", c.Discord.Token))

	discordServerIDMissingErr := requireNotZeroValue("discord.server", c.Discord.Guild)
	if discordServerIDMissingErr == nil {
		errs = append(errs, requireValidSnowflake("discord.server", c.Discord.Guild))
	} else {
		errs = append(errs, discordServerIDMissingErr)
	}

	discordChannelIDMissingErr := requireNotZeroValue("discord.channel", c.Discord.Channel)
	if discordChannelIDMissingErr == nil {
		errs = append(errs, requireValidSnowflake("discord.channel", c.Discord.Channel))
	} else {
		errs = append(errs, discordChannelIDMissingErr)
	}

	errs = append(errs, requireNotZeroValue("binary.ytdlp", c.Binary.YTDLPath))
	errs = append(errs, requireNotZeroValue("binary.mpv", c.Binary.MPVPath))
	for i, role := range c.Discord.AdminRoles {
		errs = append(errs, requireValidSnowflake(fmt.Sprintf("discord.admin_roles[%d]", i), role))
	}

	var filtered validationErrors
	for _, err := range errs {
		if err != nil {
			filtered = append(filtered, err)
		}
	}

	if len(filtered) > 0 {
		return filtered
	}

	return nil
}

func loadConfig(path string) (cfg config, err error) {
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, err
	}
	cfg.applyDefaults()
	err = cfg.validate()
	return cfg, err
}
