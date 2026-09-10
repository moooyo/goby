package backuppg

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

// This fixture models the nonverbose PostgreSQL 17 data-only text grammar.
// It intentionally includes the complete prologue and trailer: a sequence of
// individually valid COPY blocks is not sufficient proof of a complete archive.
const decodeTestHeader = `--
-- PostgreSQL database dump
--

\restrict A1b2C3

-- Dumped from database version 17.11 (Debian 17.11-1.pgdg12+1)
-- Dumped by pg_dump version 17.11 (Debian 17.11-1.pgdg12+1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

`

const decodeTestUsers = `--
-- Data for Name: users; Type: TABLE DATA; Schema: public; Owner: -
--

COPY "public"."users" ("id", "name") FROM stdin;
1	Ada
2	A\\B\tC\nD\rE
3	\N
\.


`

const decodeTestEmpty = `--
-- Data for Name: libraries; Type: TABLE DATA; Schema: public; Owner: -
--

COPY "public"."libraries" ("id", "name") FROM stdin;
\.


`

const decodeTestSequence = `--
-- Name: users_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('"public"."users_id_seq"', 3, true);


`

const decodeTestFooter = `--
-- PostgreSQL database dump complete
--

\unrestrict A1b2C3

`

func decodeTestArchive() string {
	return decodeTestHeader + decodeTestUsers + decodeTestEmpty + decodeTestSequence + decodeTestFooter
}

func decodeTestCatalog() Catalog {
	return Catalog{
		Schema: "public",
		Tables: []TableSpec{
			{Name: "users", Columns: []string{"id", "name"}, PrimaryKey: []string{"id"}},
			{Name: "libraries", Columns: []string{"id", "name"}, PrimaryKey: []string{"id"}},
		},
		Sequences: []SequenceSpec{{Name: "users_id_seq", Table: "users", Column: "id", MinValue: 1, MaxValue: math.MaxInt64, Increment: 1}},
	}
}

type decodeTestSink struct {
	data      map[string][]byte
	sequences map[string]decodeTestSequenceValue
	copyFn    func(context.Context, TableSpec, io.Reader) (int64, error)
	seqFn     func(context.Context, SequenceSpec, int64, bool) error
}

type decodeTestSequenceValue struct {
	value  int64
	called bool
}

func (s *decodeTestSink) Copy(ctx context.Context, table TableSpec, data io.Reader) (int64, error) {
	if s.copyFn != nil {
		return s.copyFn(ctx, table, data)
	}
	content, err := io.ReadAll(data)
	if s.data == nil {
		s.data = map[string][]byte{}
	}
	s.data[table.Name] = content
	return int64(bytes.Count(content, []byte{'\n'})), err
}

func (s *decodeTestSink) SetSequence(ctx context.Context, sequence SequenceSpec, value int64, called bool) error {
	if s.seqFn != nil {
		return s.seqFn(ctx, sequence, value, called)
	}
	if s.sequences == nil {
		s.sequences = map[string]decodeTestSequenceValue{}
	}
	s.sequences[sequence.Name] = decodeTestSequenceValue{value, called}
	return nil
}

func TestDecodePreservesCOPYBytesAndTrustedDestinations(t *testing.T) {
	sink := &decodeTestSink{}
	if err := Decode(t.Context(), iotest.OneByteReader(strings.NewReader(decodeTestArchive())), decodeTestCatalog(), sink, DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	want := map[string][]byte{
		"users":     []byte("1\tAda\n2\tA\\\\B\\tC\\nD\\rE\n3\t\\N\n"),
		"libraries": {},
	}
	if !reflect.DeepEqual(sink.data, want) {
		t.Fatalf("COPY bytes changed: got %#v, want %#v", sink.data, want)
	}
	if got := sink.sequences["users_id_seq"]; got != (decodeTestSequenceValue{3, true}) {
		t.Fatalf("unexpected sequence: %#v", got)
	}
}

func TestDecodeKeepsSQLAndEscapedTerminatorsInsideCOPY(t *testing.T) {
	rows := "1\t\\\\.\n2\tSELECT pg_catalog.setval('other', 900, true);\n3\t\\\\! touch ignored\n4\t-- PostgreSQL database dump complete\n"
	archive := strings.Replace(decodeTestArchive(), "1\tAda\n2\tA\\\\B\\tC\\nD\\rE\n3\t\\N\n", rows, 1)
	sink := &decodeTestSink{}
	if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), sink, DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sink.data["users"], []byte(rows)) {
		t.Fatal("COPY content was reinterpreted or changed")
	}
}

