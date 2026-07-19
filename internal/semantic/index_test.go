package semantic

import "testing"

func TestNewIndexRejectsInvalidOptionsBeforeConnecting(t *testing.T) {
	valid := Options{
		Host: "127.0.0.1", Port: 6334, Collection: "todo_semantic", EmbeddingModel: "bge_m3_embed", Dimensions: 1024,
		ScoreThreshold: 0.85, NeighborLimit: 3, ActiveStatuses: []string{"extracted"},
	}
	tests := []struct {
		name   string
		mutate func(*Options)
	}{
		{name: "host", mutate: func(o *Options) { o.Host = "" }},
		{name: "port", mutate: func(o *Options) { o.Port = 0 }},
		{name: "collection", mutate: func(o *Options) { o.Collection = "" }},
		{name: "embedding model", mutate: func(o *Options) { o.EmbeddingModel = "" }},
		{name: "dimensions", mutate: func(o *Options) { o.Dimensions = 0 }},
		{name: "threshold", mutate: func(o *Options) { o.ScoreThreshold = 0 }},
		{name: "limit", mutate: func(o *Options) { o.NeighborLimit = 0 }},
		{name: "statuses", mutate: func(o *Options) { o.ActiveStatuses = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := valid
			test.mutate(&opts)
			if _, err := NewIndex(opts); err == nil {
				t.Fatalf("NewIndex() accepted invalid %s", test.name)
			}
		})
	}
}

func TestProjectScopeSeparatesUnassignedFromNumericProjects(t *testing.T) {
	zero := uint64(0)
	if projectScope(nil) != "unassigned" || projectScope(&zero) != "0" {
		t.Fatalf("project scopes: nil=%q zero=%q", projectScope(nil), projectScope(&zero))
	}
}
