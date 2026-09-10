package backuppg

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

const (
	defaultDecodeMaxBytes    int64 = 64 << 30
	defaultDecodeMaxRowBytes int64 = 32 << 20
	decodeMaxSQLLineBytes    int64 = 1 << 20
)

// DecodeOptions can lower, but cannot raise, the decoder's resource limits.
// MaxRowBytes includes the terminating newline. Zero values select the defaults.
type DecodeOptions struct {
	MaxBytes    int64
	MaxRowBytes int64
}

// Decode consumes PostgreSQL 17 pg_restore data-only text output without ever
// executing archive SQL. The archive must have been dumped with quoted
// identifiers and restored with --data-only --no-owner --no-acl --file=-.
// Only the trusted catalog determines COPY destinations and sequence identities.
//
// A sink must synchronously consume each reader through EOF and return its exact
// row count. It must not retain the reader. The caller must roll back all sink
// effects unless Decode and the pg_restore process both finish successfully.
// A context cannot interrupt an arbitrary blocked io.Reader: the caller must
// also cancel or close the process pipe that supplies input.
func Decode(ctx context.Context, input io.Reader, catalog Catalog, sink CopySink, options DecodeOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if input == nil || sink == nil {
		return ErrArchive
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = defaultDecodeMaxBytes
	}
	if options.MaxRowBytes == 0 {
		options.MaxRowBytes = defaultDecodeMaxRowBytes
	}
	if options.MaxBytes < 1 || options.MaxBytes > defaultDecodeMaxBytes || options.MaxRowBytes < 1 || options.MaxRowBytes > defaultDecodeMaxRowBytes {
		return ErrLimit
	}
	tables, sequences, err := decodeCatalog(catalog)
	if err != nil {
		return err
	}
	d := dumpDecoder{
		ctx: ctx, sink: sink, tables: tables, sequences: sequences,
		rowsMax: options.MaxRowBytes,
		reader:  bufio.NewReaderSize(&decodeInput{ctx: ctx, input: input, remaining: options.MaxBytes}, 32<<10),
	}
	if err := d.header(); err != nil {
		return err
	}
	return d.entries()
}

// Fixed output state follows PostgreSQL REL_17_STABLE's
// src/bin/pg_dump/pg_backup_archiver.c, _doSetFixedOutputState. These statements
// are checked and discarded; the destination connection sets its own state.
var decodePreamble = [...]string{
	"SET statement_timeout = 0;",
	"SET lock_timeout = 0;",
	"SET idle_in_transaction_session_timeout = 0;",
	"SET transaction_timeout = 0;",
	"SET client_encoding = 'UTF8';",
	"SET standard_conforming_strings = on;",
	"SELECT pg_catalog.set_config('search_path', '', false);",
	"SET check_function_bodies = false;",
	"SET xmloption = content;",
	"SET client_min_messages = warning;",
	"SET row_security = off;",
}

type decodeTable struct {
	spec   TableSpec
	header string
	seen   bool
}

type decodeSequence struct {
	spec   SequenceSpec
	prefix string
	seen   bool
}

type dumpDecoder struct {
	ctx        context.Context
	reader     *bufio.Reader
	sink       CopySink
	tables     map[string]*decodeTable
	sequences  map[string]*decodeSequence
	rowsMax    int64
	restrict   string
	sequenceOn bool
}

