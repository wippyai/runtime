// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	api "github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/service/di"
	contractmod "github.com/wippyai/runtime/runtime/lua/modules/contract"
	"gopkg.in/yaml.v3"
)

const contractCatalogVersion = "contract-catalog-v1"

type contractCatalog struct {
	definitions    map[string]*api.Definition
	bindings       map[string]*api.Binding
	resources      map[string]any
	manifest       *io.Manifest
	diagnostics    []contractmod.ManifestDiagnostic
	fingerprint    string
	boundFunctions map[regapi.ID]bool
}

func collectContractCatalog(entries []regapi.Entry) *contractCatalog {
	c := &contractCatalog{
		definitions:    make(map[string]*api.Definition),
		bindings:       make(map[string]*api.Binding),
		resources:      make(map[string]any),
		boundFunctions: make(map[regapi.ID]bool),
	}
	sorted := append([]regapi.Entry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID.String() < sorted[j].ID.String() })
	hash := sha256.New()
	hash.Write([]byte(contractCatalogVersion))
	hash.Write([]byte(contractmod.SchemaDialect))
	for _, entry := range sorted {
		if entry.Kind != di.Definition && entry.Kind != di.Binding && entry.Kind != regapi.Kind("contract.schema") {
			continue
		}
		hash.Write([]byte(entry.ID.String()))
		hash.Write([]byte(entry.Kind))
		if entry.Data == nil {
			continue
		}
		if raw, err := json.Marshal(entry.Data.Data()); err == nil {
			hash.Write(raw)
		}
		switch entry.Kind {
		case regapi.Kind("contract.schema"):
			c.resources[entry.ID.String()] = entry.Data.Data()
		case di.Definition:
			var config di.DefinitionConfig
			if err := decodeContractEntry(entry.Data, &config); err != nil {
				c.diagnostics = append(c.diagnostics, contractmod.ManifestDiagnostic{Definition: entry.ID.String(), Path: "data", Message: "contract definition unavailable: " + err.Error()})
				continue
			}
			def := config.ToDefinition()
			def.ID = entry.ID
			c.definitions[entry.ID.String()] = def
		case di.Binding:
			var config di.BindingConfig
			if err := decodeContractEntry(entry.Data, &config); err != nil {
				c.diagnostics = append(c.diagnostics, contractmod.ManifestDiagnostic{Definition: entry.ID.String(), Path: "data", Message: "contract binding unavailable: " + err.Error()})
				continue
			}
			binding := config.ToBinding()
			binding.ID = entry.ID
			c.bindings[entry.ID.String()] = binding
			for _, bound := range binding.Contracts {
				for _, functionID := range bound.Methods {
					c.boundFunctions[functionID] = true
				}
			}
		}
	}
	definitionIDs := make([]string, 0, len(c.definitions))
	for id := range c.definitions {
		definitionIDs = append(definitionIDs, id)
	}
	sort.Strings(definitionIDs)
	for _, id := range definitionIDs {
		definition := c.definitions[id]
		for _, method := range definition.Methods {
			for _, schema := range append(append([]api.SchemaDefinition(nil), method.InputSchemas...), method.OutputSchemas...) {
				hash.Write([]byte(schema.Format))
				hash.Write([]byte(contractmod.SchemaTranslatorVersion(schema.Format)))
			}
		}
	}
	c.fingerprint = hex.EncodeToString(hash.Sum(nil))
	c.manifest, c.diagnostics = buildCatalogManifest(c)
	return c
}

func decodeContractEntry(data payload.Payload, target any) error {
	if data == nil {
		return fmt.Errorf("missing entry data")
	}
	var raw []byte
	switch value := data.Data().(type) {
	case []byte:
		raw = value
	case string:
		raw = []byte(value)
	default:
		var err error
		raw, err = json.Marshal(value)
		if err != nil {
			return err
		}
	}
	if data.Format() == payload.YAML {
		var decoded any
		if err := yaml.Unmarshal(raw, &decoded); err != nil {
			return err
		}
		var err error
		raw, err = json.Marshal(decoded)
		if err != nil {
			return err
		}
	}
	return json.Unmarshal(raw, target)
}

func buildCatalogManifest(c *contractCatalog) (*io.Manifest, []contractmod.ManifestDiagnostic) {
	manifest, diagnostics := contractmod.BuildTypedCatalogManifest(c.definitions, c.bindings, c.resources)
	return manifest, append(c.diagnostics, diagnostics...)
}

func (c *contractCatalog) selectedBoundFunction(functionID regapi.ID, filters []string) bool {
	if c == nil {
		return false
	}
	if len(filters) == 0 {
		return c.boundFunctions[functionID]
	}
	for bindingID, binding := range c.bindings {
		if len(filters) != 0 && !matchesNSFilter(regapi.ParseID(bindingID).NS, filters) {
			continue
		}
		for _, bound := range binding.Contracts {
			for _, id := range bound.Methods {
				if id == functionID {
					return true
				}
			}
		}
	}
	return false
}

func appendCatalogCoverage(result *LintResult, catalog *contractCatalog, strict bool, filters []string, minSeverity severity) {
	if result == nil || catalog == nil {
		return
	}
	sev := severityWarning
	if strict {
		sev = severityError
	}
	if sev < minSeverity {
		return
	}
	for _, gap := range catalog.diagnostics {
		id := regapi.ParseID(gap.Definition)
		if len(filters) != 0 && !matchesNSFilter(id.NS, filters) {
			continue
		}
		message := gap.Message
		if gap.Method != "" {
			message = gap.Method + ": " + message
		}
		if gap.Path != "" {
			message += " (" + gap.Path + ")"
		}
		result.Diagnostics = append(result.Diagnostics, Diagnostic{EntryID: gap.Definition, Code: "C0001", Severity: sev.String(), Message: message, Line: 1, Column: 1})
		rich := diag.Diagnostic{Position: diag.Position{File: gap.Definition, Line: 1, Column: 1}, Message: message, Severity: diag.SeverityWarning}
		if strict {
			rich.Severity = diag.SeverityError
		}
		result.RichDiagnostics = append(result.RichDiagnostics, RichDiagnostic{EntryID: gap.Definition, displayCode: "C0001", Diag: rich})
		if strict {
			result.ErrorCount++
		} else {
			result.WarningCount++
		}
	}
	sortLintResults(result)
}
