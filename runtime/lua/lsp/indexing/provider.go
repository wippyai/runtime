package indexing

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
)

type NodeInfo struct {
	ID     registry.ID
	Kind   registry.Kind
	Source string
}

// Provider exposes code graph data needed for LSP indexing.
type Provider interface {
	AllNodes() []NodeInfo
	Node(id registry.ID) (NodeInfo, error)
	DirectDependencies(id registry.ID) ([]registry.ID, error)
	DependencyManifests(id registry.ID) map[string]*io.Manifest
	ModuleDefs() []*luaapi.ModuleDef
	BuiltinManifestHash() string
}

type ManagerProvider struct {
	cm *code.Manager
}

func NewManagerProvider(cm *code.Manager) Provider {
	if cm == nil {
		return nil
	}
	return &ManagerProvider{cm: cm}
}

func (p *ManagerProvider) AllNodes() []NodeInfo {
	if p == nil || p.cm == nil {
		return nil
	}
	nodes := p.cm.GetAllNodes()
	out := make([]NodeInfo, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		out = append(out, NodeInfo{
			ID:     node.ID,
			Kind:   node.Kind,
			Source: node.Source,
		})
	}
	return out
}

func (p *ManagerProvider) Node(id registry.ID) (NodeInfo, error) {
	if p == nil || p.cm == nil {
		return NodeInfo{}, nil
	}
	node, err := p.cm.GetNode(id)
	if err != nil {
		return NodeInfo{}, err
	}
	if node == nil {
		return NodeInfo{}, nil
	}
	return NodeInfo{
		ID:     node.ID,
		Kind:   node.Kind,
		Source: node.Source,
	}, nil
}

func (p *ManagerProvider) DirectDependencies(id registry.ID) ([]registry.ID, error) {
	if p == nil || p.cm == nil {
		return nil, nil
	}
	nodes, err := p.cm.GetDirectDependencies(id)
	if err != nil {
		return nil, err
	}
	ids := make([]registry.ID, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		ids = append(ids, node.ID)
	}
	return ids, nil
}

func (p *ManagerProvider) DependencyManifests(id registry.ID) map[string]*io.Manifest {
	if p == nil || p.cm == nil {
		return nil
	}
	return p.cm.GetNodeDependencyManifests(id)
}

func (p *ManagerProvider) ModuleDefs() []*luaapi.ModuleDef {
	if p == nil || p.cm == nil {
		return nil
	}
	return p.cm.GetModuleDefs()
}

func (p *ManagerProvider) BuiltinManifestHash() string {
	if p == nil || p.cm == nil {
		return ""
	}
	return p.cm.BuiltinManifestHash()
}
