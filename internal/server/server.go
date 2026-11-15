package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/LX-2v/LX-agent/internal/analyze/goroutines"
	pp "github.com/LX-2v/LX-agent/internal/analyze/pprof"
	"github.com/LX-2v/LX-agent/internal/store"
	"github.com/google/pprof/profile"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Config struct {
	MaxFileBytes, MaxDecodedBytes, StoreBytes int64
	Phase0                                    bool
}
type Server struct {
	mcp      *mcp.Server
	profiles *store.Store
	limits   pp.Limits
	logger   *slog.Logger
	phase0   bool
}

type LoadInput struct {
	Path string `json:"path" jsonschema:"local pprof file path"`
}
type LoadOutput struct {
	ProfileID string      `json:"profile_id"`
	Metadata  pp.Metadata `json:"metadata"`
	Truncated bool        `json:"truncated"`
	NextHint  string      `json:"next_hint,omitempty"`
}
type TopInput struct {
	Path       string `json:"path,omitempty" jsonschema:"local pprof file path for direct analysis"`
	ProfileID  string `json:"profile_id,omitempty"`
	SampleType string `json:"sample_type"`
	N          int    `json:"n,omitempty"`
	Sort       string `json:"sort,omitempty"`
	Cumulative bool   `json:"cumulative,omitempty"`
}
type ProfileInput struct {
	ProfileID string `json:"profile_id"`
}
type PeekInput struct {
	ProfileID string `json:"profile_id"`
	Function  string `json:"function"`
}
type DiffInput struct {
	BaseID     string `json:"base_id"`
	NewID      string `json:"new_id"`
	SampleType string `json:"sample_type"`
	N          int    `json:"n,omitempty"`
}
type DumpInput struct {
	Path      string `json:"path,omitempty"`
	ProfileID string `json:"profile_id,omitempty"`
	N         int    `json:"n,omitempty"`
}
type Result[T any] struct {
	Data      T      `json:"data"`
	Truncated bool   `json:"truncated"`
	NextHint  string `json:"next_hint,omitempty"`
}

func New(c Config, l *slog.Logger) (*Server, error) {
	if c.MaxFileBytes <= 0 || c.MaxDecodedBytes <= 0 || c.StoreBytes <= 0 {
		return nil, fmt.Errorf("all byte limits must be positive")
	}
	st, e := store.New(c.StoreBytes)
	if e != nil {
		return nil, e
	}
	if l == nil {
		l = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	s := &Server{mcp: mcp.NewServer(&mcp.Implementation{Name: "lx-agent", Version: "0.1.0"}, nil), profiles: st, limits: pp.Limits{FileBytes: c.MaxFileBytes, DecodedBytes: c.MaxDecodedBytes}, logger: l, phase0: c.Phase0}
	s.register()
	return s, nil
}

func (s *Server) Run(ctx context.Context) error { return s.mcp.Run(ctx, &mcp.StdioTransport{}) }
func (s *Server) register() {
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "top", Description: "Rank functions in a local or stored profile by flat or cumulative sample value."}, s.top)
	if s.phase0 {
		return
	}
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "load_profile", Description: "Read a local pprof file into the bounded in-memory store and return metadata."}, s.load)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "peek_function", Description: "Return callers and callees for a function in a stored profile."}, s.peek)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "diff_profiles", Description: "Compare function sample values across two stored profiles."}, s.diff)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "goroutine_summary", Description: "Group goroutines by state and stack from a local text dump or stored goroutine profile."}, s.goroutines)
}

