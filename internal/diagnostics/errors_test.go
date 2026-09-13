package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"testing"
)

type diagnosticComparableError struct{ value any }

func (diagnosticComparableError) Error() string { panic("private-comparable-error") }

func TestErrorTraversalUsesOnlyKnownConcreteWrappers(t *testing.T) {
	calls := 0
	hostile := diagnosticPanicError{calls: &calls}
	known := fmt.Errorf("private-wrap: %w", context.DeadlineExceeded)
	joined := errors.Join(hostile, &fs.PathError{Op: "private-op", Path: "/private-path", Err: known})
	if ErrorClass(joined) != "deadline_exceeded" || !ErrorMatches(joined, context.DeadlineExceeded) || calls != 0 {
		t.Fatal("trusted traversal lost a known error or evaluated an arbitrary method")
	}
	if ErrorClass(hostile) != "unclassified" || ErrorMatches(hostile, context.Canceled) || calls != 0 {
		t.Fatal("an unknown error implementation gained trusted traversal")
	}
	multiple := fmt.Errorf("private-wrappers: %w %w", io.EOF, context.Canceled)
	if ErrorClass(multiple) != "eof" || !ErrorMatches(multiple, context.Canceled) {
		t.Fatal("multiple standard wraps did not retain ordered classes and sentinels")
	}
}

func TestErrorTraversalHandlesNilAndNonComparableValues(t *testing.T) {
	for _, typeID := range []reflect.Type{diagnosticJoinType, diagnosticWrapType, diagnosticWrapsType, reflect.TypeOf((*classifiedError)(nil))} {
		err := reflect.Zero(typeID).Interface().(error)
		if ErrorClass(err) != "unclassified" || ErrorMatches(err, context.Canceled) || ErrorMatches(context.Canceled, err) {
			t.Fatal("a typed nil error was traversed or matched")
		}
	}
	err := diagnosticComparableError{value: []string{"private-value"}}
	if ErrorMatches(err, err) || ErrorClass(err) != "unclassified" || ErrorMatches(diagnosticSliceError{"private-value"}, context.Canceled) {
		t.Fatal("non-comparable error values were matched")
	}
	if ErrorClass(nil) != "none" || !ErrorMatches(nil, nil) || ErrorMatches(context.Canceled, nil) {
		t.Fatal("nil error classification changed")
	}
}

func TestErrorTraversalBoundsDepthWidthAndCycles(t *testing.T) {
	var deep error = context.Canceled
	for index := 0; index < maxDiagnosticErrorDepth; index++ {
		deep = fmt.Errorf("fixed wrapper: %w", deep)
	}
	if ErrorClass(deep) != "unclassified" || ErrorMatches(deep, context.Canceled) {
		t.Fatal("diagnostic traversal exceeded its depth budget")
	}
	children := make([]error, maxDiagnosticErrorNodes+1)
	for index := range children {
		children[index] = errors.New("fixed unknown failure")
	}
	children[len(children)-1] = context.Canceled
	wide := errors.Join(children...)
	if ErrorClass(wide) != "unclassified" || ErrorMatches(wide, context.Canceled) {
		t.Fatal("diagnostic traversal exceeded its total node budget")
	}
	cycle := &fs.PathError{Op: "fixed", Path: "/private-path"}
	cycle.Err = cycle
	if ErrorClass(cycle) != "unclassified" || ErrorMatches(cycle, io.EOF) {
		t.Fatal("a wrapper cycle escaped the traversal budget")
	}
	if ErrorClass(errors.Join(cycle, (*fs.PathError)(nil), io.EOF)) != "eof" {
		t.Fatal("bounded opaque branches hid a later known sibling")
	}
}

func TestWithErrorClassKeepsSafeSentinelsAndRejectsDynamicClasses(t *testing.T) {
	err := WithErrorClass(context.Canceled, "panic")
	if ErrorClass(errors.Join(errors.New("fixed stage"), err)) != "panic" || !errors.Is(err, context.Canceled) || err.Error() != "panic" {
		t.Fatal("an explicit safe class lost its sentinel or survived join incorrectly")
	}
	for index := 0; index < 8; index++ {
		err = errors.Join(nil, err)
	}
	if ErrorClass(err) != "panic" || !ErrorMatches(err, context.Canceled) {
		t.Fatal("the normal generation cleanup join depth lost a safe failure")
	}
	const secret = "private-class-and-payload"
	unknown := WithErrorClass(errors.New(secret), secret)
	if ErrorClass(unknown) != "unclassified" || strings.Contains(unknown.Error(), secret) || WithErrorClass(nil, "panic") != nil {
		t.Fatal("a dynamic class or raw cause reached diagnostic text")
	}
}
