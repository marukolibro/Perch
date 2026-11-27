package search

import (
	"fmt"
	"path"
	"strings"
)

// PathFilter limits which workspace files a search scans.
type PathFilter struct {
	// Include and Exclude hold comma-separated path prefixes or globs
	// (such as "internal/app, *.go" or "*_test.go, docs/").
	Include string
	Exclude string
}

// Empty reports whether the filter places no restrictions on paths.
func (f PathFilter) Empty() bool {
	return strings.TrimSpace(f.Include) == "" && strings.TrimSpace(f.Exclude) == ""
}

// CompiledFilter matches slash-separated relative workspace paths.
type CompiledFilter struct {
	include []pathRule
	exclude []pathRule
}

type pathRule struct {
	raw      string
	segments []string
	hasGlob  bool
	hasSlash bool
	dirOnly  bool
}

// Compile parses and validates a PathFilter.
func (f PathFilter) Compile() (*CompiledFilter, error) {
	include, err := parseRules(f.Include)
	if err != nil {
		return nil, err
	}
	exclude, err := parseRules(f.Exclude)
	if err != nil {
		return nil, err
	}
	return &CompiledFilter{include: include, exclude: exclude}, nil
}

// Match reports whether relPath passes the filter.
func (c *CompiledFilter) Match(relPath string) bool {
	if c == nil {
		return true
	}
	if len(c.include) > 0 && !matchesAny(c.include, relPath) {
		return false
	}
	if len(c.exclude) > 0 && matchesAny(c.exclude, relPath) {
		return false
	}
	return true
}

// FilterPaths returns the subset of paths accepted by c.
func FilterPaths(paths []string, c *CompiledFilter) []string {
	if c == nil || (len(c.include) == 0 && len(c.exclude) == 0) {
		return paths
	}
	filtered := make([]string, 0, len(paths))
	for _, p := range paths {
		if c.Match(p) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func parseRules(spec string) ([]pathRule, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	parts := strings.Split(spec, ",")
	rules := make([]pathRule, 0, len(parts))
	for _, raw := range parts {
		rule, ok, err := parseRule(raw)
		if err != nil {
			return nil, err
		}
		if ok {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

func parseRule(raw string) (pathRule, bool, error) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return pathRule{}, false, nil
	}
	cleaned = strings.ReplaceAll(cleaned, "\\", "/")
	for strings.HasPrefix(cleaned, "./") {
		cleaned = strings.TrimPrefix(cleaned, "./")
	}
	dirOnly := strings.HasSuffix(cleaned, "/")
	cleaned = strings.Trim(cleaned, "/")
	if cleaned == "" {
		return pathRule{}, false, nil
	}
	segments := strings.Split(cleaned, "/")
	hasGlob := strings.ContainsAny(cleaned, "*?[")
	for _, seg := range segments {
		if seg == "**" {
			continue
		}
		if _, err := path.Match(seg, ""); err != nil {
			return pathRule{}, false, fmt.Errorf("compile path filter %q: %w", raw, err)
		}
	}
	return pathRule{
		raw:      cleaned,
		segments: segments,
		hasGlob:  hasGlob,
		hasSlash: len(segments) > 1,
		dirOnly:  dirOnly,
	}, true, nil
}

func matchesAny(rules []pathRule, relPath string) bool {
	for index := range rules {
		if rules[index].matches(relPath) {
			return true
		}
	}
	return false
}

func (r *pathRule) matches(relPath string) bool {
	if !r.hasGlob {
		return r.matchesLiteral(relPath)
	}
	if !r.hasSlash && !r.dirOnly {
		matched, _ := path.Match(r.raw, path.Base(relPath))
		if matched {
			return true
		}
	}
	parts := strings.Split(relPath, "/")
	if r.dirOnly {
		// Match any ancestor directory of the file.
		for end := 1; end < len(parts); end++ {
			if matchSegments(r.segments, parts[:end]) {
				return true
			}
		}
		return false
	}
	return matchSegments(r.segments, parts)
}

func (r *pathRule) matchesLiteral(relPath string) bool {
	if r.dirOnly {
		return strings.HasPrefix(relPath, r.raw+"/") || strings.Contains(relPath, "/"+r.raw+"/")
	}
	if r.hasSlash {
		return relPath == r.raw || strings.HasPrefix(relPath, r.raw+"/")
	}
	// Single literal segment matches either an exact file name or a folder name.
	if path.Base(relPath) == r.raw {
		return true
	}
	return strings.HasPrefix(relPath, r.raw+"/") || strings.Contains(relPath, "/"+r.raw+"/")
}

// matchSegments matches glob segments (supporting "**") against path segments.
func matchSegments(pattern []string, target []string) bool {
	if len(pattern) == 0 {
		return len(target) == 0
	}
	if pattern[0] == "**" {
		if matchSegments(pattern[1:], target) {
			return true
		}
		return len(target) > 0 && matchSegments(pattern, target[1:])
	}
	if len(target) == 0 {
		return false
	}
	matched, _ := path.Match(pattern[0], target[0])
	return matched && matchSegments(pattern[1:], target[1:])
}
