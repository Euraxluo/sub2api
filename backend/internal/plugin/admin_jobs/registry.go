package admin_jobs

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type builtinJob struct {
	definition BuiltinDefinition
	handler    Handler
}

var builtinRegistry = struct {
	sync.RWMutex
	items map[string]builtinJob
}{items: make(map[string]builtinJob)}

// RegisterBuiltin registers one compiled Admin Tools job.
func RegisterBuiltin(definition BuiltinDefinition, handler Handler) {
	id := strings.TrimSpace(definition.ID)
	if id == "" || handler == nil {
		panic("admin_jobs: builtin id and handler are required")
	}
	definition.ID = id
	builtinRegistry.Lock()
	defer builtinRegistry.Unlock()
	if _, exists := builtinRegistry.items[id]; exists {
		panic(fmt.Sprintf("admin_jobs: duplicate builtin %q", id))
	}
	builtinRegistry.items[id] = builtinJob{definition: definition, handler: handler}
}

func lookupBuiltin(id string) (builtinJob, bool) {
	builtinRegistry.RLock()
	defer builtinRegistry.RUnlock()
	job, ok := builtinRegistry.items[strings.TrimSpace(id)]
	return job, ok
}

// BuiltinDefinitions returns stable, sorted metadata for the Admin Tools UI.
func BuiltinDefinitions() []BuiltinDefinition {
	builtinRegistry.RLock()
	definitions := make([]BuiltinDefinition, 0, len(builtinRegistry.items))
	for _, item := range builtinRegistry.items {
		definitions = append(definitions, item.definition)
	}
	builtinRegistry.RUnlock()
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	return definitions
}
