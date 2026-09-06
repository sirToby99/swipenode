// Command compliance generates SwipeNode's deterministic SPDX SBOM and exact
// third-party license bundle from the reviewed machine-readable inventory.
package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

type inventory struct {
	SchemaVersion        string       `json:"schema_version"`
	AuditedProductCommit string       `json:"audited_product_commit"`
	GeneratedAt          string       `json:"generated_at"`
	ReleaseToolchain     toolchain    `json:"release_toolchain"`
	Dependencies         []dependency `json:"dependencies"`
}

type toolchain struct {
	Name                string   `json:"name"`
	Version             string   `json:"version"`
	License             string   `json:"license"`
	LicenseFiles        []string `json:"license_files"`
	CommercialUse       *bool    `json:"commercial_use"`
	Modification        *bool    `json:"modification"`
	Redistribution      *bool    `json:"redistribution"`
	AttributionRequired *bool    `json:"attribution_required"`
	SourceDisclosure    *bool    `json:"source_disclosure"`
	PatentClause        string   `json:"patent_clause"`
	NoticeRequired      *bool    `json:"notice_required"`
	CopyleftScope       string   `json:"copyleft_scope"`
	Risk                string   `json:"risk"`
	Action              string   `json:"action"`
}

type dependency struct {
	Module              string   `json:"module"`
	Version             string   `json:"version"`
	Direct              bool     `json:"direct"`
	ReleaseTargets      []string `json:"release_targets"`
	License             string   `json:"license"`
	LicenseFiles        []string `json:"license_files"`
	GoSum               string   `json:"go_sum"`
	CommercialUse       *bool    `json:"commercial_use"`
	Modification        *bool    `json:"modification"`
	Redistribution      *bool    `json:"redistribution"`
	AttributionRequired *bool    `json:"attribution_required"`
	SourceDisclosure    *bool    `json:"source_disclosure"`
	PatentClause        string   `json:"patent_clause"`
	NoticeRequired      *bool    `json:"notice_required"`
	CopyleftScope       string   `json:"copyleft_scope"`
	Risk                string   `json:"risk"`
	Action              string   `json:"action"`
}

type moduleDownload struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
	Dir     string `json:"Dir"`
	Sum     string `json:"Sum"`
	Error   *struct {
		Err string `json:"Err"`
	} `json:"Error"`
}

type spdxDocument struct {
	SPDXVersion            string                 `json:"spdxVersion"`
	DataLicense            string                 `json:"dataLicense"`
	SPDXID                 string                 `json:"SPDXID"`
	Name                   string                 `json:"name"`
	DocumentNamespace      string                 `json:"documentNamespace"`
	CreationInfo           spdxCreationInfo       `json:"creationInfo"`
	DocumentDescribes      []string               `json:"documentDescribes"`
	Packages               []spdxPackage          `json:"packages"`
	Relationships          []spdxRelationship     `json:"relationships"`
	ExtractedLicensingInfo []spdxExtractedLicense `json:"hasExtractedLicensingInfos"`
}

type spdxCreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPackage struct {
	Name             string            `json:"name"`
	SPDXID           string            `json:"SPDXID"`
	VersionInfo      string            `json:"versionInfo"`
	DownloadLocation string            `json:"downloadLocation"`
	FilesAnalyzed    bool              `json:"filesAnalyzed"`
	LicenseConcluded string            `json:"licenseConcluded"`
	LicenseDeclared  string            `json:"licenseDeclared"`
	CopyrightText    string            `json:"copyrightText"`
	Checksums        []spdxChecksum    `json:"checksums,omitempty"`
	ExternalRefs     []spdxExternalRef `json:"externalRefs,omitempty"`
	Comment          string            `json:"comment,omitempty"`
}

type spdxChecksum struct {
	Algorithm     string `json:"algorithm"`
	ChecksumValue string `json:"checksumValue"`
}

type spdxExternalRef struct {
	Category string `json:"referenceCategory"`
	Type     string `json:"referenceType"`
	Locator  string `json:"referenceLocator"`
}

type spdxRelationship struct {
	Element string `json:"spdxElementId"`
	Type    string `json:"relationshipType"`
	Related string `json:"relatedSpdxElement"`
}

type spdxExtractedLicense struct {
	LicenseID     string   `json:"licenseId"`
	ExtractedText string   `json:"extractedText"`
	Name          string   `json:"name"`
	SeeAlso       []string `json:"seeAlsos,omitempty"`
}