func TestDecodeAcceptsPG17WithoutRestrictedMode(t *testing.T) {
	archive := strings.ReplaceAll(decodeTestArchive(), "\\restrict A1b2C3\n", "")
	archive = strings.ReplaceAll(archive, "\\unrestrict A1b2C3\n", "")
	archive = strings.ReplaceAll(archive, "17.11 (Debian 17.11-1.pgdg12+1)", "17.5")
	if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), &decodeTestSink{}, DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeRejectsUnknownSQLAndObjects(t *testing.T) {
	tests := map[string]struct{ from, to string }{
		"program_copy":        {`COPY "public"."users" ("id", "name") FROM stdin;`, `COPY "public"."users" FROM PROGRAM 'id';`},
		"select_copy":         {`COPY "public"."users" ("id", "name") FROM stdin;`, `COPY (SELECT 1) TO stdout;`},
		"schema":              {`COPY "public"."users"`, `COPY "other"."users"`},
		"table":               {`COPY "public"."users"`, `COPY "public"."credentials"`},
		"column":              {`("id", "name") FROM stdin;`, `("id", "password_hash") FROM stdin;`},
		"column_order":        {`("id", "name") FROM stdin;`, `("name", "id") FROM stdin;`},
		"column_duplicate":    {`("id", "name") FROM stdin;`, `("id", "id") FROM stdin;`},
		"unquoted":            {`COPY "public"."users"`, `COPY public.users`},
		"same_line_suffix":    {`FROM stdin;`, `FROM stdin; DELETE FROM users;`},
		"drop":                {decodeTestSequence, "DROP TABLE users;\n" + decodeTestSequence},
		"psql_shell":          {decodeTestSequence, "\\! id\n" + decodeTestSequence},
		"psql_connect":        {decodeTestSequence, "\\connect other\n" + decodeTestSequence},
		"role":                {"SET row_security = off;", "SET ROLE administrator;"},
		"encoding":            {"SET client_encoding = 'UTF8';", "SET client_encoding = 'SQL_ASCII';"},
		"search_path":         {"SELECT pg_catalog.set_config('search_path', '', false);", "SELECT pg_catalog.set_config('search_path', 'untrusted', false);"},
		"sequence_object":     {`'"public"."users_id_seq"'`, `'"public"."unknown_seq"'`},
		"sequence_expression": {`', 3, true);`, `', (SELECT 3), true);`},
		"sequence_suffix":     {`', 3, true);`, `', 3, true); SELECT pg_sleep(99);`},
		"table_comment":       {"Data for Name: users;", "Data for Name: unknown;"},
		"owner_comment":       {"Schema: public; Owner: -", "Schema: public; Owner: administrator"},
		"unlisted_comment":    {decodeTestSequence, "-- unrecognized archive section\n" + decodeTestSequence},
		"block_comment":       {decodeTestSequence, "/* hidden content */\n" + decodeTestSequence},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			original := decodeTestArchive()
			archive := strings.Replace(original, test.from, test.to, 1)
			if archive == original {
				t.Fatal("mutation did not change fixture")
			}
			err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), &decodeTestSink{}, DecodeOptions{})
			if !errors.Is(err, ErrArchive) {
				t.Fatalf("got %v, want ErrArchive", err)
			}
		})
	}
}

