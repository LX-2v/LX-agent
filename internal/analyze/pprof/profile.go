package pprof

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/google/pprof/profile"
)

type Limits struct{ FileBytes, DecodedBytes int64 }
type Metadata struct {
	Type          string   `json:"type"`
	DurationNanos int64    `json:"duration_nanos"`
	SampleTypes   []string `json:"sample_types"`
	Total         int64    `json:"total"`
	Truncated     bool     `json:"truncated"`
	NextHint      string   `json:"next_hint,omitempty"`
}
type Row struct {
	Function   string `json:"function"`
	Value      int64  `json:"value"`
	Cumulative int64  `json:"cumulative"`
}
type Table struct {
	Rows      []Row  `json:"rows"`
	Truncated bool   `json:"truncated"`
	NextHint  string `json:"next_hint,omitempty"`
}
type Edge struct {
	Function string `json:"function"`
	Value    int64  `json:"value"`
}
type FunctionSummary struct {
	Function  string `json:"function"`
	Callers   []Edge `json:"callers"`
	Callees   []Edge `json:"callees"`
	Truncated bool   `json:"truncated"`
	NextHint  string `json:"next_hint,omitempty"`
}
type DiffRow struct {
	Function string `json:"function"`
	Base     int64  `json:"base"`
	New      int64  `json:"new"`
	Delta    int64  `json:"delta"`
}
type Diff struct {
	Rows      []DiffRow `json:"rows"`
	Truncated bool      `json:"truncated"`
	NextHint  string    `json:"next_hint,omitempty"`
}

func Load(ctx context.Context, path string, limits Limits) (*profile.Profile, int64, error) {
	if len(path) > 4096 {
		return nil, 0, errors.New("profile path exceeds 4096 bytes")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open profile %q: %w", path, err)
	}
	defer f.Close()
	b, err := readLimited(ctx, f, limits.FileBytes)
	if err != nil {
		return nil, 0, fmt.Errorf("read profile: %w", err)
	}
	return ParseData(ctx, b, limits.DecodedBytes)
}

func ParseData(ctx context.Context, data []byte, maxDecoded int64) (*profile.Profile, int64, error) {
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	default:
	}
	decoded := data
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		z, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, 0, fmt.Errorf("open gzip profile: %w", err)
		}
		decoded, err = readLimited(ctx, z, maxDecoded)
		closeErr := z.Close()
		if err != nil {
			return nil, 0, fmt.Errorf("decompress profile: %w", err)
		}
		if closeErr != nil {
			return nil, 0, fmt.Errorf("finish gzip profile: %w", closeErr)
		}
	} else if int64(len(data)) > maxDecoded {
		return nil, 0, fmt.Errorf("profile exceeds decoded byte limit of %d", maxDecoded)
	}
	p, err := profile.ParseData(decoded)
	if err != nil {
		return nil, 0, fmt.Errorf("parse pprof profile: %w", err)
	}
	if err := p.CheckValid(); err != nil {
		return nil, 0, fmt.Errorf("invalid pprof profile: %w", err)
	}
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	default:
	}
	return p, EstimatedSize(p, int64(len(decoded))), nil
}

func EstimatedSize(p *profile.Profile, wireBytes int64) int64 {
	size := wireBytes
	add := func(n int64) {
		if n > 0 && size <= int64(^uint64(0)>>1)-n {
			size += n
		}
	}
	for _, s := range p.Sample {
		add(128 + int64(len(s.Value))*8 + int64(len(s.Location))*8)
		for k, values := range s.Label {
			add(int64(len(k))*2 + 32)
			for _, v := range values {
				add(int64(len(v))*2 + 16)
			}
		}
		for k, values := range s.NumLabel {
			add(int64(len(k))*2 + int64(len(values))*16)
		}
	}
	for _, f := range p.Function {
		add(128 + int64(len(f.Name)+len(f.SystemName)+len(f.Filename))*2)
	}
	for _, l := range p.Location {
		add(96 + int64(len(l.Line))*32)
	}
	return size
}

