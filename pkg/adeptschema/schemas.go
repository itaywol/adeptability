// Package adeptschema embeds JSON Schemas for canonical types so consumers
// can validate skill.yaml, adapter.yaml, org.yaml, and config.json without I/O.
package adeptschema

import _ "embed"

//go:embed skill.schema.json
var SkillSchema []byte

//go:embed adapter.schema.json
var AdapterSchema []byte

//go:embed org.schema.json
var OrgSchema []byte

//go:embed config.schema.json
var ConfigSchema []byte
