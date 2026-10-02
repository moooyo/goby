package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/primaryio"
)

type primaryImageContent struct {
	mu        sync.Mutex
	ioMu      sync.Mutex
	methods   sync.WaitGroup
	operation *PrimaryRootIO
	rootID    string
	ctx       context.Context
	cancel    context.CancelFunc
	stopStore func() bool
	file      *os.File
	reader    *io.SectionReader
	closing   bool
	closedFD  chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// The descriptor is deliberately private: every body read is synchronous,
// bounded and admitted, and no WriterTo, ReaderAt or raw-file fast path exists.
func newPrimaryImageContent(ctx context.Context, operation *PrimaryRootIO, rootID string, file *os.File, size int64) *primaryImageContent {
	work, cancel := context.WithCancel(ctx)
	content := &primaryImageContent{operation: operation, rootID: rootID, ctx: work, cancel: cancel,
		file: file, reader: io.NewSectionReader(file, 0, size), closedFD: make(chan struct{})}
	content.stopStore = context.AfterFunc(operation.Context(), cancel)
	context.AfterFunc(work, func() {
		defer close(content.closedFD)
		content.closeErr = closePrimarySidecarResource(operation.Context(), file)
	})
	return content
}

func (content *primaryImageContent) Read(buffer []byte) (n int, resultErr error) {
	content.mu.Lock()
	if content.closing {
		content.mu.Unlock()
		return 0, primaryio.ErrClosed
	}
	content.methods.Add(1)
	content.mu.Unlock()
	defer content.methods.Done()
	content.ioMu.Lock()
	defer content.ioMu.Unlock()
	if err := content.ctx.Err(); err != nil {
		return 0, err
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	var readErr error
	resultErr = content.operation.Run(content.ctx, content.rootID, primaryio.Foreground, func(context.Context) error {
		n, readErr = content.reader.Read(buffer[:min(len(buffer), primaryio.MaxReadChunkBytes)])
		if readErr == io.EOF {
			return nil
		}
		return readErr
	})
	if resultErr == nil {
		return n, readErr
	}
	return n, resultErr
}

// Cancellation interrupts the native descriptor but completes neither the
// actual Read nor its root phase. Close joins both before retiring the Store.
func (content *primaryImageContent) Close() error {
	content.closeOnce.Do(func() {
		content.mu.Lock()
		content.closing = true
		content.mu.Unlock()
		content.cancel()
		<-content.closedFD
		content.methods.Wait()
		content.stopStore()
		content.closeErr = errors.Join(content.closeErr, content.operation.Close())
	})
	return content.closeErr
}

type imageContentWorkResult struct {
	reader io.ReadCloser
	image  Image
	err    error
}

func runImageContentWorker(ctx context.Context, work func() (io.ReadCloser, Image, error)) (io.ReadCloser, Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, Image{}, err
	}
	select {
	case publicImageWorkers <- struct{}{}:
	case <-ctx.Done():
		return nil, Image{}, ctx.Err()
	}
	result := make(chan imageContentWorkResult)
	go func() {
		defer func() { <-publicImageWorkers }()
		if ctx.Err() != nil {
			return
		}
		reader, image, err := work()
		if err != nil && reader != nil {
			_ = reader.Close()
			reader = nil
		}
		select {
		case result <- imageContentWorkResult{reader: reader, image: image, err: err}:
		case <-ctx.Done():
			if reader != nil {
				_ = reader.Close()
			}
		}
	}()
	select {
	case outcome := <-result:
		if err := ctx.Err(); err != nil {
			if outcome.reader != nil {
				_ = outcome.reader.Close()
			}
			return nil, Image{}, err
		}
		return outcome.reader, outcome.image, outcome.err
	case <-ctx.Done():
		return nil, Image{}, ctx.Err()
	}
}

func (s *Store) readStoredImageSnapshotFor(ctx context.Context, subject Subject, itemID, imageType string, index int) (storedImage, error) {
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return storedImage{}, err
	}
	defer rollback(tx)
	stored, err := scanStoredImage(tx.QueryRow(ctx, "SELECT "+storedImageColumns+storedImageSource+`
		WHERE i.id=$1 AND im.image_type=$2 AND im.image_index=$3 AND `+access.directSQL("i"), itemID, imageType, index))
	if errors.Is(err, pgx.ErrNoRows) {
		return storedImage{}, ErrNotFound
	}
	if err != nil {
		return storedImage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return storedImage{}, fmt.Errorf("complete authorized image source read: %w", err)
	}
	return stored, nil
}

func (s *Store) openSidecarImageContentFor(ctx context.Context, subject Subject, itemID, imageType string, index int) (io.ReadCloser, Image, error) {
	return s.openSidecarImageContent(ctx, func(work context.Context) (storedImage, error) {
		return s.readStoredImageSnapshotFor(work, subject, itemID, imageType, index)
	})
}

func (s *Store) openSidecarImageContent(ctx context.Context, readSnapshot func(context.Context) (storedImage, error)) (io.ReadCloser, Image, error) {
	return runImageContentWorker(ctx, func() (_ io.ReadCloser, image Image, resultErr error) {
		stored, err := readSnapshot(ctx)
		if err != nil {
			return nil, Image{}, err
		}
		operation, hint, err := s.prepareSidecarRootIO(ctx, stored.root)
		if err != nil {
			return nil, Image{}, err
		}
		var file *os.File
		adopted := false
		defer func() {
			if adopted {
				return
			}
			if file != nil {
				resultErr = errors.Join(resultErr, closePrimarySidecarResource(operation.Context(), file))
			}
			resultErr = errors.Join(resultErr, operation.Close())
		}()
		err = operation.Run(ctx, stored.root.id, primaryio.Foreground, func(work context.Context) error {
			if err := s.checkSidecarRootHint(work, hint); err != nil {
				return err
			}
			current, err := readSnapshot(work)
			if err != nil {
				return err
			}
			if current.root != stored.root {
				return fmt.Errorf("%w: %w: artwork root changed while queued", ErrUnavailable, ErrSourceChanged)
			}
			stored = current
			file, err = s.openStoredImage(work, stored)
			return err
		})
		if err != nil {
			return nil, Image{}, err
		}
		content := newPrimaryImageContent(ctx, operation, stored.root.id, file, stored.Size)
		adopted = true
		return content, stored.Image, nil
	})
}