func TestDecodeRejectsIncompleteAndDuplicateSections(t *testing.T) {
	tests := map[string]string{
		"missing_table":        decodeTestHeader + decodeTestUsers + decodeTestSequence + decodeTestFooter,
		"missing_sequence":     decodeTestHeader + decodeTestUsers + decodeTestEmpty + decodeTestFooter,
		"duplicate_table":      decodeTestHeader + decodeTestUsers + decodeTestUsers + decodeTestEmpty + decodeTestSequence + decodeTestFooter,
		"duplicate_sequence":   decodeTestHeader + decodeTestUsers + decodeTestEmpty + decodeTestSequence + decodeTestSequence + decodeTestFooter,
		"sequence_early":       decodeTestHeader + decodeTestUsers + decodeTestSequence + decodeTestEmpty + decodeTestFooter,
		"missing_header":       decodeTestUsers + decodeTestEmpty + decodeTestSequence + decodeTestFooter,
		"missing_footer":       decodeTestHeader + decodeTestUsers + decodeTestEmpty + decodeTestSequence,
		"missing_terminator":   strings.Replace(decodeTestArchive(), "\\.\n", "", 1),
		"second_archive":       decodeTestArchive() + decodeTestArchive(),
		"after_footer":         decodeTestArchive() + "SELECT 1;\n",
		"comment_after_footer": decodeTestArchive() + "-- ignored?\n",
		"footer_only":          decodeTestHeader + decodeTestFooter,
	}
	for name, archive := range tests {
		t.Run(name, func(t *testing.T) {
			if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), &decodeTestSink{}, DecodeOptions{}); !errors.Is(err, ErrArchive) {
				t.Fatalf("got %v, want ErrArchive", err)
			}
		})
	}
}

func TestDecodeRejectsEveryTruncatedRequiredSuffix(t *testing.T) {
	archive := decodeTestArchive()
	lastRequired := strings.LastIndex(archive, "\\unrestrict A1b2C3\n") + len("\\unrestrict A1b2C3\n")
	for length := range lastRequired {
		err := Decode(t.Context(), strings.NewReader(archive[:length]), decodeTestCatalog(), &decodeTestSink{}, DecodeOptions{})
		if !errors.Is(err, ErrArchive) {
			t.Fatalf("accepted truncation at byte %d: %v", length, err)
		}
	}
}

func TestDecodeRestrictedModeAndVersionBoundaries(t *testing.T) {
	tests := map[string]struct{ from, to string }{
		"restrict_missing":   {"\\restrict A1b2C3\n", ""},
		"unrestrict_missing": {"\\unrestrict A1b2C3\n", ""},
		"different_key":      {"\\unrestrict A1b2C3", "\\unrestrict Different"},
		"empty_key":          {"\\restrict A1b2C3", "\\restrict "},
		"key_shell":          {"\\restrict A1b2C3", "\\restrict A1b2C3;id"},
		"key_space":          {"\\restrict A1b2C3", "\\restrict A1 b2"},
		"key_too_long":       {"\\restrict A1b2C3", "\\restrict " + strings.Repeat("A", 129)},
		"older_pg":           {"database version 17.11", "database version 16.11"},
		"newer_pg_dump":      {"pg_dump version 17.11", "pg_dump version 18.1"},
		"development_pg":     {"database version 17.11", "database version 17.devel"},
		"no_minor":           {"database version 17.11", "database version 17."},
		"vendor_controls":    {"(Debian 17.11-1.pgdg12+1)", "(Debian\t17.11)"},
		"duplicate_preamble": {"SET row_security = off;", "SET row_security = off;\nSET row_security = off;"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			archive := strings.Replace(decodeTestArchive(), test.from, test.to, 1)
			if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), &decodeTestSink{}, DecodeOptions{}); !errors.Is(err, ErrArchive) {
				t.Fatalf("got %v, want ErrArchive", err)
			}
		})
	}
}

