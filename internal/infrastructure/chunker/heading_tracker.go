// Package chunker - heading_tracker.go maintains a Markdown ATX heading stack
// (# through ######) across split units so subsequent chunks can inherit chapter context.
package chunker

import (
	"strings"
)

type headingEntry struct {
	level int
	line  string
}

// headingTracker tracks the current ATX heading path while scanning split units.
type headingTracker struct {
	stack []headingEntry
}

func newHeadingTracker() *headingTracker {
	return &headingTracker{}
}

// update scans text for ATX heading lines and updates the heading stack.
func (ht *headingTracker) update(text string) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		heading, ok := parseATXHeading(line)
		if !ok {
			continue
		}
		for len(ht.stack) > 0 && ht.stack[len(ht.stack)-1].level >= heading.level {
			ht.stack = ht.stack[:len(ht.stack)-1]
		}
		ht.stack = append(ht.stack, headingEntry{
			level: heading.level,
			line:  strings.TrimSpace(line),
		})
	}
}

type atxHeading struct {
	level int
	text  string
}

// parseATXHeading parses a Markdown ATX heading line (# to ######).
func parseATXHeading(line string) (atxHeading, bool) {
	line = strings.TrimSpace(line)
	level := 0
	for level < len(line) && level < 6 && line[level] == '#' {
		level++
	}
	if level == 0 || level >= len(line) || line[level] != ' ' {
		return atxHeading{}, false
	}
	text := strings.TrimSpace(line[level+1:])
	if text == "" {
		return atxHeading{}, false
	}
	return atxHeading{level: level, text: text}, true
}

// getPrefix returns the current heading path as ATX lines joined with newlines.
func (ht *headingTracker) getPrefix() string {
	if len(ht.stack) == 0 {
		return ""
	}
	lines := make([]string, len(ht.stack))
	for i, e := range ht.stack {
		lines[i] = e.line
	}
	return strings.Join(lines, "\n") + "\n"
}

// startsWithATXHeading reports whether text begins with a non-empty ATX heading line.
func startsWithATXHeading(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		_, ok := parseATXHeading(trimmed)
		return ok
	}
	return false
}

// headingPrefixAlreadyPresent returns true when the heading path should not be prepended.
func headingPrefixAlreadyPresent(prefix, overlapText, unitText string) bool {
	if prefix == "" {
		return true
	}
	combined := overlapText + unitText
	if startsWithATXHeading(combined) {
		return true
	}
	trimmed := strings.TrimRight(prefix, "\n")
	return trimmed != "" && strings.Contains(overlapText, trimmed)
}
