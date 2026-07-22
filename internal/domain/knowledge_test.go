package domain

import "testing"

func TestKnowledgeModels(t *testing.T) {
	t.Parallel()
	models := KnowledgeModels()
	if got, want := len(models), 1; got != want {
		t.Fatalf("KnowledgeModels() length = %d, want %d", got, want)
	}
	if got := models[0].(*RelationFact).TableName(); got != "relation_fact" {
		t.Fatalf("RelationFact table = %q, want relation_fact", got)
	}
}