func readLimited(ctx context.Context, r io.Reader, max int64) ([]byte, error) {
	if max <= 0 {
		return nil, errors.New("byte limit must be positive")
	}
	var out bytes.Buffer
	chunk := make([]byte, 32<<10)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		n, err := r.Read(chunk)
		if int64(out.Len())+int64(n) > max {
			return nil, fmt.Errorf("input exceeds byte limit of %d", max)
		}
		if n > 0 {
			out.Write(chunk[:n])
		}
		if err == io.EOF {
			return out.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func MetadataFor(p *profile.Profile) Metadata {
	m, _ := MetadataForContext(context.Background(), p)
	return m
}

func MetadataForContext(ctx context.Context, p *profile.Profile) (Metadata, error) {
	m := Metadata{Type: profileType(p), DurationNanos: int64(p.DurationNanos)}
	for i, t := range p.SampleType {
		if i == 32 {
			m.Truncated = true
			m.NextHint = "use top with a specific sample_type to inspect available data"
			break
		}
		m.SampleTypes = append(m.SampleTypes, clip(t.Type, 256)+":"+clip(t.Unit, 256))
	}
	for i, s := range p.Sample {
		if i%4096 == 0 {
			select {
			case <-ctx.Done():
				return Metadata{}, ctx.Err()
			default:
			}
		}
		if len(s.Value) > 0 {
			m.Total += s.Value[0]
		}
	}
	return m, nil
}
func profileType(p *profile.Profile) string {
	for _, s := range p.SampleType {
		switch s.Type {
		case "cpu", "alloc_objects", "alloc_space", "inuse_objects", "inuse_space", "goroutine", "mutex", "block":
			return s.Type
		}
	}
	return "unknown"
}
func sampleIndex(p *profile.Profile, name string) (int, error) {
	if len(name) > 512 {
		return 0, errors.New("sample_type exceeds 512 bytes")
	}
	for i, t := range p.SampleType {
		if name == t.Type || name == t.Type+":"+t.Unit {
			return i, nil
		}
	}
	return 0, fmt.Errorf("sample type %q not found; inspect sample_types returned by load_profile", clip(name, 512))
}
func clip(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}
func label(s *profile.Sample) string {
	if len(s.Location) == 0 {
		return "[unknown]"
	}
	l := s.Location[0]
	if len(l.Line) == 0 || l.Line[0].Function == nil {
		return "[unknown]"
	}
	return l.Line[0].Function.Name
}

func Top(ctx context.Context, p *profile.Profile, sampleType string, n int, sortMode string, cumulative bool) (Table, error) {
	i, err := sampleIndex(p, sampleType)
	if err != nil {
		return Table{}, err
	}
	if n < 1 || n > 100 {
		return Table{}, fmt.Errorf("n must be between 1 and 100")
	}
	if sortMode != "value" && sortMode != "cumulative" {
		return Table{}, fmt.Errorf("sort must be value or cumulative")
	}
	if profileType(p) == "mutex" || profileType(p) == "block" || strings.Contains(sampleType, "contentions") || strings.Contains(sampleType, "delay") {
		hasValue := false
		for k, sample := range p.Sample {
			if k%4096 == 0 {
				select {
				case <-ctx.Done():
					return Table{}, ctx.Err()
				default:
				}
			}
			if i < len(sample.Value) && sample.Value[i] != 0 {
				hasValue = true
				break
			}
		}
		if !hasValue {
			return Table{}, fmt.Errorf("profile contains no samples; enable mutex or block profiling in the target before capturing")
		}
	}
	values := map[string]int64{}
	cum := map[string]int64{}
	stringsTruncated := false
	for k, s := range p.Sample {
		if k%1024 == 0 {
			select {
			case <-ctx.Done():
				return Table{}, ctx.Err()
			default:
			}
		}
		if i >= len(s.Value) {
			continue
		}
		values[label(s)] += s.Value[i]
		seen := map[string]bool{}
		for _, loc := range s.Location {
			for _, ln := range loc.Line {
				if ln.Function != nil && !seen[ln.Function.Name] {
					cum[ln.Function.Name] += s.Value[i]
					seen[ln.Function.Name] = true
				}
			}
		}
	}
	rows := make([]Row, 0, len(values))
	for fn, v := range values {
		if len(fn) > 1024 {
			stringsTruncated = true
		}
		rows = append(rows, Row{Function: clip(fn, 1024), Value: v, Cumulative: cum[fn]})
	}
	if sortMode == "cumulative" || cumulative {
		for fn, v := range cum {
			if _, ok := values[fn]; !ok {
				if len(fn) > 1024 {
					stringsTruncated = true
				}
				rows = append(rows, Row{Function: clip(fn, 1024), Cumulative: v})
			}
		}
	}
	key := func(r Row) int64 {
		if sortMode == "cumulative" || cumulative {
			return r.Cumulative
		}
		return r.Value
	}
	sort.Slice(rows, func(i, j int) bool {
		if key(rows[i]) == key(rows[j]) {
			return rows[i].Function < rows[j].Function
		}
		return key(rows[i]) > key(rows[j])
	})
	select {
	case <-ctx.Done():
		return Table{}, ctx.Err()
	default:
	}
	truncated := len(rows) > n || stringsTruncated
	if truncated {
		rows = rows[:min(len(rows), n)]
	}
	out := Table{Rows: rows, Truncated: truncated}
	if truncated && len(rows) > 0 {
		out.NextHint = "use peek_function with a returned function name or increase n"
	}
	return out, nil
}

func Peek(ctx context.Context, p *profile.Profile, function string) (FunctionSummary, error) {
	if function == "" {
		return FunctionSummary{}, errors.New("function is required")
	}
	if len(function) > 4096 {
		return FunctionSummary{}, errors.New("function exceeds 4096 bytes")
	}
	counts := map[string]map[string]int64{"callers": {}, "callees": {}}
	stringsTruncated := len(function) > 1024
	for k, s := range p.Sample {
		if k%1024 == 0 {
			select {
			case <-ctx.Done():
				return FunctionSummary{}, ctx.Err()
			default:
			}
		}
		if len(s.Value) == 0 {
			continue
		}
		for i, l := range s.Location {
			for _, ln := range l.Line {
				if ln.Function == nil || ln.Function.Name != function {
					continue
				}
				if i+1 < len(s.Location) {
					for _, x := range s.Location[i+1].Line {
						if x.Function != nil {
							if len(x.Function.Name) > 1024 {
								stringsTruncated = true
							}
							counts["callers"][clip(x.Function.Name, 1024)] += s.Value[0]
						}
					}
				}
				if i > 0 {
					for _, x := range s.Location[i-1].Line {
						if x.Function != nil {
							if len(x.Function.Name) > 1024 {
								stringsTruncated = true
							}
							counts["callees"][clip(x.Function.Name, 1024)] += s.Value[0]
						}
					}
				}
			}
		}
	}
	toEdges := func(m map[string]int64) ([]Edge, bool) {
		a := make([]Edge, 0, len(m))
		truncated := len(m) > 50
		for f, v := range m {
			if len(f) > 1024 {
				truncated = true
			}
			a = append(a, Edge{clip(f, 1024), v})
		}
		sort.Slice(a, func(i, j int) bool {
			if a[i].Value == a[j].Value {
				return a[i].Function < a[j].Function
			}
			return a[i].Value > a[j].Value
		})
		if len(a) > 50 {
			a = a[:50]
		}
		return a, truncated
	}
	callers, callersTruncated := toEdges(counts["callers"])
	callees, calleesTruncated := toEdges(counts["callees"])
	select {
	case <-ctx.Done():
		return FunctionSummary{}, ctx.Err()
	default:
	}
	truncated := callersTruncated || calleesTruncated || stringsTruncated
	hint := ""
	if truncated {
		hint = "use a more specific function name or inspect fewer related functions"
	}
	return FunctionSummary{Function: clip(function, 1024), Callers: callers, Callees: callees, Truncated: truncated, NextHint: hint}, nil
}

func DiffProfiles(ctx context.Context, a, b *profile.Profile, typ string, n int) (Diff, error) {
	if n < 1 || n > 100 {
		return Diff{}, errors.New("n must be between 1 and 100")
	}
	ai, e := sampleIndex(a, typ)
	if e != nil {
		return Diff{}, e
	}
	bi, e := sampleIndex(b, typ)
	if e != nil {
		return Diff{}, e
	}
	sums := func(p *profile.Profile, i int) (map[string]int64, error) {
		m := map[string]int64{}
		for k, s := range p.Sample {
			if k%4096 == 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
				}
			}
			if i < len(s.Value) {
				m[label(s)] += s.Value[i]
			}
		}
		return m, nil
	}
	am, e := sums(a, ai)
	if e != nil {
		return Diff{}, e
	}
	bm, e := sums(b, bi)
	if e != nil {
		return Diff{}, e
	}
	rows := []DiffRow{}
	stringsTruncated := false
	for f, v := range am {
		if len(f) > 1024 {
			stringsTruncated = true
		}
		nv := bm[f]
		rows = append(rows, DiffRow{clip(f, 1024), v, nv, nv - v})
		delete(bm, f)
	}
	for f, v := range bm {
		if len(f) > 1024 {
			stringsTruncated = true
		}
		rows = append(rows, DiffRow{clip(f, 1024), 0, v, v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if abs(rows[i].Delta) == abs(rows[j].Delta) {
			return rows[i].Function < rows[j].Function
		}
		return abs(rows[i].Delta) > abs(rows[j].Delta)
	})
	select {
	case <-ctx.Done():
		return Diff{}, ctx.Err()
	default:
	}
	tr := len(rows) > n || stringsTruncated
	if tr {
		rows = rows[:n]
	}
	out := Diff{Rows: rows, Truncated: tr}
	if tr {
		out.NextHint = "narrow by sample type or increase n"
	}
	return out, nil
}
func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
func Marshal(v any) (string, error) { b, e := json.Marshal(v); return string(b), e }
