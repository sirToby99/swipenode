package knowledge

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxPackBytes = 256 << 10

//go:embed builtins/*.yaml
var builtinFiles embed.FS

type Registry struct {
	packs map[string]Pack
}

func Load(root string) (*Registry, error) {
	return LoadWithState(root, "")
}

// LoadWithState includes explicitly activated, signature-verified managed Pack
// releases from repository-local runtime state. Project packs remain separate
// and cannot override either built-ins or managed releases.
func LoadWithState(root, stateDir string) (*Registry, error) {
	registry := &Registry{packs: map[string]Pack{}}
	entries, err := fs.Glob(builtinFiles, "builtins/*.yaml")
	if err != nil {
		return nil, fmt.Errorf("list built-in knowledge packs: %w", err)
	}
	for _, name := range entries {
		data, err := builtinFiles.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read built-in knowledge pack %s: %w", name, err)
		}
		if err := registry.add(data, OriginBuiltin, name); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(stateDir) != "" {
		if err := registry.loadDirectory(filepath.Join(stateDir, "knowledge-active"), OriginManaged); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(root) == "" {
		return registry, nil
	}
	dir := filepath.Join(root, ".swipenode", "knowledge")
	if err := registry.loadDirectory(dir, OriginProject); err != nil {
		return nil, err
	}
	return registry, nil
}

func (r *Registry) loadDirectory(dir string, origin Origin) error {
	local, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s knowledge directory: %w", origin, err)
	}
	for _, entry := range local {
		if entry.IsDir() || (filepath.Ext(entry.Name()) != ".yaml" && filepath.Ext(entry.Name()) != ".yml") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect %s knowledge pack %s: %w", origin, entry.Name(), err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s knowledge pack %s must not be a symbolic link", origin, entry.Name())
		}
		if info.Size() > maxPackBytes {
			return fmt.Errorf("%s knowledge pack %s exceeds %d bytes", origin, entry.Name(), maxPackBytes)
		}
		name := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read %s knowledge pack %s: %w", origin, entry.Name(), err)
		}
		if err := r.add(data, origin, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func Parse(data []byte, origin Origin) (Pack, error) {
	if len(data) > maxPackBytes {
		return Pack{}, fmt.Errorf("knowledge pack exceeds %d bytes", maxPackBytes)
	}
	if secretPattern.Match(data) {
		return Pack{}, fmt.Errorf("knowledge packs must not contain secrets")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var pack Pack
	if err := decoder.Decode(&pack); err != nil {
		return Pack{}, fmt.Errorf("parse knowledge pack: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return Pack{}, fmt.Errorf("parse trailing knowledge pack document: %w", err)
		}
		return Pack{}, fmt.Errorf("knowledge pack must contain exactly one YAML document")
	}
	pack.Origin = origin
	if err := pack.Validate(); err != nil {
		return Pack{}, err
	}
	return pack, nil
}

func (r *Registry) add(data []byte, origin Origin, source string) error {
	pack, err := Parse(data, origin)
	if err != nil {
		return fmt.Errorf("knowledge pack %s: %w", source, err)
	}
	if prior, exists := r.packs[pack.ID]; exists {
		if origin == OriginManaged && prior.Origin == OriginBuiltin {
			r.packs[pack.ID] = pack
			return nil
		}
		return fmt.Errorf("duplicate knowledge pack id %q (%s cannot override %s)", pack.ID, origin, prior.Origin)
	}
	r.packs[pack.ID] = pack
	return nil
}

func (r *Registry) List() []Pack {
	packs := make([]Pack, 0, len(r.packs))
	for _, pack := range r.packs {
		packs = append(packs, pack)
	}
	sort.Slice(packs, func(i, j int) bool { return packs[i].ID < packs[j].ID })
	return packs
}

func (r *Registry) Find(id string) (Pack, bool) {
	pack, ok := r.packs[strings.TrimSpace(id)]
	return pack, ok
}

func (r *Registry) Resolve(identity TechnicalIdentity) []Pack {
	var matches []Pack
	for _, pack := range r.packs {
		for _, candidate := range pack.Identities {
			ownerMatches := identity.Owner == "" || strings.EqualFold(strings.TrimSpace(identity.Owner), strings.TrimSpace(candidate.Owner)) || strings.EqualFold(strings.TrimSpace(identity.Owner), strings.TrimSpace(pack.Owner))
			if ownerMatches && strings.EqualFold(strings.TrimSpace(identity.Kind), strings.TrimSpace(candidate.Kind)) && strings.EqualFold(strings.TrimSpace(identity.Name), strings.TrimSpace(candidate.Name)) {
				matches = append(matches, pack)
				break
			}
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })
	return matches
}

func (r *Registry) ResolveOne(identity TechnicalIdentity) (Pack, error) {
	matches := r.Resolve(identity)
	switch len(matches) {
	case 0:
		return Pack{}, fmt.Errorf("no knowledge pack resolves %s/%s", identity.Kind, identity.Name)
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, len(matches))
		for i := range matches {
			ids[i] = matches[i].ID
		}
		return Pack{}, fmt.Errorf("ambiguous knowledge identity %s/%s: %s", identity.Kind, identity.Name, strings.Join(ids, ", "))
	}
}
