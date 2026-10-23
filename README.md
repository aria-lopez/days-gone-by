# days-gone-by

Durable document archive, easy to self host, comes with a UI and some search tools~

Feel free to fork and change it or suggest PR's.

## System requirements

Currently support is for Linux/MacOS only.

## Installing

## CLI Spec:

`days-gone-by`

```

usage: days-gone-by <command>

commands:
  init                          create the data dir and database, and the first admin API key
  serve                         run the server
  key new [--role R] NAME       create an API key, R is viewer, member (default) or admin
  key list                      list API keys
  key revoke ID                 revoke an API key

environment:
  DAYS_GONE_BY_DIR              where the data lives
  DAYS_GONE_BY_SERVER_ADDR      address to listen on, default 127.0.0.1:8080
  DAYS_GONE_BY_MAX_UPLOAD       biggest upload in bytes, default 50 MiB
`
```

## API Spec