func decodeCatalog(catalog Catalog) (map[string]*decodeTable, map[string]*decodeSequence, error) {
	if !decodeIdentifier(catalog.Schema) || len(catalog.Tables) == 0 || len(catalog.Tables) > 4096 || len(catalog.Sequences) > 4096 {
		return nil, nil, ErrArchive
	}
	tables := make(map[string]*decodeTable, len(catalog.Tables))
	tableNames := make(map[string]map[string]bool, len(catalog.Tables))
	for _, table := range catalog.Tables {
		if !decodeIdentifier(table.Name) || len(table.Columns) == 0 || len(table.Columns) > 1600 || tableNames[table.Name] != nil {
			return nil, nil, ErrArchive
		}
		columns := make(map[string]bool, len(table.Columns))
		quoted := make([]string, len(table.Columns))
		for i, column := range table.Columns {
			if !decodeIdentifier(column) || columns[column] {
				return nil, nil, ErrArchive
			}
			columns[column] = true
			quoted[i] = `"` + column + `"`
		}
		keys := make(map[string]bool, len(table.PrimaryKey))
		for _, key := range table.PrimaryKey {
			if !columns[key] || keys[key] {
				return nil, nil, ErrArchive
			}
			keys[key] = true
		}
		tableNames[table.Name] = columns
		comment := "-- Data for Name: " + table.Name + "; Type: TABLE DATA; Schema: " + catalog.Schema + "; Owner: -"
		tables[comment] = &decodeTable{
			spec:   table,
			header: `COPY "` + catalog.Schema + `"."` + table.Name + `" (` + strings.Join(quoted, ", ") + ") FROM stdin;",
		}
	}
	sequences := make(map[string]*decodeSequence, len(catalog.Sequences))
	sequenceNames := make(map[string]bool, len(catalog.Sequences))
	for _, sequence := range catalog.Sequences {
		if !decodeIdentifier(sequence.Name) || sequenceNames[sequence.Name] || tableNames[sequence.Table] == nil || !tableNames[sequence.Table][sequence.Column] || sequence.MinValue < 1 || sequence.MaxValue < sequence.MinValue || sequence.Increment < 1 {
			return nil, nil, ErrArchive
		}
		sequenceNames[sequence.Name] = true
		comment := "-- Name: " + sequence.Name + "; Type: SEQUENCE SET; Schema: " + catalog.Schema + "; Owner: -"
		sequences[comment] = &decodeSequence{
			spec:   sequence,
			prefix: `SELECT pg_catalog.setval('"` + catalog.Schema + `"."` + sequence.Name + `"', `,
		}
	}
	return tables, sequences, nil
}

func decodeIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 63 {
		return false
	}
	for i := range value {
		c := value[i]
		if c != '_' && (c < 'a' || c > 'z') && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func (d *dumpDecoder) header() error {
	for _, expected := range []string{"--", "-- PostgreSQL database dump", "--"} {
		if err := d.expect(expected); err != nil {
			return err
		}
	}
	line, err := d.next()
	if err != nil {
		return err
	}
	if strings.HasPrefix(line, `\restrict `) {
		d.restrict = strings.TrimPrefix(line, `\restrict `)
		if !decodeRestrictKey(d.restrict) {
			return ErrArchive
		}
		line, err = d.next()
		if err != nil {
			return err
		}
	}
	if !decodeVersion(line, "-- Dumped from database version ") {
		return ErrArchive
	}
	line, err = d.next()
	if err != nil {
		return err
	}
	if !decodeVersion(line, "-- Dumped by pg_dump version ") {
		return ErrArchive
	}
	for _, expected := range decodePreamble {
		if err := d.expect(expected); err != nil {
			return err
		}
	}
	return nil
}

func (d *dumpDecoder) entries() error {
	for {
		if err := d.expect("--"); err != nil {
			return err
		}
		comment, err := d.next()
		if err != nil {
			return err
		}
		if comment == "-- PostgreSQL database dump complete" {
			return d.footer()
		}
		if err := d.expect("--"); err != nil {
			return err
		}
		statement, err := d.next()
		if err != nil {
			return err
		}
		if table := d.tables[comment]; table != nil {
			if table.seen || d.sequenceOn || statement != table.header {
				return ErrArchive
			}
			data := &decodeCopyReader{decoder: d, columnCount: len(table.spec.Columns)}
			data.active.Store(true)
			rows, err := d.sink.Copy(d.ctx, table.spec, data)
			// PgConn.CopyFrom can return a transport error before its reader
			// goroutine has exited. Published state is atomic, and this decoder
			// never advances the archive after an unsuccessful callback.
			data.active.Store(false)
			if contextErr := d.ctx.Err(); contextErr != nil {
				return contextErr
			}
			if failure := data.failure.Load(); failure != nil {
				return failure.err
			}
			if err != nil {
				return err
			}
			if !data.done.Load() || rows != data.rows.Load() {
				return ErrArchive
			}
			table.seen = true
			continue
		}
		if sequence := d.sequences[comment]; sequence != nil {
			if sequence.seen || !d.tablesComplete() {
				return ErrArchive
			}
			value, called, err := decodeSetval(statement, sequence)
			if err != nil {
				return err
			}
			if err := d.sink.SetSequence(d.ctx, sequence.spec, value, called); err != nil {
				if contextErr := d.ctx.Err(); contextErr != nil {
					return contextErr
				}
				return err
			}
			d.sequenceOn = true
			sequence.seen = true
			continue
		}
		return ErrArchive
	}
}

func (d *dumpDecoder) tablesComplete() bool {
	for _, table := range d.tables {
		if !table.seen {
			return false
		}
	}
	return true
}

func (d *dumpDecoder) footer() error {
	if !d.tablesComplete() {
		return ErrArchive
	}
	for _, sequence := range d.sequences {
		if !sequence.seen {
			return ErrArchive
		}
	}
	if err := d.expect("--"); err != nil {
		return err
	}
	if d.restrict != "" {
		if err := d.expect(`\unrestrict ` + d.restrict); err != nil {
			return err
		}
	}
	for {
		line, err := d.line(decodeMaxSQLLineBytes)
		if errors.Is(err, io.EOF) {
			return d.ctx.Err()
		}
		if err != nil {
			return err
		}
		if len(line) != 1 {
			return ErrArchive
		}
	}
}

func decodeSetval(statement string, sequence *decodeSequence) (int64, bool, error) {
	valueAndCalled, ok := strings.CutPrefix(statement, sequence.prefix)
	if !ok {
		return 0, false, ErrArchive
	}
	valueText, calledText, ok := strings.Cut(valueAndCalled, ", ")
	if !ok || len(valueText) == 0 || len(valueText) > 19 || valueText[0] == '0' {
		return 0, false, ErrArchive
	}
	for i := range valueText {
		if valueText[i] < '0' || valueText[i] > '9' {
			return 0, false, ErrArchive
		}
	}
	value, err := strconv.ParseInt(valueText, 10, 64)
	if err != nil || value < sequence.spec.MinValue || value > sequence.spec.MaxValue {
		return 0, false, ErrArchive
	}
	switch calledText {
	case "true);":
		return value, true, nil
	case "false);":
		return value, false, nil
	default:
		return 0, false, ErrArchive
	}
}

func decodeRestrictKey(key string) bool {
	if len(key) == 0 || len(key) > 128 {
		return false
	}
	for i := range key {
		c := key[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func decodeVersion(line, prefix string) bool {
	version, ok := strings.CutPrefix(line, prefix+"17.")
	if !ok {
		return false
	}
	minor, vendor, found := strings.Cut(version, " ")
	if len(minor) == 0 || len(minor) > 5 {
		return false
	}
	for i := range minor {
		if minor[i] < '0' || minor[i] > '9' {
			return false
		}
	}
	if !found {
		return true
	}
	if len(vendor) < 3 || len(vendor) > 256 || vendor[0] != '(' || vendor[len(vendor)-1] != ')' {
		return false
	}
	for i := range vendor {
		if vendor[i] < 32 || vendor[i] > 126 {
			return false
		}
	}
	return true
}

func (d *dumpDecoder) expect(expected string) error {
	line, err := d.next()
	if err != nil {
		return err
	}
	if line != expected {
		return ErrArchive
	}
	return nil
}

func (d *dumpDecoder) next() (string, error) {
	for {
		line, err := d.line(decodeMaxSQLLineBytes)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", ErrArchive
			}
			return "", err
		}
		if len(line) > 1 {
			return string(line[:len(line)-1]), nil
		}
	}
}

// line returns a complete LF-terminated line. COPY escapes CR and LF inside
// values, so CRLF and partial final lines are never canonical Linux dump output.
func (d *dumpDecoder) line(limit int64) ([]byte, error) {
	var whole []byte
	for {
		if err := d.ctx.Err(); err != nil {
			return nil, err
		}
		fragment, err := d.reader.ReadSlice('\n')
		if int64(len(whole))+int64(len(fragment)) > limit {
			return nil, ErrLimit
		}
		if bytes.IndexByte(fragment, 0) >= 0 || bytes.IndexByte(fragment, '\r') >= 0 {
			return nil, ErrArchive
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			whole = append(whole, fragment...)
			continue
		}
		if err != nil {
			if contextErr := d.ctx.Err(); contextErr != nil {
				return nil, contextErr
			}
			if errors.Is(err, ErrLimit) {
				return nil, ErrLimit
			}
			if errors.Is(err, io.EOF) && len(whole) == 0 && len(fragment) == 0 {
				return nil, io.EOF
			}
			return nil, ErrArchive
		}
		if len(whole) != 0 {
			return append(whole, fragment...), nil
		}
		return fragment, nil
	}
}

type decodeInput struct {
	ctx       context.Context
	input     io.Reader
	remaining int64
}

func (r *decodeInput) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if int64(len(p)) > r.remaining+1 {
		p = p[:r.remaining+1]
	}
	n, err := r.input.Read(p)
	if int64(n) > r.remaining {
		return 0, ErrLimit
	}
	r.remaining -= int64(n)
	return n, err
}

type decodeCopyReader struct {
	decoder     *dumpDecoder
	columnCount int
	pending     []byte
	rows        atomic.Int64
	done        atomic.Bool
	failure     atomic.Pointer[decodeReadError]
	active      atomic.Bool
}

type decodeReadError struct {
	err error
}

func (r *decodeCopyReader) Read(p []byte) (int, error) {
	if err := r.decoder.ctx.Err(); err != nil {
		return 0, err
	}
	if !r.active.Load() {
		return 0, ErrArchive
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.done.Load() {
		return 0, io.EOF
	}
	if failure := r.failure.Load(); failure != nil {
		return 0, failure.err
	}
	if len(r.pending) == 0 {
		line, err := r.decoder.line(max(r.decoder.rowsMax, 3))
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = ErrArchive
			}
			r.failure.Store(&decodeReadError{err: err})
			return 0, err
		}
		if bytes.Equal(line, []byte("\\.\n")) {
			r.done.Store(true)
			return 0, io.EOF
		}
		if int64(len(line)) > r.decoder.rowsMax {
			r.failure.Store(&decodeReadError{err: ErrLimit})
			return 0, ErrLimit
		}
		if !decodeCopyRow(line, r.columnCount) {
			r.failure.Store(&decodeReadError{err: ErrArchive})
			return 0, ErrArchive
		}
		r.rows.Add(1)
		r.pending = line
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

// PostgreSQL COPY TO text emits these escapes, with an entire unquoted \N
// field for NULL. Validate boundaries without changing a byte. In particular,
// COPY FROM also accepts backslash followed by a literal newline, which could
// join many bounded physical lines into an unbounded server-side logical row.
// It also recognizes an unescaped \. within a line as end-of-data. Neither
// form is emitted by COPY TO, and neither can be passed through to the server.
func decodeCopyRow(line []byte, expectedColumns int) bool {
	if !utf8.Valid(line) {
		return false
	}
	columns, fieldStart := 1, 0
	for i := 0; i < len(line)-1; i++ {
		switch line[i] {
		case '\t':
			columns++
			fieldStart = i + 1
			if columns > expectedColumns {
				return false
			}
		case '\b', '\f', '\v':
			return false
		case '\\':
			if i+1 >= len(line)-1 {
				return false
			}
			switch line[i+1] {
			case '\\', 'b', 'f', 'n', 'r', 't', 'v':
			case 'N':
				if i != fieldStart || (i+2 < len(line)-1 && line[i+2] != '\t') {
					return false
				}
			default:
				return false
			}
			i++
		}
	}
	return columns == expectedColumns
}
