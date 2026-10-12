package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
)

// When no row matches the filter, the agents' conversations are searched for
// it: what you and the agent said, not what its tools read or printed, which
// would have every agent that opened a file match that file's words.

// searchMin is the shortest filter the conversations are searched for.
const searchMin = 3

// searchLineMax is the longest transcript record read: a longer one is a
// tool's output, which isn't searched anyway.
const searchLineMax = 8 << 20

// snippetWidth is about how much of a message is shown around what matched.
const snippetWidth = 200

// searchWords are a filter's words as the conversations are searched for
// them, or nil when it's too short to be worth it.
func searchWords(filter string) []string {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if len([]rune(filter)) < searchMin {
		return nil
	}
	return strings.Fields(filter)
}

// finder gathers what a conversation says of the words: which of them it
// has, and the message that has the most, the latest of those.
type finder struct {
	words []string
	seen  []bool
	best  string
	score int
}

func newFinder(words []string) *finder {
	return &finder{words: words, seen: make([]bool, len(words))}
}

// see takes one message.
func (f *finder) see(text string) {
	low := strings.ToLower(text)
	n := 0
	for i, w := range f.words {
		if strings.Contains(low, w) {
			f.seen[i] = true
			n++
		}
	}
	if n > 0 && n >= f.score {
		f.best, f.score = text, n
	}
}

// snippet is the best message around what matched, or "" unless every word
// was said somewhere.
func (f *finder) snippet() string {
	for _, ok := range f.seen {
		if !ok {
			return ""
		}
	}
	return around(f.best, f.words, snippetWidth)
}

// around is text cut to about width around the first of the words in it, on
// one line.
func around(text string, words []string, width int) string {
	runes := []rune(strings.Join(strings.Fields(clean(text, false)), " "))
	low := []rune(strings.ToLower(string(runes)))
	at := -1
	for _, w := range words {
		if i := strings.Index(string(low), w); i >= 0 {
			if r := len([]rune(string(low)[:i])); at < 0 || r < at {
				at = r
			}
		}
	}
	// Lowering a rune can change how many there are of them, rarely: then
	// the text is shown from its start.
	if at < 0 || len(low) != len(runes) {
		at = 0
	}
	start := max(0, at-width/4)
	// From a word's start, not its middle.
	for start > 0 && start < at && !unicode.IsSpace(runes[start-1]) {
		start++
	}
	end := min(len(runes), start+width)
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

// searchSession is where an agent's conversation says every one of words:
// some of the message that has the most of them, or "" when it doesn't.
func searchSession(ctx context.Context, cfg config, s claudeSession, words []string) string {
	if len(words) == 0 {
		return ""
	}
	if !claudes(s) && !s.Native {
		// Read through Memex: the end of the conversation, as a recap has it.
		text, err := memexTranscript(ctx, cfg, s)
		if err != nil {
			return ""
		}
		f := newFinder(words)
		for _, part := range strings.Split(text, "\n\n") {
			f.see(part)
		}
		return f.snippet()
	}
	return searchTranscript(ctx, s.Transcript, words)
}

// searchTranscript searches a Claude or Codex transcript: a record a line.
func searchTranscript(ctx context.Context, path string, words []string) string {
	if path == "" {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	f := newFinder(words)
	needles := make([][]byte, len(words))
	for i, w := range words {
		needles[i] = []byte(w)
	}
	r := bufio.NewReaderSize(file, 256<<10)
	for n := 0; ; n++ {
		line, long, err := readLine(r, searchLineMax)
		if len(line) > 0 && !long && mentions(line, needles) {
			var record any
			if json.Unmarshal(line, &record) == nil {
				said(record, f.see)
			}
		}
		if err != nil || (n%256 == 0 && ctx.Err() != nil) {
			break
		}
	}
	return f.snippet()
}

// readLine reads a line, however long, keeping at most limit of it: long
// says it was longer, and the rest was passed over.
func readLine(r *bufio.Reader, limit int) (line []byte, long bool, err error) {
	for {
		part, more, err := r.ReadLine()
		if !long {
			if len(line)+len(part) > limit {
				long, line = true, nil
			} else {
				line = append(line, part...)
			}
		}
		if err != nil || !more {
			return line, long, err
		}
	}
}

// mentions is whether a record could have any of the words: a cheap look
// before it's parsed, which most records (tools' calls and output) fail.
func mentions(line []byte, needles [][]byte) bool {
	low := bytes.ToLower(line)
	for _, n := range needles {
		if bytes.Contains(low, n) {
			return true
		}
	}
	return false
}

// said calls see with each thing a transcript record has you or the agent
// saying. Claude's is {type, message: {role, content}}, Codex's {type,
// payload: {type: message, role, content}}; content is a string or blocks,
// of which only the text ones are speech.
func said(record any, see func(string)) {
	r, ok := record.(map[string]any)
	if !ok || r["isMeta"] == true || r["isSidechain"] == true {
		return
	}
	for _, key := range []string{"message", "payload"} {
		m, ok := r[key].(map[string]any)
		if !ok {
			continue
		}
		if role, _ := m["role"].(string); role != "user" && role != "assistant" {
			continue
		}
		switch content := m["content"].(type) {
		case string:
			see(content)
		case []any:
			for _, b := range content {
				block, _ := b.(map[string]any)
				switch block["type"] {
				case "text", "input_text", "output_text":
					if text, _ := block["text"].(string); text != "" {
						see(text)
					}
				}
			}
		}
	}
}

// With Memex installed, the agents a filter is found in are listed best
// match first, by the score its search gives each one's conversation. Who's
// listed is still decided by reading the conversations, which says what was
// said a moment ago; Memex's index may be behind that.

// rankTimeout is how long Memex is given to score the conversations.
const rankTimeout = 5 * time.Second

// memexQuery is a filter as Memex's search takes it: its words, without the
// punctuation its query syntax would read.
func memexQuery(filter string) string {
	return strings.Join(strings.FieldsFunc(filter, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}), " ")
}

// rankSessions is Memex's score for the filter in each of the sessions: the
// best of what you or the agent said there. Empty without Memex, or when it
// can't say.
func rankSessions(ctx context.Context, cfg config, filter string, ids []string) map[string]float64 {
	query := memexQuery(filter)
	if query == "" || len(ids) == 0 || memexBinary(cfg) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, rankTimeout)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	scores := map[string]float64{}
	room := make(chan struct{}, 4)
	for _, id := range ids {
		if !memexSessionIDPattern.MatchString(id) {
			continue
		}
		for _, role := range []string{"user", "assistant"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				room <- struct{}{}
				defer func() { <-room }()
				out, err := runMemex(ctx, cfg, "search", "--session", id, "--role", role, "--limit", "1",
					"--format", "json", "--fields", "score,session_id", "--non-interactive", "--no-update-check", "--", query)
				if err != nil {
					return
				}
				var rows []struct {
					Score     float64 `json:"score"`
					SessionID string  `json:"session_id"`
				}
				if json.Unmarshal(out, &rows) != nil {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				for _, row := range rows {
					if row.SessionID == id && row.Score > scores[id] {
						scores[id] = row.Score
					}
				}
			}()
		}
	}
	wg.Wait()
	return scores
}
