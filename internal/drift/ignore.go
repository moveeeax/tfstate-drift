package drift

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// IgnoreFile is the name of the ignore file read from the working directory.
const IgnoreFile = ".tfdriftignore"

// IgnoreRule silences known-noisy drift. Address is a glob over the resource
// address; Attr is an optional glob over the flattened attribute path. A rule
// without Attr ignores the whole resource.
//
// Globs support "*" (any run of characters, dots included) and "?" (one
// character); everything else, brackets and quotes included, is literal, so
// addresses like `aws_instance.web["a"]` need no escaping.
type IgnoreRule struct {
	Address string
	Attr    string
}

func (r IgnoreRule) String() string {
	if r.Attr == "" {
		return r.Address
	}
	return r.Address + ":" + r.Attr
}

// ParseIgnoreRule parses `<address glob>[:<attr path glob>]`. The separator is
// the first ":" outside square brackets, so for_each keys such as
// `aws_s3_bucket.b["a:b"]` keep their colon, and attribute paths such as
// `tags.aws:cloudformation:stack-name` keep theirs.
func ParseIgnoreRule(s string) (IgnoreRule, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return IgnoreRule{}, errors.New("empty ignore rule")
	}
	depth := 0
	for i, r := range s {
		switch r {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				addr, attr := strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
				if addr == "" || attr == "" {
					return IgnoreRule{}, fmt.Errorf("invalid ignore rule %q: want <address glob>[:<attr path glob>]", s)
				}
				return IgnoreRule{Address: addr, Attr: attr}, nil
			}
		}
	}
	return IgnoreRule{Address: s}, nil
}

// ReadIgnoreRules parses one rule per line. Blank lines and lines starting
// with "#" are skipped.
func ReadIgnoreRules(r io.Reader) ([]IgnoreRule, error) {
	var rules []IgnoreRule
	sc := bufio.NewScanner(r)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		rule, err := ParseIgnoreRule(text)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		rules = append(rules, rule)
	}
	return rules, sc.Err()
}

// LoadIgnoreFile reads IgnoreFile from dir. A missing file is not an error.
func LoadIgnoreFile(dir string) ([]IgnoreRule, error) {
	path := filepath.Join(dir, IgnoreFile)
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rules, err := ReadIgnoreRules(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rules, nil
}

// ignoresResource reports whether some rule without an attribute glob matches
// the address.
func ignoresResource(rules []IgnoreRule, address string) bool {
	for _, r := range rules {
		if r.Attr == "" && globMatch(r.Address, address) {
			return true
		}
	}
	return false
}

// filterIgnored drops every change matched by an attribute rule for this
// address. A rule matches a path when it matches the path itself or one of its
// ancestors, so `tags` covers `tags.LastModified`.
func filterIgnored(rules []IgnoreRule, address string, changes []AttrChange) []AttrChange {
	var active []IgnoreRule
	for _, r := range rules {
		if r.Attr != "" && globMatch(r.Address, address) {
			active = append(active, r)
		}
	}
	if len(active) == 0 {
		return changes
	}
	kept := changes[:0:0]
	for _, c := range changes {
		if !matchesAnyAttr(active, c.Path) {
			kept = append(kept, c)
		}
	}
	return kept
}

func matchesAnyAttr(rules []IgnoreRule, path string) bool {
	for _, r := range rules {
		for _, p := range ancestors(path) {
			if globMatch(r.Attr, p) {
				return true
			}
		}
	}
	return false
}

// ancestors returns path and every prefix of it that ends at a segment
// boundary: "a.b[0].c" -> "a", "a.b", "a.b[0]", "a.b[0].c".
func ancestors(path string) []string {
	var out []string
	for i := 1; i < len(path); i++ {
		if path[i] == '.' || path[i] == '[' {
			out = append(out, path[:i])
		}
	}
	return append(out, path)
}

// globMatch matches s against pattern, where "*" matches any run of
// characters and "?" matches exactly one; everything else is literal.
func globMatch(pattern, s string) bool {
	p, n := 0, 0
	star, mark := -1, 0
	for n < len(s) {
		switch {
		case p < len(pattern) && (pattern[p] == '?' || pattern[p] == s[n]):
			p++
			n++
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, n
			p++
		case star >= 0:
			p = star + 1
			mark++
			n = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}
