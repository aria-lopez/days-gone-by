// Package config ;; settings from built in defaults, then config.json, then DAYS_GONE_BY_* env vars
package config

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aria-lopez/days-gone-by/sniffer"
	"github.com/dustin/go-humanize"
)

// Duration ;; written as "30s", "10m", "2h"
type Duration time.Duration

func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("%q is not a duration, use things like 30s, 10m or 2h", b)
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

func (d Duration) String() string { return tidy(time.Duration(d).String()) }

// tidy ;; "10m0s" to "10m", "2h0m0s" to "2h"
func tidy(s string) string {
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}

// Size ;; bytes, written as "500KiB", "100MiB", "4GiB" or a plain number of bytes
type Size int64

func (s *Size) UnmarshalText(b []byte) error {
	n, err := humanize.ParseBytes(string(b))
	if err != nil || n > math.MaxInt64 {
		return fmt.Errorf("%q is not a size, use things like 500KiB, 100MiB or 4GiB", b)
	}
	*s = Size(n)
	return nil
}

func (s Size) MarshalText() ([]byte, error) {
	for _, u := range []struct {
		n    int64
		name string
	}{{1 << 30, "GiB"}, {1 << 20, "MiB"}, {1 << 10, "KiB"}} {
		if s != 0 && int64(s)%u.n == 0 {
			return []byte(strconv.FormatInt(int64(s)/u.n, 10) + u.name), nil
		}
	}
	return []byte(strconv.FormatInt(int64(s), 10)), nil
}

func (s Size) String() string {
	b, _ := s.MarshalText()
	return string(b)
}

// Limit ;; what one kind of file is held to
type Limit struct {
	MaxSize Size     `json:"max_size"` // ;; biggest file
	Timeout Duration `json:"timeout"`  // ;; longest the whole upload may take
}

// Limits ;; one per sniffer.Kind. a struct rather than a map so a file can set just one field of a kind
type Limits struct {
	Image    Limit `json:"image"`
	Video    Limit `json:"video"`
	Audio    Limit `json:"audio"`
	Document Limit `json:"document"`
	Archive  Limit `json:"archive"`
}

func (l *Limits) of(k sniffer.Kind) *Limit {
	switch k {
	case sniffer.Image:
		return &l.Image
	case sniffer.Video:
		return &l.Video
	case sniffer.Audio:
		return &l.Audio
	case sniffer.Document:
		return &l.Document
	case sniffer.Archive:
		return &l.Archive
	}
	return nil
}

type Maintenance struct {
	PurgeEvery   Duration `json:"purge_every"`    // ;; how often unused blobs get checked for removal
	OrphanEvery  Duration `json:"orphan_every"`   // ;; how often we walk every file looking for orphans
	OrphanMinAge Duration `json:"orphan_min_age"` // ;; a file younger than this is never an orphan
}

type Config struct {
	ServerAddr  string      `json:"server_addr"`  // ;; [ip]:[port]
	IdleTimeout Duration    `json:"idle_timeout"` // ;; an upload that sends nothing for this long is cut off
	Limits      Limits      `json:"limits"`
	Maintenance Maintenance `json:"maintenance"`
}

const (
	maxLimitSize    = 1 << 40
	maxLimitTimeout = 24 * time.Hour
)

func Default() Config {
	return Config{
		ServerAddr:  "127.0.0.1:8080",
		IdleTimeout: Duration(30 * time.Second),
		Limits: Limits{
			Image:    Limit{MaxSize: 100 << 20, Timeout: Duration(10 * time.Minute)},
			Video:    Limit{MaxSize: 4 << 30, Timeout: Duration(2 * time.Hour)},
			Audio:    Limit{MaxSize: 1 << 30, Timeout: Duration(30 * time.Minute)},
			Document: Limit{MaxSize: 100 << 20, Timeout: Duration(10 * time.Minute)},
			Archive:  Limit{MaxSize: 1 << 30, Timeout: Duration(time.Hour)},
		},
		Maintenance: Maintenance{
			PurgeEvery:   Duration(10 * time.Minute),
			OrphanEvery:  Duration(24 * time.Hour),
			OrphanMinAge: Duration(24 * time.Hour),
		},
	}
}

// Limit ;; the limit for a kind that sniffer.KindOf gave back
func (c Config) Limit(k sniffer.Kind) Limit { return *c.Limits.of(k) }

