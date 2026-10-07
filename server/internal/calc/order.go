package calc

import (
	"fmt"
	"sort"
	"strings"
)

// CycleError is a set of calculation fields that depend on each other, so none
// of them can be computed first.
type CycleError struct {
	Fields []string
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("these calculation fields depend on each other: %s", strings.Join(e.Fields, " -> "))
}

// Order sorts calculation fields so that each one is computed after the
// calculation fields its formula reads. `reads` maps a calculation field to
// the field names its formula mentions; names that are not calculation fields
// are ignored, because an ordinary field is already in the record.
//
// A field that reads itself, or a ring of fields that read each other, cannot
// be ordered and comes back as a *CycleError.
func Order(reads map[string][]string) ([]string, error) {
	// Only dependencies on other calculation fields constrain the order.
	deps := make(map[string][]string, len(reads))
	for field, names := range reads {
		var on []string
		for _, name := range names {
			if _, isCalculation := reads[name]; isCalculation {
				on = append(on, name)
			}
		}
		sort.Strings(on)
		deps[field] = on
	}

	names := make([]string, 0, len(deps))
	for field := range deps {
		names = append(names, field)
	}
	// Sorted so the order is the same every time, which keeps the SQL and the
	// tests stable.
	sort.Strings(names)

	const (
		unvisited = iota
		inProgress
		done
	)
	state := make(map[string]int, len(names))
	var ordered []string
	var stack []string

	var visit func(string) error
	visit = func(field string) error {
		switch state[field] {
		case done:
			return nil
		case inProgress:
			// Report the ring itself, from where it closes.
			start := 0
			for i, name := range stack {
				if name == field {
					start = i
					break
				}
			}
			return &CycleError{Fields: append(append([]string{}, stack[start:]...), field)}
		}

		state[field] = inProgress
		stack = append(stack, field)
		for _, on := range deps[field] {
			if err := visit(on); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[field] = done
		ordered = append(ordered, field)
		return nil
	}

	for _, field := range names {
		if err := visit(field); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}
