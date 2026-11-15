package pprof

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/pprof/profile"
)

func fixture() *profile.Profile {
	hot := &profile.Function{ID: 1, Name: "example.com/app.hotLoop"}
	caller := &profile.Function{ID: 2, Name: "example.com/app.run"}
	cold := &profile.Function{ID: 3, Name: "example.com/app.cold"}
	leaf := &profile.Location{ID: 1, Line: []profile.Line{{Function: hot}}}
	root := &profile.Location{ID: 2, Line: []profile.Line{{Function: caller}}}
	coldLoc := &profile.Location{ID: 3, Line: []profile.Line{{Function: cold}}}
	return &profile.Profile{SampleType: []*profile.ValueType{{Type: "cpu", Unit: "nanoseconds"}}, Sample: []*profile.Sample{{Location: []*profile.Location{leaf, root}, Value: []int64{90}}, {Location: []*profile.Location{leaf}, Value: []int64{10}}, {Location: []*profile.Location{coldLoc}, Value: []int64{1}}}, Function: []*profile.Function{hot, caller, cold}, Location: []*profile.Location{leaf, root, coldLoc}}
}

func TestTopPeekAndDiff(t *testing.T) {
	p := fixture()
	top, err := Top(context.Background(), p, "cpu:nanoseconds", 1, "value", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(top.Rows) != 1 || top.Rows[0].Function != "example.com/app.hotLoop" || !top.Truncated {
		t.Fatalf("unexpected top: %#v", top)
	}
	peek, err := Peek(context.Background(), p, "example.com/app.hotLoop")
	if err != nil {
		t.Fatal(err)
	}
	if len(peek.Callers) != 1 || peek.Callers[0].Function != "example.com/app.run" {
		t.Fatalf("unexpected callers: %#v", peek.Callers)
	}
	diff, err := DiffProfiles(context.Background(), p, fixture(), "cpu", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Rows) == 0 || diff.Rows[0].Delta != 0 {
		t.Fatalf("unexpected diff: %#v", diff)
	}
}

func TestTopSerializedOutputIsBounded(t *testing.T) {
	p := &profile.Profile{SampleType: []*profile.ValueType{{Type: "cpu", Unit: "nanoseconds"}}}
	for i := 0; i < 200; i++ {
		fn := &profile.Function{ID: uint64(i + 1), Name: strings.Repeat("x", 2048) + string(rune(i+1))}
		loc := &profile.Location{ID: uint64(i + 1), Line: []profile.Line{{Function: fn}}}
		p.Function = append(p.Function, fn)
		p.Location = append(p.Location, loc)
		p.Sample = append(p.Sample, &profile.Sample{Location: []*profile.Location{loc}, Value: []int64{int64(i + 1)}})
	}
	got, err := Top(context.Background(), p, "cpu", 100, "value", false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 1<<20 {
		t.Fatalf("output is %d bytes", len(data))
	}
	if !got.Truncated {
		t.Fatal("expected truncation for capped rows and names")
	}
}

func TestEmptyMutexProfileExplainsRequiredTargetSetting(t *testing.T) {
	p := &profile.Profile{SampleType: []*profile.ValueType{{Type: "contentions", Unit: "count"}}}
	_, err := Top(context.Background(), p, "contentions", 20, "value", false)
	if err == nil || !strings.Contains(err.Error(), "enable mutex or block profiling") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseDataRejectsGzipExpansionAndOversizedFile(t *testing.T) {
	var compressed bytes.Buffer
	z := gzip.NewWriter(&compressed)
	chunk := make([]byte, 32<<10)
	for i := 0; i < 512; i++ {
		if _, err := z.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, _, err := ParseData(context.Background(), compressed.Bytes(), 512); err == nil {
		t.Fatal("expected decompressed size rejection")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("gzip expansion rejection exceeded time bound")
	}
	path := filepath.Join(t.TempDir(), "large.pprof")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 100), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(context.Background(), path, Limits{FileBytes: 16, DecodedBytes: 32}); err == nil {
		t.Fatal("expected file size rejection")
	}
}

func FuzzParseData(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("not a profile"))
	p := fixture()
	var b bytes.Buffer
	if err := p.Write(&b); err == nil {
		f.Add(b.Bytes())
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _, _ = ParseData(context.Background(), data, 1<<20)
	})
}

func TestCommittedFixtureAccuracy(t *testing.T) {
	fixtures := []struct{ file, sampleType, want string }{
		{"cpu.pprof", "cpu", "main.cpuHotLoop"},
		{"heap.pprof", "alloc_space", "main.allocationHotspot"},
		{"mutex.pprof", "contentions", "main.lockContention"},
	}
	for _, tc := range fixtures {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join("..", "..", "..", "testdata", tc.file)
			p, _, err := Load(context.Background(), path, Limits{FileBytes: 64 << 20, DecodedBytes: 256 << 20})
			if err != nil {
				t.Fatal(err)
			}
			var typ string
			for _, sample := range p.SampleType {
				if sample.Type == tc.sampleType {
					typ = sample.Type + ":" + sample.Unit
					break
				}
			}
			if typ == "" {
				t.Fatalf("sample type %q missing", tc.sampleType)
			}
			sortMode := "value"
			if tc.sampleType == "contentions" {
				sortMode = "cumulative"
			}
			top, err := Top(context.Background(), p, typ, 3, sortMode, false)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range top.Rows {
				if strings.Contains(row.Function, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("planted root cause %q absent from top 3: %#v", tc.want, top.Rows)
			}
		})
	}
}
