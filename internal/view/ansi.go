package view

import (
	"regexp"
	"strings"
)

// LogSpan is a run of log text with the colour class its ANSI codes selected; Class is empty for plain text.
type LogSpan struct {
	Text  string
	Class string
}

var ansiSeq = regexp.MustCompile("\x1b\\[([0-9;]*)([A-Za-z])")

var ansiColour = map[int]string{
	31: "text-destructive", 32: "text-success", 33: "text-warning", 34: "text-ring", 35: "text-ring", 36: "text-ring",
	91: "text-destructive", 92: "text-success", 93: "text-warning", 94: "text-ring", 95: "text-ring", 96: "text-ring",
	30: "text-muted-foreground", 90: "text-muted-foreground",
}

// ParseANSI splits a log line at its SGR colour codes into spans that carry token classes. Every other escape
// sequence is dropped, and a stray ESC byte is removed, so a log cannot move the cursor or inject markup.
func ParseANSI(line string) []LogSpan {
	if !strings.Contains(line, "\x1b") {
		return []LogSpan{{Text: line}}
	}
	var out []LogSpan
	class, bold, last := "", false, 0
	emit := func(end int) {
		if end > last {
			t := strings.ReplaceAll(line[last:end], "\x1b", "")
			c := class
			if bold {
				c = strings.TrimSpace(c + " font-medium")
			}
			out = append(out, LogSpan{Text: t, Class: c})
		}
	}
	for _, m := range ansiSeq.FindAllStringSubmatchIndex(line, -1) {
		emit(m[0])
		last = m[1]
		if line[m[4]:m[5]] != "m" {
			continue
		}
		params := line[m[2]:m[3]]
		if params == "" {
			params = "0"
		}
		for _, p := range strings.Split(params, ";") {
			n := 0
			for _, r := range p {
				if r < '0' || r > '9' {
					n = -1
					break
				}
				n = n*10 + int(r-'0')
			}
			switch n {
			case 0:
				class, bold = "", false
			case 1:
				bold = true
			case 22:
				bold = false
			case 39:
				class = ""
			default:
				if c, ok := ansiColour[n]; ok {
					class = c
				}
			}
		}
	}
	emit(len(line))
	return out
}
