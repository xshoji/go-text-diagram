package plantuml

import (
	"errors"
	"fmt"
)

var ErrInvalidSyntax = errors.New("invalid PlantUML syntax")

type SyntaxError struct {
	Line    int
	Column  int
	Message string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message)
}

func (e *SyntaxError) Unwrap() error { return ErrInvalidSyntax }

func syntax(line, column int, format string, args ...any) error {
	return &SyntaxError{Line: line, Column: column, Message: fmt.Sprintf(format, args...)}
}
