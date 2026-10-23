package main

import (
	"context"
	"time"

	"github.com/aria-lopez/days-gone-by/logger"
	squel "github.com/aria-lopez/days-gone-by/squeal"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := squel.NewSquel(ctx)
	defer db.ByeBye()
	if err != nil {
		logger.ErrorF(err, "squel... couldn't connect :'(")
		return
	}
}