func TestDecodeEnforcesSequenceBoundsWithoutPrecisionLoss(t *testing.T) {
	for _, value := range []string{"1", "9007199254740993", "9223372036854775807"} {
		t.Run(value, func(t *testing.T) {
			archive := strings.Replace(decodeTestArchive(), "', 3, true);", "', "+value+", false);", 1)
			sink := &decodeTestSink{}
			if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), sink, DecodeOptions{}); err != nil {
				t.Fatal(err)
			}
			if sink.sequences["users_id_seq"].called {
				t.Fatal("is_called changed")
			}
			if value == "9007199254740993" && sink.sequences["users_id_seq"].value != 9007199254740993 {
				t.Fatal("sequence lost integer precision")
			}
		})
	}
	for _, value := range []string{"0", "-1", "+1", "01", "1.0", " 1", "1 ", "9223372036854775808", "99999999999999999999"} {
		t.Run("invalid_"+value, func(t *testing.T) {
			archive := strings.Replace(decodeTestArchive(), "', 3, true);", "', "+value+", true);", 1)
			if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), &decodeTestSink{}, DecodeOptions{}); !errors.Is(err, ErrArchive) {
				t.Fatalf("got %v, want ErrArchive", err)
			}
		})
	}
	catalog := decodeTestCatalog()
	catalog.Sequences[0].MinValue = 4
	catalog.Sequences[0].MaxValue = 10
	if err := Decode(t.Context(), strings.NewReader(decodeTestArchive()), catalog, &decodeTestSink{}, DecodeOptions{}); !errors.Is(err, ErrArchive) {
		t.Fatalf("accepted sequence below configured minimum: %v", err)
	}
	catalog.Sequences[0].MinValue = 1
	catalog.Sequences[0].MaxValue = 2
	if err := Decode(t.Context(), strings.NewReader(decodeTestArchive()), catalog, &decodeTestSink{}, DecodeOptions{}); !errors.Is(err, ErrArchive) {
		t.Fatalf("accepted sequence above configured maximum: %v", err)
	}
}

func TestDecodeRequiresSinkToConsumeEOFAndCorrectRowCount(t *testing.T) {
	for _, name := range []string{"no_read", "one_byte", "exact_payload_without_eof", "wrong_count", "swallowed_read_error"} {
		t.Run(name, func(t *testing.T) {
			sink := &decodeTestSink{copyFn: func(_ context.Context, _ TableSpec, data io.Reader) (int64, error) {
				switch name {
				case "no_read":
					return 0, nil
				case "one_byte":
					_, _ = data.Read(make([]byte, 1))
					return 0, nil
				case "exact_payload_without_eof":
					_, _ = io.ReadFull(data, make([]byte, len("1\tAda\n2\tA\\\\B\\tC\\nD\\rE\n3\t\\N\n")))
					return 3, nil
				case "wrong_count":
					_, err := io.Copy(io.Discard, data)
					return 999, err
				default:
					_, _ = io.Copy(io.Discard, data)
					return 3, nil
				}
			}}
			archive := decodeTestArchive()
			if name == "swallowed_read_error" {
				archive = decodeTestHeader + strings.TrimSuffix(decodeTestUsers, "\\.\n\n\n")
			}
			if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), sink, DecodeOptions{}); !errors.Is(err, ErrArchive) {
				t.Fatalf("got %v, want ErrArchive", err)
			}
		})
	}
}

