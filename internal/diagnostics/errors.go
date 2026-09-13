package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"syscall"
)

const (
	maxDiagnosticErrorDepth = 16
	maxDiagnosticErrorNodes = 64
)

// These identities admit only the standard library's concrete implementations,
// including when an unknown error embeds or imitates their Unwrap interfaces.
var (
	diagnosticJoinType  = reflect.TypeOf(errors.Join(context.Canceled, io.EOF))
	diagnosticWrapType  = reflect.TypeOf(fmt.Errorf("%w", context.Canceled))
	diagnosticWrapsType = reflect.TypeOf(fmt.Errorf("%w %w", context.Canceled, io.EOF))
)

type classifiedError struct {
	cause error
	class string
}

// WithErrorClass marks an already sanitized cause without formatting it. Callers
// must retain only safe sentinels or fixed errors when control flow needs Unwrap.
// Unknown class strings are discarded rather than becoming diagnostic text.
func WithErrorClass(safeCause error, class string) error {
	if safeCause == nil {
		return nil
	}
	if !diagnosticErrorClassAllowed(class) || class == "none" {
		class = "unclassified"
	}
	return &classifiedError{cause: safeCause, class: class}
}

func (err *classifiedError) Error() string {
	if err == nil || !diagnosticErrorClassAllowed(err.class) || err.class == "none" {
		return "unclassified"
	}
	return err.class
}

func (err *classifiedError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

// ErrorClass returns the first recognized class within a bounded, trusted error
// graph. It never formats error text or invokes methods on unknown error types.
func ErrorClass(err error) string {
	if err == nil {
		return "none"
	}
	class := "unclassified"
	walk := diagnosticErrorWalk{remaining: maxDiagnosticErrorNodes}
	walk.visit(err, 0, func(current error) bool {
		value := diagnosticErrorNodeClass(current)
		if value == "unclassified" {
			return false
		}
		class = value
		return true
	})
	return class
}

// ErrorMatches extracts a known sentinel for safe diagnostics and sanitization.
// It deliberately does not implement arbitrary Is/Unwrap hooks or unbounded
// errors.Is traversal; recovery control flow must keep its existing errors.Is.
func ErrorMatches(err, target error) bool {
	if err == nil || target == nil {
		return err == nil && target == nil
	}
	if nilDiagnosticError(target) || !reflect.ValueOf(target).Comparable() {
		return false
	}
	walk := diagnosticErrorWalk{remaining: maxDiagnosticErrorNodes}
	return walk.visit(err, 0, func(current error) bool { return sameDiagnosticError(current, target) })
}

func sameDiagnosticError(left, right error) bool {
	return reflect.TypeOf(left) == reflect.TypeOf(right) && reflect.ValueOf(left).Comparable() &&
		reflect.ValueOf(right).Comparable() && left == right
}

func nilDiagnosticError(err error) bool {
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	}
	return false
}

type diagnosticErrorWalk struct{ remaining int }

func (walk *diagnosticErrorWalk) visit(err error, depth int, accept func(error) bool) bool {
	if walk.remaining == 0 {
		return false
	}
	walk.remaining--
	if depth >= maxDiagnosticErrorDepth || err == nil || nilDiagnosticError(err) {
		return false
	}
	if accept(err) {
		return true
	}
	var child error
	switch typed := err.(type) {
	case *classifiedError:
		child = typed.cause
	case *fs.PathError:
		child = typed.Err
	case *os.LinkError:
		child = typed.Err
	case *os.SyscallError:
		child = typed.Err
	case *net.OpError:
		child = typed.Err
	case *url.Error:
		child = typed.Err
	default:
		typeID := reflect.TypeOf(err)
		switch typeID {
		case diagnosticWrapType:
			child = err.(interface{ Unwrap() error }).Unwrap()
		case diagnosticJoinType, diagnosticWrapsType:
			children := err.(interface{ Unwrap() []error }).Unwrap()
			for index := 0; index < len(children) && walk.remaining > 0; index++ {
				if walk.visit(children[index], depth+1, accept) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
	return walk.visit(child, depth+1, accept)
}

func diagnosticErrorNodeClass(err error) string {
	for _, known := range []struct {
		err   error
		class string
	}{
		{context.Canceled, "cancelled"}, {context.DeadlineExceeded, "deadline_exceeded"},
		{io.EOF, "eof"}, {io.ErrUnexpectedEOF, "unexpected_eof"},
		{fs.ErrNotExist, "not_found"}, {fs.ErrPermission, "permission_denied"}, {fs.ErrClosed, "closed"},
	} {
		if sameDiagnosticError(err, known.err) {
			return known.class
		}
	}
	switch typed := err.(type) {
	case *classifiedError:
		if diagnosticErrorClassAllowed(typed.class) && typed.class != "none" {
			return typed.class
		}
	case *net.DNSError:
		if typed.IsTimeout {
			return "deadline_exceeded"
		}
		return "network_error"
	case *exec.ExitError:
		return "process_exit"
	case syscall.Errno:
		switch typed {
		case syscall.ENOENT:
			return "not_found"
		case syscall.EACCES, syscall.EPERM:
			return "permission_denied"
		case syscall.ENOSPC:
			return "no_space"
		case syscall.ECONNREFUSED:
			return "connection_refused"
		case syscall.ECONNRESET, syscall.EPIPE:
			return "connection_closed"
		case syscall.ETIMEDOUT:
			return "deadline_exceeded"
		case syscall.EADDRINUSE:
			return "address_in_use"
		case syscall.EIO:
			return "io_error"
		}
		return "system_error"
	}
	return "unclassified"
}