// Largest ;; the biggest size and longest timeout of any kind, what an upload is held to until its kind is known
func (c Config) Largest() Limit {
	var top Limit
	for _, k := range sniffer.Kinds {
		l := c.Limits.of(k)
		top.MaxSize = max(top.MaxSize, l.MaxSize)
		top.Timeout = max(top.Timeout, l.Timeout)
	}
	return top
}

// Load ;; defaults, then the file, then env. the file is DAYS_GONE_BY_CONFIG if set (and must exist),
// otherwise <dir>/config.json if there is one
func Load(dir string) (Config, error) {
	c := Default()

	path, required := os.LookupEnv("DAYS_GONE_BY_CONFIG")
	if path == "" {
		path, required = filepath.Join(dir, "config.json"), false
	}
	if err := c.readFile(path, required); err != nil {
		return Config{}, err
	}
	if err := c.applyEnv(os.Getenv); err != nil {
		return Config{}, err
	}
	if err := c.validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (c *Config) readFile(path string, required bool) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) && !required {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields() // a typo should stop startup, not quietly keep the default
	// an empty file is fine, it changes nothing
	if err := dec.Decode(c); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", path, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("%s: unexpected data after the config", path)
	}
	return nil
}

// applyEnv ;; DAYS_GONE_BY_<NAME> beats the file, names are the json keys in capitals, a kind first: VIDEO_MAX_SIZE
func (c *Config) applyEnv(getenv func(string) string) error {
	if getenv("DAYS_GONE_BY_MAX_UPLOAD") != "" {
		return errors.New("DAYS_GONE_BY_MAX_UPLOAD is gone, limits are per kind now: DAYS_GONE_BY_IMAGE_MAX_SIZE and so on")
	}

	vars := []envVar{
		{"SERVER_ADDR", func(s string) error { c.ServerAddr = s; return nil }},
		{"IDLE_TIMEOUT", text(&c.IdleTimeout)},
		{"PURGE_EVERY", text(&c.Maintenance.PurgeEvery)},
		{"ORPHAN_EVERY", text(&c.Maintenance.OrphanEvery)},
		{"ORPHAN_MIN_AGE", text(&c.Maintenance.OrphanMinAge)},
	}
	for _, k := range sniffer.Kinds {
		l, name := c.Limits.of(k), strings.ToUpper(string(k))
		vars = append(vars, envVar{name + "_MAX_SIZE", text(&l.MaxSize)}, envVar{name + "_TIMEOUT", text(&l.Timeout)})
	}

	for _, v := range vars {
		if s := getenv("DAYS_GONE_BY_" + v.name); s != "" {
			if err := v.set(s); err != nil {
				return fmt.Errorf("bad DAYS_GONE_BY_%s: %w", v.name, err)
			}
		}
	}
	return nil
}

type envVar struct {
	name string
	set  func(string) error
}

func text(dst encoding.TextUnmarshaler) func(string) error {
	return func(s string) error { return dst.UnmarshalText([]byte(s)) }
}

func (c *Config) validate() error {
	if _, _, err := net.SplitHostPort(c.ServerAddr); err != nil {
		return fmt.Errorf("server_addr %q: %w", c.ServerAddr, err)
	}
	for _, d := range []struct {
		name string
		v    Duration
	}{
		{"idle_timeout", c.IdleTimeout},
		{"maintenance.purge_every", c.Maintenance.PurgeEvery},
		{"maintenance.orphan_every", c.Maintenance.OrphanEvery},
		{"maintenance.orphan_min_age", c.Maintenance.OrphanMinAge},
	} {
		if d.v <= 0 {
			return fmt.Errorf("%s must be above 0", d.name)
		}
	}
	for _, k := range sniffer.Kinds {
		l := c.Limits.of(k)
		if l.MaxSize <= 0 || l.MaxSize > maxLimitSize {
			return fmt.Errorf("limits.%s.max_size must be above 0 and at most %s, got %s", k, Size(maxLimitSize), l.MaxSize)
		}
		if l.Timeout <= 0 || l.Timeout.Std() > maxLimitTimeout {
			return fmt.Errorf("limits.%s.timeout must be above 0 and at most %s, got %s", k, Duration(maxLimitTimeout), l.Timeout)
		}
	}
	return nil
}
