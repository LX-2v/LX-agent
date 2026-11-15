package goroutines

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/google/pprof/profile"
)

type Group struct {
	State string   `json:"state"`
	Stack []string `json:"stack"`
	Count int      `json:"count"`
}
type Summary struct {
	Total     int     `json:"total"`
	Groups    []Group `json:"groups"`
	Truncated bool    `json:"truncated"`
	NextHint  string  `json:"next_hint,omitempty"`
}

func Parse(ctx context.Context, path string, maxBytes int64, n int) (Summary, error) {
	if len(path) > 4096 {
		return Summary{}, fmt.Errorf("goroutine dump path exceeds 4096 bytes")
	}
	if n < 1 || n > 100 {
		return Summary{}, fmt.Errorf("n must be between 1 and 100")
	}
	f, e := os.Open(path)
	if e != nil {
		return Summary{}, fmt.Errorf("open goroutine dump: %w", e)
	}
	defer f.Close()
	b, e := readBounded(ctx, f, maxBytes)
	if e != nil {
		return Summary{}, e
	}
	if int64(len(b)) > maxBytes {
		return Summary{}, fmt.Errorf("goroutine dump exceeds %d byte limit", maxBytes)
	}
	s := bufio.NewScanner(bytes.NewReader(b))
	s.Buffer(make([]byte, 64<<10), 1<<20)
	groups := map[string]*Group{}
	total := 0
	contentTruncated := false
	state := "unknown"
	stack := []string{}
	flush := func() {
		if len(stack) == 0 {
			return
		}
		key := state + "\x00" + strings.Join(stack, "\n")
		g := groups[key]
		if g == nil {
			g = &Group{State: state, Stack: append([]string(nil), stack...)}
			groups[key] = g
		}
		g.Count++
		total++
		stack = nil
	}
	for s.Scan() {
		select {
		case <-ctx.Done():
			return Summary{}, ctx.Err()
		default:
		}
		line := s.Text()
		if strings.HasPrefix(line, "goroutine ") {
			flush()
			state = "unknown"
			if i := strings.Index(line, "["); i >= 0 {
				if j := strings.Index(line[i:], "]"); j >= 0 {
					state = strings.TrimSpace(line[i+1 : i+j])
					if len(state) > 256 {
						state = state[:256]
						contentTruncated = true
					}
				}
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if len(line) > 256 {
			line = line[:256]
			contentTruncated = true
		}
		stack = append(stack, line)
	}
	flush()
	if e := s.Err(); e != nil {
		return Summary{}, e
	}
	out := make([]Group, 0, len(groups))
	for _, g := range groups {
		copyGroup := *g
		if len(copyGroup.Stack) > 8 {
			copyGroup.Stack = copyGroup.Stack[:8]
			contentTruncated = true
		}
		out = append(out, copyGroup)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].State < out[j].State
		}
		return out[i].Count > out[j].Count
	})
	select {
	case <-ctx.Done():
		return Summary{}, ctx.Err()
	default:
	}
	tr := len(out) > n || contentTruncated
	if len(out) > n {
		out = out[:n]
	}
	r := Summary{Total: total, Groups: out, Truncated: tr}
	if tr {
		r.NextHint = "increase n or inspect the listed stack groups"
	}
	return r, nil
}

func FromProfile(ctx context.Context, p *profile.Profile, n int) (Summary, error) {
	if n < 1 || n > 100 {
		return Summary{}, fmt.Errorf("n must be between 1 and 100")
	}
	groups := map[string]*Group{}
	total := 0
	contentTruncated := false
	for i, s := range p.Sample {
		if i%1024 == 0 {
			select {
			case <-ctx.Done():
				return Summary{}, ctx.Err()
			default:
			}
		}
		stack := []string{}
		for _, loc := range s.Location {
			for _, ln := range loc.Line {
				if ln.Function != nil {
					name := ln.Function.Name
					if len(name) > 256 {
						name = name[:256]
						contentTruncated = true
					}
					stack = append(stack, name)
				}
			}
		}
		if len(stack) == 0 {
			stack = []string{"[unknown]"}
		}
		state := "unknown"
		for k, vs := range s.Label {
			if strings.EqualFold(k, "state") && len(vs) > 0 {
				state = vs[0]
			}
		}
		if len(state) > 256 {
			state = state[:256]
			contentTruncated = true
		}
		key := state + "\x00" + strings.Join(stack, "\n")
		g := groups[key]
		if g == nil {
			bounded := stack
			if len(bounded) > 8 {
				bounded = bounded[:8]
				contentTruncated = true
			}
			g = &Group{State: state, Stack: bounded}
			groups[key] = g
		}
		count := 1
		if len(s.Value) > 0 && s.Value[0] > 0 && s.Value[0] < 1_000_000 {
			count = int(s.Value[0])
		}
		g.Count += count
		total += count
	}
	out := make([]Group, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].State < out[j].State
		}
		return out[i].Count > out[j].Count
	})
	select {
	case <-ctx.Done():
		return Summary{}, ctx.Err()
	default:
	}
	tr := len(out) > n || contentTruncated
	if len(out) > n {
		out = out[:n]
	}
	r := Summary{Total: total, Groups: out, Truncated: tr}
	if tr {
		r.NextHint = "increase n or inspect the listed stack groups"
	}
	return r, nil
}

func readBounded(ctx context.Context, r io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("byte limit must be positive")
	}
	var b bytes.Buffer
	chunk := make([]byte, 32<<10)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		n, err := r.Read(chunk)
		if int64(b.Len())+int64(n) > maxBytes {
			return nil, fmt.Errorf("goroutine dump exceeds %d byte limit", maxBytes)
		}
		if n > 0 {
			b.Write(chunk[:n])
		}
		if err == io.EOF {
			return b.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
	}
}