func TestDecodeInvalidatesRetainedCOPYReaders(t *testing.T) {
	var retained []io.Reader
	sink := &decodeTestSink{copyFn: func(_ context.Context, _ TableSpec, data io.Reader) (int64, error) {
		retained = append(retained, data)
		content, err := io.ReadAll(data)
		return int64(bytes.Count(content, []byte{'\n'})), err
	}}
	if err := Decode(t.Context(), strings.NewReader(decodeTestArchive()), decodeTestCatalog(), sink, DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, data := range retained {
		if _, err := data.Read(make([]byte, 1)); !errors.Is(err, ErrArchive) {
			t.Fatalf("retained COPY reader remained usable: %v", err)
		}
	}
}

func TestDecodeEnforcesStreamingSizeLimits(t *testing.T) {
	archive := decodeTestArchive()
	if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), &decodeTestSink{}, DecodeOptions{MaxBytes: int64(len(archive))}); err != nil {
		t.Fatalf("rejected exact byte boundary: %v", err)
	}
	if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), &decodeTestSink{}, DecodeOptions{MaxBytes: int64(len(archive) - 1)}); !errors.Is(err, ErrLimit) {
		t.Fatalf("got %v, want ErrLimit", err)
	}
	row := "1\t" + strings.Repeat("x", 70<<10) + "\n"
	archive = strings.Replace(archive, "1\tAda\n", row, 1)
	sink := &decodeTestSink{}
	if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), sink, DecodeOptions{MaxRowBytes: int64(len(row))}); err != nil {
		t.Fatalf("rejected fragmented exact row boundary: %v", err)
	}
	if !bytes.HasPrefix(sink.data["users"], []byte(row)) {
		t.Fatal("large row changed across reader fragments")
	}
	if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), &decodeTestSink{}, DecodeOptions{MaxRowBytes: int64(len(row) - 1)}); !errors.Is(err, ErrLimit) {
		t.Fatalf("got %v, want ErrLimit", err)
	}
	archive = decodeTestHeader + "--" + strings.Repeat("x", 1<<20) + "\n"
	if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), &decodeTestSink{}, DecodeOptions{}); !errors.Is(err, ErrLimit) {
		t.Fatalf("unbounded SQL/comment line accepted: %v", err)
	}
	for _, options := range []DecodeOptions{{MaxBytes: -1}, {MaxRowBytes: -1}, {MaxBytes: (64 << 30) + 1}, {MaxRowBytes: (32 << 20) + 1}} {
		if err := Decode(t.Context(), strings.NewReader(decodeTestArchive()), decodeTestCatalog(), &decodeTestSink{}, options); !errors.Is(err, ErrLimit) {
			t.Fatalf("accepted invalid limits %#v: %v", options, err)
		}
	}
}

func TestDecodeRejectsNoncanonicalLineEndingsAndNUL(t *testing.T) {
	for name, archive := range map[string]string{
		"crlf":               strings.ReplaceAll(decodeTestArchive(), "\n", "\r\n"),
		"copy_cr":            strings.Replace(decodeTestArchive(), "1\tAda", "1\tA\rda", 1),
		"copy_nul":           strings.Replace(decodeTestArchive(), "1\tAda", "1\tA\x00da", 1),
		"sql_nul":            strings.Replace(decodeTestArchive(), "FROM stdin;", "FROM stdin;\x00", 1),
		"terminator_space":   strings.Replace(decodeTestArchive(), "\\.\n", "\\. \n", 1),
		"terminator_partial": decodeTestHeader + strings.TrimSuffix(decodeTestUsers, "\n\n\n"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), &decodeTestSink{}, DecodeOptions{}); !errors.Is(err, ErrArchive) {
				t.Fatalf("got %v, want ErrArchive", err)
			}
		})
	}
}