func (s *Server) load(ctx context.Context, _ *mcp.CallToolRequest, in LoadInput) (*mcp.CallToolResult, LoadOutput, error) {
	p, size, e := pp.Load(ctx, in.Path, s.limits)
	if e != nil {
		return nil, LoadOutput{}, e
	}
	metadata, e := pp.MetadataForContext(ctx, p)
	if e != nil {
		return nil, LoadOutput{}, e
	}
	id, e := s.profiles.Put(p, size)
	if e != nil {
		return nil, LoadOutput{}, e
	}
	return nil, LoadOutput{ProfileID: id, Metadata: metadata, Truncated: metadata.Truncated, NextHint: metadata.NextHint}, nil
}
func (s *Server) top(ctx context.Context, _ *mcp.CallToolRequest, in TopInput) (*mcp.CallToolResult, Result[pp.Table], error) {
	var p *profile.Profile
	var e error
	if in.Path != "" && in.ProfileID != "" {
		return nil, Result[pp.Table]{}, fmt.Errorf("provide path or profile_id, not both")
	}
	if in.Path != "" {
		p, _, e = pp.Load(ctx, in.Path, s.limits)
	} else if in.ProfileID != "" {
		p, e = s.profiles.Get(in.ProfileID)
	} else {
		return nil, Result[pp.Table]{}, fmt.Errorf("path or profile_id is required")
	}
	if e != nil {
		return nil, Result[pp.Table]{}, e
	}
	if in.N == 0 {
		in.N = 20
	}
	if in.Sort == "" {
		in.Sort = "value"
	}
	v, e := pp.Top(ctx, p, in.SampleType, in.N, in.Sort, in.Cumulative)
	if e != nil {
		return nil, Result[pp.Table]{}, e
	}
	return nil, Result[pp.Table]{Data: v, Truncated: v.Truncated, NextHint: v.NextHint}, nil
}
func (s *Server) peek(ctx context.Context, _ *mcp.CallToolRequest, in PeekInput) (*mcp.CallToolResult, Result[pp.FunctionSummary], error) {
	p, e := s.profiles.Get(in.ProfileID)
	if e != nil {
		return nil, Result[pp.FunctionSummary]{}, e
	}
	v, e := pp.Peek(ctx, p, in.Function)
	if e != nil {
		return nil, Result[pp.FunctionSummary]{}, e
	}
	return nil, Result[pp.FunctionSummary]{Data: v, Truncated: v.Truncated, NextHint: v.NextHint}, nil
}
func (s *Server) diff(ctx context.Context, _ *mcp.CallToolRequest, in DiffInput) (*mcp.CallToolResult, Result[pp.Diff], error) {
	a, e := s.profiles.Get(in.BaseID)
	if e != nil {
		return nil, Result[pp.Diff]{}, e
	}
	b, e := s.profiles.Get(in.NewID)
	if e != nil {
		return nil, Result[pp.Diff]{}, e
	}
	if in.N == 0 {
		in.N = 20
	}
	v, e := pp.DiffProfiles(ctx, a, b, in.SampleType, in.N)
	if e != nil {
		return nil, Result[pp.Diff]{}, e
	}
	return nil, Result[pp.Diff]{Data: v, Truncated: v.Truncated, NextHint: v.NextHint}, nil
}
func (s *Server) goroutines(ctx context.Context, _ *mcp.CallToolRequest, in DumpInput) (*mcp.CallToolResult, Result[goroutines.Summary], error) {
	if in.N == 0 {
		in.N = 20
	}
	var v goroutines.Summary
	var e error
	if in.Path != "" && in.ProfileID != "" {
		return nil, Result[goroutines.Summary]{}, fmt.Errorf("provide path or profile_id, not both")
	}
	if in.Path != "" {
		v, e = goroutines.Parse(ctx, in.Path, s.limits.FileBytes, in.N)
	} else if in.ProfileID != "" {
		p, err := s.profiles.Get(in.ProfileID)
		if err != nil {
			return nil, Result[goroutines.Summary]{}, err
		}
		v, e = goroutines.FromProfile(ctx, p, in.N)
	} else {
		return nil, Result[goroutines.Summary]{}, fmt.Errorf("path or profile_id is required")
	}
	if e != nil {
		return nil, Result[goroutines.Summary]{}, e
	}
	return nil, Result[goroutines.Summary]{Data: v, Truncated: v.Truncated, NextHint: v.NextHint}, nil
}