func main() {
	inventoryPath := flag.String("inventory", "docs/compliance/dependency-licenses.json", "reviewed dependency inventory")
	sbomPath := flag.String("sbom", "docs/compliance/sbom.spdx.json", "SPDX 2.3 JSON output")
	noticesPath := flag.String("notices", "docs/compliance/THIRD_PARTY_NOTICES.md", "third-party notice output")
	allowUnresolved := flag.Bool("allow-unresolved", false, "write an audit snapshot even when a dependency has no license evidence")
	flag.Parse()

	inv, raw, err := loadInventory(*inventoryPath)
	if err != nil {
		fatal(err)
	}
	if !*allowUnresolved {
		if issues := unresolvedInventoryIssues(inv); len(issues) > 0 {
			fatal(fmt.Errorf("commercial dependency evidence unresolved for %s", strings.Join(issues, ", ")))
		}
	}
	licenseText, err := os.ReadFile("LICENSE")
	if err != nil {
		fatal(fmt.Errorf("read SwipeNode license: %w", err))
	}
	if err := writeSBOM(*sbomPath, inv, raw, string(licenseText)); err != nil {
		fatal(err)
	}
	unresolved, err := writeNotices(*noticesPath, inv)
	if err != nil {
		fatal(err)
	}
	if len(unresolved) > 0 && !*allowUnresolved {
		fatal(fmt.Errorf("license evidence unresolved for %s", strings.Join(unresolved, ", ")))
	}
}

func unresolvedInventoryIssues(inv inventory) []string {
	var issues []string
	for _, dep := range inv.Dependencies {
		if dep.Risk != "GREEN" || dep.License == "NOASSERTION" || dep.CommercialUse == nil || !*dep.CommercialUse || dep.Modification == nil || !*dep.Modification || dep.Redistribution == nil || !*dep.Redistribution || len(dep.LicenseFiles) == 0 {
			issues = append(issues, dep.Module+"@"+dep.Version)
		}
	}
	sort.Strings(issues)
	return issues
}

func loadInventory(path string) (inventory, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return inventory{}, nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var inv inventory
	if err := decoder.Decode(&inv); err != nil {
		return inventory{}, nil, fmt.Errorf("parse inventory: %w", err)
	}
	if inv.SchemaVersion != "swipenode.license-inventory.v1" || inv.GeneratedAt == "" || inv.AuditedProductCommit == "" || len(inv.Dependencies) == 0 {
		return inventory{}, nil, fmt.Errorf("inventory identity is incomplete")
	}
	seen := map[string]bool{}
	for _, dep := range inv.Dependencies {
		key := dep.Module + "@" + dep.Version
		if dep.Module == "" || dep.Version == "" || dep.License == "" || dep.GoSum == "" || seen[key] {
			return inventory{}, nil, fmt.Errorf("invalid or duplicate dependency %q", key)
		}
		seen[key] = true
	}
	return inv, data, nil
}

func writeSBOM(path string, inv inventory, inventoryBytes []byte, ownLicense string) error {
	digest := sha256.Sum256(inventoryBytes)
	mainID := "SPDXRef-Package-SwipeNode"
	doc := spdxDocument{
		SPDXVersion: "SPDX-2.3", DataLicense: "CC0-1.0", SPDXID: "SPDXRef-DOCUMENT",
		Name:                   "SwipeNode release dependency SBOM",
		DocumentNamespace:      "https://swipenode.dev/sbom/" + hex.EncodeToString(digest[:]),
		CreationInfo:           spdxCreationInfo{Created: inv.GeneratedAt, Creators: []string{"Tool: swipenode-compliance-sbom/1"}},
		DocumentDescribes:      []string{mainID},
		ExtractedLicensingInfo: []spdxExtractedLicense{{LicenseID: "LicenseRef-Functional-Source-License-1.1", ExtractedText: ownLicense, Name: "Functional Source License, Version 1.1"}},
	}
	doc.Packages = append(doc.Packages, spdxPackage{Name: "SwipeNode", SPDXID: mainID, VersionInfo: inv.AuditedProductCommit, DownloadLocation: "NOASSERTION", FilesAnalyzed: false, LicenseConcluded: "LicenseRef-Functional-Source-License-1.1", LicenseDeclared: "LicenseRef-Functional-Source-License-1.1", CopyrightText: "Copyright 2026 SwipeNode Contributors", Comment: "Source audit target; release binaries are built by the pinned release toolchain."})
	toolchainID := "SPDXRef-Package-Go-Standard-Library"
	doc.Packages = append(doc.Packages, spdxPackage{Name: inv.ReleaseToolchain.Name, SPDXID: toolchainID, VersionInfo: inv.ReleaseToolchain.Version, DownloadLocation: "https://go.dev/dl/", FilesAnalyzed: false, LicenseConcluded: inv.ReleaseToolchain.License, LicenseDeclared: inv.ReleaseToolchain.License, CopyrightText: "NOASSERTION", Comment: inv.ReleaseToolchain.Action})
	doc.Relationships = append(doc.Relationships, spdxRelationship{Element: mainID, Type: "DEPENDS_ON", Related: toolchainID})

	dependencies := append([]dependency(nil), inv.Dependencies...)
	sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].Module < dependencies[j].Module })
	for _, dep := range dependencies {
		id := spdxID(dep.Module)
		checksum, err := goSumSHA256(dep.GoSum)
		if err != nil {
			return fmt.Errorf("%s: %w", dep.Module, err)
		}
		comment := fmt.Sprintf("direct=%t; release_targets=%s; risk=%s; action=%s", dep.Direct, strings.Join(dep.ReleaseTargets, ","), dep.Risk, dep.Action)
		doc.Packages = append(doc.Packages, spdxPackage{
			Name: dep.Module, SPDXID: id, VersionInfo: dep.Version, DownloadLocation: "NOASSERTION", FilesAnalyzed: false,
			LicenseConcluded: dep.License, LicenseDeclared: dep.License, CopyrightText: "NOASSERTION",
			Checksums:    []spdxChecksum{{Algorithm: "SHA256", ChecksumValue: checksum}},
			ExternalRefs: []spdxExternalRef{{Category: "PACKAGE-MANAGER", Type: "purl", Locator: "pkg:golang/" + dep.Module + "@" + dep.Version}}, Comment: comment,
		})
		doc.Relationships = append(doc.Relationships, spdxRelationship{Element: mainID, Type: "DEPENDS_ON", Related: id})
	}
	return writeJSONAtomic(path, doc)
}