func TestDecodeRejectsNoncanonicalCOPYBeforeSendingTheRow(t *testing.T) {
	for name, row := range map[string]string{
		"continued_row":       "1\tjoined\\\n",
		"continued_delimiter": "1\\\tjoined\n",
		"embedded_terminator": "1\tname\\.\n",
		"terminator_suffix":   "1\tname\\.suffix\n",
		"null_prefix":         "1\tname\\N\n",
		"null_suffix":         "1\t\\Nname\n",
		"null_escapes":        "1\t\\N\\t\n",
		"octal_escape":        "1\t\\141\n",
		"hex_escape":          "1\t\\x61\n",
		"unknown_escape":      "1\t\\z\n",
		"backspace":           "1\tbefore\bafter\n",
		"formfeed":            "1\tbefore\fafter\n",
		"vertical_tab":        "1\tbefore\vafter\n",
		"too_many_columns":    "1\tname\textra\n",
		"too_few_columns":     "single_column\n",
		"invalid_utf8":        "1\tinvalid\xff\n",
	} {
		t.Run(name, func(t *testing.T) {
			archive := strings.Replace(decodeTestArchive(), "1\tAda\n", row, 1)
			sink := &decodeTestSink{}
			if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), sink, DecodeOptions{}); err != ErrArchive {
				t.Fatalf("got %v, want ErrArchive", err)
			}
			if len(sink.data["users"]) != 0 {
				t.Fatal("noncanonical COPY row reached sink")
			}
		})
	}
}

func TestDecodePreservesAllCanonicalCOPYEscapesAndControlBytes(t *testing.T) {
	rows := "\\N\t\\N\n1\t\\\\N\\\\.\\b\\f\\n\\r\\t\\v\n2\tUnicode: \u4e16\u754c\n3\tOther controls: \x01\x07\x0e\x1f\x7f\n"
	archive := strings.Replace(decodeTestArchive(), "1\tAda\n2\tA\\\\B\\tC\\nD\\rE\n3\t\\N\n", rows, 1)
	sink := &decodeTestSink{}
	if err := Decode(t.Context(), strings.NewReader(archive), decodeTestCatalog(), sink, DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sink.data["users"], []byte(rows)) {
		t.Fatal("canonical COPY escapes or control bytes changed")
	}
}

func TestDecodeRejectsUnsafeOrAmbiguousTrustedCatalog(t *testing.T) {
	tests := map[string]func(*Catalog){
		"schema_injection":      func(c *Catalog) { c.Schema = `public"; DROP TABLE users; --` },
		"schema_unicode":        func(c *Catalog) { c.Schema = "p\u00fcblich" },
		"table_injection":       func(c *Catalog) { c.Tables[0].Name = "users;delete" },
		"table_duplicate":       func(c *Catalog) { c.Tables = append(c.Tables, c.Tables[0]) },
		"no_tables":             func(c *Catalog) { c.Tables = nil },
		"no_columns":            func(c *Catalog) { c.Tables[0].Columns = nil },
		"column_injection":      func(c *Catalog) { c.Tables[0].Columns[0] = `id"` },
		"column_duplicate":      func(c *Catalog) { c.Tables[0].Columns[1] = "id" },
		"column_too_long":       func(c *Catalog) { c.Tables[0].Columns[0] = strings.Repeat("a", 64) },
		"unknown_primary_key":   func(c *Catalog) { c.Tables[0].PrimaryKey = []string{"missing"} },
		"duplicate_primary_key": func(c *Catalog) { c.Tables[0].PrimaryKey = []string{"id", "id"} },
		"sequence_duplicate":    func(c *Catalog) { c.Sequences = append(c.Sequences, c.Sequences[0]) },
		"sequence_table":        func(c *Catalog) { c.Sequences[0].Table = "missing" },
		"sequence_column":       func(c *Catalog) { c.Sequences[0].Column = "missing" },
		"sequence_min":          func(c *Catalog) { c.Sequences[0].MinValue = 0 },
		"sequence_range":        func(c *Catalog) { c.Sequences[0].MaxValue = -1 },
		"sequence_increment":    func(c *Catalog) { c.Sequences[0].Increment = 0 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			catalog := decodeTestCatalog()
			mutate(&catalog)
			sink := &decodeTestSink{}
			if err := Decode(t.Context(), strings.NewReader(decodeTestArchive()), catalog, sink, DecodeOptions{}); !errors.Is(err, ErrArchive) {
				t.Fatalf("got %v, want ErrArchive", err)
			}
			if len(sink.data) != 0 || len(sink.sequences) != 0 {
				t.Fatal("unsafe catalog reached sink")
			}
		})
	}
}

