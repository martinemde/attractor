package pipeline

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// CLIInterviewer implements the Interviewer interface for terminal use.
// It presents questions on the writer and reads answers from the reader.
type CLIInterviewer struct {
	In  io.Reader
	Out io.Writer
}

// NewCLIInterviewer creates a CLIInterviewer that reads from in and writes to out.
func NewCLIInterviewer(in io.Reader, out io.Writer) *CLIInterviewer {
	return &CLIInterviewer{In: in, Out: out}
}

// errTimeout is a sentinel error indicating a timeout occurred during input.
var errTimeout = errors.New("input timeout")

// readLineResult holds the result of a non-blocking line read.
type readLineResult struct {
	line string
	err  error
}

// Ask presents a question to the terminal and returns the user's answer.
func (i *CLIInterviewer) Ask(q *Question) (*Answer, error) {
	if q == nil {
		return nil, fmt.Errorf("nil question")
	}

	// Print the question
	fmt.Fprintf(i.Out, "\n[%s] %s\n", q.Stage, q.Text)

	switch q.Type {
	case QuestionYesNo, QuestionConfirmation:
		return i.askYesNo(q)
	case QuestionMultipleChoice:
		return i.askMultipleChoice(q)
	case QuestionFreeform:
		return i.askFreeform(q)
	default:
		return i.askFreeform(q)
	}
}

func (i *CLIInterviewer) askYesNo(q *Question) (*Answer, error) {
	defaultHint := ""
	if q.Default != nil {
		if q.Default.Value == AnswerYes {
			defaultHint = " [Y/n]"
		} else {
			defaultHint = " [y/N]"
		}
	} else {
		defaultHint = " [y/n]"
	}

	fmt.Fprintf(i.Out, "%s: ", defaultHint)

	line, err := i.readLineWithTimeout(q.TimeoutSeconds)
	if errors.Is(err, errTimeout) {
		return &Answer{Timeout: true}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}

	line = strings.TrimSpace(strings.ToLower(line))

	if line == "" && q.Default != nil {
		return q.Default, nil
	}

	if strings.HasPrefix(line, "y") {
		return &Answer{Value: AnswerYes}, nil
	}
	return &Answer{Value: AnswerNo}, nil
}

func (i *CLIInterviewer) askMultipleChoice(q *Question) (*Answer, error) {
	for idx, opt := range q.Options {
		fmt.Fprintf(i.Out, "  [%s] %s\n", opt.Key, opt.Label)
		_ = idx
	}

	fmt.Fprintf(i.Out, "Choice: ")

	line, err := i.readLineWithTimeout(q.TimeoutSeconds)
	if errors.Is(err, errTimeout) {
		return &Answer{Timeout: true}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}
	line = strings.TrimSpace(line)

	if line == "" && q.Default != nil {
		return q.Default, nil
	}

	// Match by key (case-insensitive)
	for idx := range q.Options {
		opt := &q.Options[idx]
		if strings.EqualFold(line, opt.Key) {
			return &Answer{Value: opt.Key, SelectedOption: opt}, nil
		}
	}

	// No match: return the raw input as the value
	return &Answer{Value: line}, nil
}

func (i *CLIInterviewer) askFreeform(q *Question) (*Answer, error) {
	fmt.Fprintf(i.Out, "> ")

	line, err := i.readLineWithTimeout(q.TimeoutSeconds)
	if errors.Is(err, errTimeout) {
		return &Answer{Timeout: true}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}
	line = strings.TrimSpace(line)

	if line == "" && q.Default != nil {
		return q.Default, nil
	}

	return &Answer{Value: line, Text: line}, nil
}

func (i *CLIInterviewer) readLine() (string, error) {
	return i.readLineWithTimeout(0)
}

// readLineWithTimeout reads a line from input with an optional timeout.
// When timeoutSeconds > 0, it uses a goroutine and select to implement
// non-blocking read with timeout. Returns errTimeout if the timeout expires.
// When timeoutSeconds <= 0, it uses blocking read (original behavior).
func (i *CLIInterviewer) readLineWithTimeout(timeoutSeconds float64) (string, error) {
	// No timeout - use blocking read
	if timeoutSeconds <= 0 {
		scanner := bufio.NewScanner(i.In)
		if scanner.Scan() {
			return scanner.Text(), nil
		}
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}

	// With timeout - use goroutine and select
	resultCh := make(chan readLineResult, 1)
	go func() {
		scanner := bufio.NewScanner(i.In)
		if scanner.Scan() {
			resultCh <- readLineResult{line: scanner.Text()}
			return
		}
		if err := scanner.Err(); err != nil {
			resultCh <- readLineResult{err: err}
			return
		}
		resultCh <- readLineResult{err: io.EOF}
	}()

	timeout := time.Duration(timeoutSeconds * float64(time.Second))
	select {
	case result := <-resultCh:
		return result.line, result.err
	case <-time.After(timeout):
		return "", errTimeout
	}
}