func writeNotices(path string, inv inventory) ([]string, error) {
	if runtime.Version() != inv.ReleaseToolchain.Version {
		return nil, fmt.Errorf("notice generation requires %s, running %s", inv.ReleaseToolchain.Version, runtime.Version())
	}
	var output strings.Builder
	output.WriteString("# Third-Party Notices\n\n")
	output.WriteString("Generated from `docs/compliance/dependency-licenses.json`. These are exact license artifacts from the pinned release toolchain and Go module archives.\n\n")
	unresolved := []string{}
	writeEvidence := func(name, version, license, base string, files []string) error {
		fmt.Fprintf(&output, "## %s %s\n\nDeclared license: `%s`\n\n", name, version, license)
		if len(files) == 0 {
			output.WriteString("**BLOCKING: the exact dependency archive contains no authoritative license or notice file.**\n\n")
			unresolved = append(unresolved, name+"@"+version)
			return nil
		}
		for _, name := range files {
			clean := filepath.Clean(name)
			if clean == "." || !filepath.IsLocal(clean) {
				return fmt.Errorf("unsafe license path %q", name)
			}
			data, err := os.ReadFile(filepath.Join(base, clean))
			if err != nil {
				return fmt.Errorf("read %s %s: %w", version, name, err)
			}
			fmt.Fprintf(&output, "### `%s`\n\n```text\n%s", filepath.ToSlash(clean), data)
			if len(data) == 0 || data[len(data)-1] != '\n' {
				output.WriteByte('\n')
			}
			output.WriteString("```\n\n")
		}
		return nil
	}
	if err := writeEvidence(inv.ReleaseToolchain.Name, inv.ReleaseToolchain.Version, inv.ReleaseToolchain.License, runtime.GOROOT(), inv.ReleaseToolchain.LicenseFiles); err != nil {
		return nil, err
	}
	dependencies := append([]dependency(nil), inv.Dependencies...)
	sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].Module < dependencies[j].Module })
	for _, dep := range dependencies {
		download, err := downloadModule(dep)
		if err != nil {
			return nil, err
		}
		if err := writeEvidence(dep.Module, dep.Version, dep.License, download.Dir, dep.LicenseFiles); err != nil {
			return nil, err
		}
	}
	notices := strings.TrimRight(output.String(), "\n") + "\n"
	if err := writeFileAtomic(path, []byte(notices), 0o644); err != nil {
		return nil, err
	}
	return unresolved, nil
}

func downloadModule(dep dependency) (moduleDownload, error) {
	command := exec.Command("go", "mod", "download", "-json", dep.Module+"@"+dep.Version)
	data, err := command.Output()
	if err != nil {
		return moduleDownload{}, fmt.Errorf("download %s@%s: %w", dep.Module, dep.Version, err)
	}
	var result moduleDownload
	if err := json.Unmarshal(data, &result); err != nil {
		return moduleDownload{}, err
	}
	if result.Error != nil || result.Dir == "" || result.Sum != dep.GoSum {
		return moduleDownload{}, fmt.Errorf("module archive identity mismatch for %s@%s", dep.Module, dep.Version)
	}
	return result, nil
}

var invalidSPDX = regexp.MustCompile(`[^A-Za-z0-9.-]+`)

func spdxID(module string) string {
	return "SPDXRef-Package-" + strings.Trim(invalidSPDX.ReplaceAllString(module, "-"), "-")
}

func goSumSHA256(value string) (string, error) {
	if !strings.HasPrefix(value, "h1:") {
		return "", errors.New("module checksum is not an h1 digest")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "h1:"))
	if err != nil || len(decoded) != sha256.Size {
		return "", errors.New("module checksum is not a SHA-256 digest")
	}
	return hex.EncodeToString(decoded), nil
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0o644)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".compliance-*.tmp")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "compliance:", err)
	os.Exit(1)
}
