package apeye

import (
	"context"
	"errors"
	"time"

	"github.com/aria-lopez/days-gone-by/blobby"
	"github.com/aria-lopez/days-gone-by/logger"
	"github.com/aria-lopez/days-gone-by/squeal"
)

// FIXME: [config]
const (
	purgeEvery   = 10 * time.Minute // ;; how often unused blobs get checked for removal
	orphanEvery  = 24 * time.Hour   // ;; how often we walk every file looking for orphans
	orphanMinAge = 24 * time.Hour   // ;; a file younger than this is never an orphan (so we dont scan new stuff)
	chunkSize    = 25               // ;; blobs handled per lock hold so uploads dont wait on a long sweep
)

// Maintain ;; general cleanup for oprhaned files or blobs to be removed
func (a *API) Maintain(ctx context.Context) {
	t := time.NewTicker(purgeEvery)
	defer t.Stop()

	var lastOrphans time.Time // zero so the first loop does it too
	for {
		a.sweep(ctx)
		// a sweep that didnt finish (uploads in flight, error) doesnt count, it runs again next tick
		if time.Since(lastOrphans) >= orphanEvery && a.sweepOrphans(ctx) {
			lastOrphans = time.Now()
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// sweep ;; delete blobs that dont have a live record, in chunks.
func (a *API) sweep(ctx context.Context) {
	shas, err := a.squeal.PurgeCandidates(ctx)
	if err != nil {
		if ctx.Err() == nil {
			logger.ErrorF(err, "sweep failed during Squeal.PurgeCandidates")
		}
		return
	}
	for len(shas) > 0 {
		n := min(chunkSize, len(shas))
		ran, err := a.purge(ctx, shas[:n])
		if err != nil {
			if ctx.Err() == nil {
				logger.ErrorF(err, "sweep failed during purge")
			}
			return
		}
		if !ran {
			logger.InfoF("sweep skipped, uploads in flight, will try again next time")
			return
		}
		shas = shas[n:]
	}
}

// sweepOrphans ;; moves files nobody owns into quarantine/, never deletes them.
func (a *API) sweepOrphans(ctx context.Context) bool {
	// 1. look without the lock, this walks every file so it can take a while
	cutoff := time.Now().Add(-orphanMinAge)
	var orphans []string
	err := a.store.Walk(func(sha string, modTime time.Time) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if modTime.After(cutoff) {
			return nil
		}
		// BlobMime says ErrNotFound for both a missing and a purged row
		_, err := a.squeal.BlobMime(ctx, sha)
		if errors.Is(err, squeal.ErrNotFound) {
			orphans = append(orphans, sha)
			return nil
		}
		return err
	})
	if err != nil {
		if ctx.Err() == nil {
			logger.ErrorF(err, "orphan sweep failed during Blobby.Walk")
		}
		return false
	}

	// 2. act on them under the lock in chunks, checking each one again
	moved := 0
	for len(orphans) > 0 {
		n := min(chunkSize, len(orphans))
		ran, count, err := a.quarantine(ctx, orphans[:n])
		moved += count
		if err != nil {
			if ctx.Err() == nil {
				logger.ErrorF(err, "orphan sweep failed during quarantine")
			}
			return false
		}
		if !ran {
			logger.InfoF("orphan sweep skipped, uploads in flight, will try again next time")
			break
		}
		orphans = orphans[n:]
	}
	if moved > 0 {
		logger.InfoF("orphan sweep moved %d files to quarantine", moved)
	}
	return len(orphans) == 0
}

// quarantine ;; parks the given blobs unless they became live since we looked.
func (a *API) quarantine(ctx context.Context, shas []string) (ran bool, count int, err error) {
	if !a.mu.TryLock() {
		return false, 0, nil
	}
	defer a.mu.Unlock()

	for _, sha := range shas {
		// an upload may have linked this file to a live image since the walk
		_, err := a.squeal.BlobMime(ctx, sha)
		if err == nil {
			continue
		}
		if !errors.Is(err, squeal.ErrNotFound) {
			return true, count, err
		}
		if err := a.store.Quarantine(sha); err != nil {
			if errors.Is(err, blobby.ErrNotFound) {
				continue // already gone, nothing to park
			}
			return true, count, err
		}
		logger.InfoF("quarantined orphan blob sha256 [%s]", sha)
		count++
	}
	return true, count, nil
}
