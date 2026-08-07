package domain

import (
	"reflect"
	"testing"
)

func TestMigrationModelRegistries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		models     []any
		want       []any
		tableNames []string
	}{
		{
			"core", CoreModels(),
			[]any{&Project{}, &KeyMatter{}, &Group{}, &Person{}, &Todo{}, &Task{}, &Resource{}, &ScanRecord{}, &PrincipalProfile{}, &ManagedResource{}, &DailyDigest{}, &ScheduledTask{}},
			[]string{"project", "key_matter", "feishu_group", "person", "todo", "task", "resource", "scan_record", "principal_profile", "managed_resource", "daily_digest", "scheduled_task"},
		},
		{
			"capture", CaptureModels(),
			[]any{&Message{}, &Checkpoint{}, &PrincipalActivityCheckpoint{}},
			[]string{"message", "chat_checkpoint", "principal_activity_checkpoint"},
		},
		{"extract", ExtractModels(), []any{&TodoExtractWatermark{}, &TodoEvent{}, &ExtractionRun{}}, []string{"todo_extract_watermark", "todo_event", "extraction_run"}},
		{"knowledge", KnowledgeModels(), []any{&RelationFact{}}, []string{"relation_fact"}},
		{"progress", ProgressModels(), []any{&TaskEvent{}, &Fact{}}, []string{"task_event", "fact"}},
		{"factengine", FactEngineModels(), []any{&FactSourceCursor{}}, []string{"fact_source_cursor"}},
		{"proactive", ProactiveModels(), []any{&ProactiveRun{}}, []string{"proactive_run"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !reflect.DeepEqual(reflectTypes(test.models), reflectTypes(test.want)) {
				t.Fatalf("registered model types = %v, want %v", reflectTypes(test.models), reflectTypes(test.want))
			}
			if !reflect.DeepEqual(modelTableNames(t, test.models), test.tableNames) {
				t.Fatalf("registered table names = %v, want %v", modelTableNames(t, test.models), test.tableNames)
			}
		})
	}
}

func reflectTypes(models []any) []reflect.Type {
	types := make([]reflect.Type, len(models))
	for i := range models {
		types[i] = reflect.TypeOf(models[i])
	}
	return types
}

func modelTableNames(t *testing.T, models []any) []string {
	t.Helper()
	names := make([]string, len(models))
	for i := range models {
		model, ok := models[i].(interface{ TableName() string })
		if !ok {
			t.Fatalf("registered model %T does not expose TableName", models[i])
		}
		names[i] = model.TableName()
	}
	return names
}