type decodeCountingReader struct {
	reader io.Reader
	reads  int
}

func (r *decodeCountingReader) Read(p []byte) (int, error) {
	r.reads++
	return r.reader.Read(p)
}

func TestDecodeCancellationStopsReadsAndSinkOperations(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	input := &decodeCountingReader{reader: strings.NewReader(decodeTestArchive())}
	if err := Decode(ctx, input, decodeTestCatalog(), &decodeTestSink{}, DecodeOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if input.reads != 0 {
		t.Fatal("read input despite prior cancellation")
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	copyCalls, sequenceCalls := 0, 0
	sink := &decodeTestSink{
		copyFn: func(_ context.Context, _ TableSpec, data io.Reader) (int64, error) {
			copyCalls++
			_, err := data.Read(make([]byte, 1))
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			_, err = io.Copy(io.Discard, data)
			return 0, err
		},
		seqFn: func(context.Context, SequenceSpec, int64, bool) error {
			sequenceCalls++
			return nil
		},
	}
	if err := Decode(ctx, strings.NewReader(decodeTestArchive()), decodeTestCatalog(), sink, DecodeOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if copyCalls != 1 || sequenceCalls != 0 {
		t.Fatalf("callbacks continued after cancellation: copies=%d sequences=%d", copyCalls, sequenceCalls)
	}
}

type decodeFailReader struct{}

func (decodeFailReader) Read([]byte) (int, error) {
	return 0, errors.New("private reader failure must not escape")
}

func TestDecodeKeepsInputErrorsPrivateAndPreservesSinkSentinels(t *testing.T) {
	input := io.MultiReader(strings.NewReader(decodeTestHeader), decodeFailReader{})
	if err := Decode(t.Context(), input, decodeTestCatalog(), &decodeTestSink{}, DecodeOptions{}); err != ErrArchive {
		t.Fatalf("got %v, want fixed ErrArchive", err)
	}
	for _, failCopy := range []bool{true, false} {
		sink := &decodeTestSink{}
		if failCopy {
			sink.copyFn = func(context.Context, TableSpec, io.Reader) (int64, error) { return 0, ErrDatabase }
		} else {
			sink.seqFn = func(context.Context, SequenceSpec, int64, bool) error { return ErrDatabase }
		}
		if err := Decode(t.Context(), strings.NewReader(decodeTestArchive()), decodeTestCatalog(), sink, DecodeOptions{}); err != ErrDatabase {
			t.Fatalf("got %v, want ErrDatabase", err)
		}
	}
}

type decodeBlockedReader struct {
	entered chan struct{}
	release chan struct{}
}

func (r *decodeBlockedReader) Read([]byte) (int, error) {
	close(r.entered)
	<-r.release
	return 0, errors.New("private pipe failure")
}

func TestDecodeHandlesSinkTransportFailureBeforeReaderExits(t *testing.T) {
	blocked := &decodeBlockedReader{entered: make(chan struct{}), release: make(chan struct{})}
	copyDone := make(chan error, 1)
	copyHeader := decodeTestUsers[:strings.Index(decodeTestUsers, "1\tAda\n")]
	input := io.MultiReader(strings.NewReader(decodeTestHeader+copyHeader), blocked)
	sink := &decodeTestSink{copyFn: func(_ context.Context, _ TableSpec, data io.Reader) (int64, error) {
		go func() {
			_, err := io.Copy(io.Discard, data)
			copyDone <- err
		}()
		<-blocked.entered
		return 0, ErrDatabase
	}}
	if err := Decode(t.Context(), input, decodeTestCatalog(), sink, DecodeOptions{}); err != ErrDatabase {
		close(blocked.release)
		<-copyDone
		t.Fatalf("got %v, want ErrDatabase", err)
	}
	close(blocked.release)
	if err := <-copyDone; err != ErrArchive {
		t.Fatalf("background read error escaped sanitization: %v", err)
	}
}
