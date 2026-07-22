package knowledge

import "fmt"

type targetKind uint8

const (
	targetEntity targetKind = iota + 1
	targetValue
)

type cardinality uint8

const (
	cardinalityMany cardinality = iota + 1
	cardinalityOne
)

type predicateSpec struct {
	target       targetKind
	cardinality  cardinality
	subjectTypes map[EntityType]struct{}
	objectTypes  map[EntityType]struct{}
}

func entityTypes(values ...EntityType) map[EntityType]struct{} {
	result := make(map[EntityType]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

var predicateSpecs = map[string]predicateSpec{
	"responsible_for": {
		target: targetEntity, cardinality: cardinalityMany,
		subjectTypes: entityTypes(EntityPerson, EntityPrincipal),
		objectTypes:  entityTypes(EntityProject),
	},
	"reports_to": {
		target: targetEntity, cardinality: cardinalityOne,
		subjectTypes: entityTypes(EntityPerson, EntityPrincipal),
		objectTypes:  entityTypes(EntityPerson),
	},
	"collaborates_with": {
		target: targetEntity, cardinality: cardinalityMany,
		subjectTypes: entityTypes(EntityPerson, EntityPrincipal),
		objectTypes:  entityTypes(EntityPerson, EntityPrincipal),
	},
	"depends_on": {
		target: targetEntity, cardinality: cardinalityMany,
		subjectTypes: entityTypes(EntityProject),
		objectTypes:  entityTypes(EntityProject),
	},
	"blocked_by": {
		target: targetEntity, cardinality: cardinalityMany,
		subjectTypes: entityTypes(EntityProject),
		objectTypes:  entityTypes(EntityTask),
	},
	"affects": {
		target: targetEntity, cardinality: cardinalityMany,
		subjectTypes: entityTypes(EntityProject, EntityTask, EntityTodo),
		objectTypes:  entityTypes(EntityProject, EntityTask, EntityTodo),
	},
	"prefers": {
		target: targetValue, cardinality: cardinalityMany,
		subjectTypes: entityTypes(EntityPerson, EntityPrincipal),
	},
	"status_summary": {
		target: targetValue, cardinality: cardinalityOne,
		subjectTypes: entityTypes(EntityProject),
	},
	"uses": {
		target: targetValue, cardinality: cardinalityMany,
		subjectTypes: entityTypes(EntityProject, EntityTask),
	},
}

var typedPredicates = map[string]string{
	"belongs_to_project": "use the existing project_id foreign key",
	"materialized_from":  "use task.todo_id",
	"source_group":       "use todo.group_id",
}

func lookupPredicate(name string) (predicateSpec, error) {
	if reason, ok := typedPredicates[name]; ok {
		return predicateSpec{}, fmt.Errorf("%w: predicate %q is typed; %s", ErrInvalidInput, name, reason)
	}
	spec, ok := predicateSpecs[name]
	if !ok {
		return predicateSpec{}, fmt.Errorf("%w: unsupported predicate %q", ErrInvalidInput, name)
	}
	return spec, nil
}
