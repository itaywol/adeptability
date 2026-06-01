// Package adeptschema embeds JSON Schemas for canonical types so consumers
// can validate skill.yaml, adapter.yaml, and org.yaml without I/O.
package adeptschema

import _ "embed"

//go:embed skill.schema.json
var SkillSchema []byte

//go:embed adapter.schema.json
var AdapterSchema []byte

//go:embed org.schema.json
var OrgSchema []byte
