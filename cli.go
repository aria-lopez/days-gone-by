package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/aria-lopez/days-gone-by/bouncer"
	"github.com/aria-lopez/days-gone-by/squeal"
)

// errUsage ;; the command line made no sense, main prints usage and exits 2
var errUsage = errors.New("usage")

const usage = `usage: days-gone-by <command>

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

// dispatch ;; picks the command.
func dispatch(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "init":
		if len(args) != 1 {
			return errUsage
		}
		return cmdInit(ctx)
	case "serve":
		if len(args) != 1 {
			return errUsage
		}
		return serve() // runs for as long as the server does, so no 60 second ctx
	case "key":
		return dispatchKey(ctx, args[1:])
	}
	return errUsage
}

func dispatchKey(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "new":
		return cmdKeyNew(ctx, args[1:])
	case "list":
		if len(args) != 1 {
			return errUsage
		}
		return cmdKeyList(ctx)
	case "revoke":
		if len(args) != 2 {
			return errUsage
		}
		return cmdKeyRevoke(ctx, args[1])
	}
	return errUsage
}

// openDB ;; what it says
func openDB(ctx context.Context, create bool) (*squeal.Squeal, error) {
	dp, err := defaultPath()
	if err != nil {
		return nil, err
	}
	if create {
		if err := os.MkdirAll(dp, 0o700); err != nil {
			return nil, err
		}
	} else if _, err := os.Stat(dbPath(dp)); err != nil {
		return nil, fmt.Errorf("no database in %s, run `days-gone-by init` first (is DAYS_GONE_BY_DIR right?)", dp)
	}
	return squeal.NewSqueal(ctx, dbPath(dp))
}

// mintKey ;; makes a key and stores its hash, the returned token is the only copy there will ever be
// so dont forget it (well actually its ok if you do)
func mintKey(ctx context.Context, db *squeal.Squeal, name string, role bouncer.Role) (token string, id string, err error) {
	token, hash, prefix := bouncer.NewKey()
	id, err = db.CreateKey(ctx, name, role.String(), prefix, hash)
	return token, id, err
}

// showKey ;; the key alone goes to stdout so KEY=$(days-gone-by init) works
func showKey(stderr io.Writer, token string, id string, name string, role bouncer.Role) {
	fmt.Fprintf(stderr, "created %s key %q (id %s)\n", role, name, id)
	fmt.Fprintln(stderr, "save it now, it is not stored anywhere and can never be shown again:")
	fmt.Println(token)
	fmt.Fprintln(stderr, "\nuse it like:  curl -H \"Authorization: Bearer <key>\" http://<addr>/api/v1/images/<id>")
}

func cmdInit(ctx context.Context) error {
	db, err := openDB(ctx, true)
	if err != nil {
		return err
	}
	defer db.ByeBye()

	_, admins, err := db.CountActiveKeys(ctx)
	if err != nil {
		return err
	}
	if admins > 0 {
		return fmt.Errorf("already initialised, there are %d active admin key(s). `days-gone-by key new` makes more", admins)
	}
	token, id, err := mintKey(ctx, db, "initial admin", bouncer.RoleAdmin)
	if err != nil {
		return err
	}
	showKey(os.Stderr, token, id, "initial admin", bouncer.RoleAdmin)
	return nil
}

func cmdKeyNew(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("key new", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	roleName := fs.String("role", "member", "")

	// the flag may come before or after the name: key new ci --role admin, or key new --role admin ci
	var name string
	for rest := args; len(rest) > 0; {
		if err := fs.Parse(rest); err != nil {
			return errUsage
		}
		if fs.NArg() == 0 {
			break
		}
		if name != "" {
			return errUsage // two names
		}
		name = fs.Arg(0)
		rest = fs.Args()[1:]
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return errors.New("a key needs a name, up to 100 characters, so you can tell who is using it later")
	}
	role, ok := bouncer.ParseRole(*roleName)
	if !ok {
		return fmt.Errorf("unknown role %q, use viewer, member or admin", *roleName)
	}

	db, err := openDB(ctx, false)
	if err != nil {
		return err
	}
	defer db.ByeBye()

	token, id, err := mintKey(ctx, db, name, role)
	if err != nil {
		return err
	}
	showKey(os.Stderr, token, id, name, role)
	return nil
}

func cmdKeyList(ctx context.Context) error {
	db, err := openDB(ctx, false)
	if err != nil {
		return err
	}
	defer db.ByeBye()

	keys, err := db.ListKeys(ctx)
	if err != nil {
		return err
	}
	// time ;; unix ms from the db to something readable, "-" for never
	when := func(ms *int64) string {
		if ms == nil {
			return "-"
		}
		return time.UnixMilli(*ms).Local().Format("2006-01-02 15:04")
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tROLE\tKEY\tCREATED\tLAST USED\tSTATUS")
	for _, k := range keys {
		created := k.CreatedAt
		status := "active"
		if k.RevokedAt != nil {
			status = "revoked " + when(k.RevokedAt)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s...\t%s\t%s\t%s\n", k.ID, k.Name, k.Role, k.Prefix, when(&created), when(k.LastUsedAt), status)
	}
	return tw.Flush()
}

func cmdKeyRevoke(ctx context.Context, id string) error {
	db, err := openDB(ctx, false)
	if err != nil {
		return err
	}
	defer db.ByeBye()

	err = db.RevokeKey(ctx, id)
	if errors.Is(err, squeal.ErrNotFound) {
		return fmt.Errorf("no active key with id %q, `days-gone-by key list` shows them", id)
	}
	if errors.Is(err, squeal.ErrLastAdmin) {
		return errors.New("that is the last active admin key, make another with `days-gone-by key new --role admin NAME` first")
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "revoked key %s, it stops working immediately\n", id)
	return nil
}
