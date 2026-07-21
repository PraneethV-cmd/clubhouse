// Package menu builds a Markdown knowledge graph for a codebase.
package menu

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const manifestName = ".manifest"

type Options struct {
	Root         string
	Out          string
	IncludeTests bool
	Clean        bool
}

type Result struct {
	Root     string
	Out      string
	Packages int
	Symbols  int
	Edges    int
}

type Graph struct {
	Root      string
	Generated time.Time
	Packages  map[string]*Package
	Symbols   map[string]*Symbol
	Edges     []Edge
}

type Package struct {
	ID      string
	Name    string
	Dir     string
	Slug    string
	Files   []string
	Imports map[string]string
	Symbols []string
}

type Symbol struct {
	ID            string
	Kind          string
	Name          string
	Receiver      string
	Signature     string
	Doc           string
	File          string
	Line          int
	PackageID     string
	Slug          string
	Fields        []string
	Calls         []string
	ExternalCalls []string
	rawCalls      []callRef
	imports       map[string]string
}

type Edge struct {
	From string
	To   string
	Kind string
}

type callRef struct {
	Name     string
	Prefix   string
	Selector string
}

// Generate scans Root and writes a navigable Markdown graph into Out.
func Generate(opts Options) (Result, error) {
	if opts.Root == "" {
		opts.Root = "."
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return Result{}, err
	}
	if opts.Out == "" {
		opts.Out = "menu"
	}
	out := opts.Out
	if !filepath.IsAbs(out) {
		out = filepath.Join(root, out)
	}

	g := &Graph{
		Root:      root,
		Generated: time.Now(),
		Packages:  map[string]*Package{},
		Symbols:   map[string]*Symbol{},
	}
	if err := scanGo(g, opts.IncludeTests); err != nil {
		return Result{}, err
	}
	resolveCalls(g)
	dedupeEdges(g)
	if err := writeMarkdown(g, out, opts.Clean); err != nil {
		return Result{}, err
	}
	return Result{
		Root:     root,
		Out:      out,
		Packages: len(g.Packages),
		Symbols:  len(g.Symbols),
		Edges:    len(g.Edges),
	}, nil
}

func scanGo(g *Graph, includeTests bool) error {
	fset := token.NewFileSet()
	return filepath.WalkDir(g.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path == g.Root {
				return nil
			}
			if skipDir(rel(g.Root, path), name) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasPrefix(name, "._") {
			return nil
		}
		if !includeTests && strings.HasSuffix(name, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		relFile := rel(g.Root, path)
		dir := rel(g.Root, filepath.Dir(path))
		p := packageFor(g, dir, file.Name.Name)
		p.Files = appendUnique(p.Files, relFile)

		fileImports := map[string]string{}
		for _, spec := range file.Imports {
			importPath, _ := strconv.Unquote(spec.Path.Value)
			alias := importAlias(spec, importPath)
			fileImports[alias] = importPath
			p.Imports[alias] = importPath
			g.Edges = append(g.Edges, Edge{From: p.ID, To: "dep:" + importPath, Kind: "imports"})
		}

		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				if d.Tok == token.TYPE {
					for _, spec := range d.Specs {
						ts := spec.(*ast.TypeSpec)
						s := typeSymbol(g, p, fset, d, ts, relFile)
						registerSymbol(g, p, s)
					}
				}
			case *ast.FuncDecl:
				s := funcSymbol(g, p, fset, d, relFile, fileImports)
				registerSymbol(g, p, s)
			}
		}
		return nil
	})
}

func packageFor(g *Graph, dir, name string) *Package {
	id := "pkg:" + dir
	if p, ok := g.Packages[id]; ok {
		return p
	}
	slugBase := dir
	if slugBase == "." || slugBase == "" {
		slugBase = "root-" + name
	}
	p := &Package{
		ID:      id,
		Name:    name,
		Dir:     dir,
		Slug:    slug(slugBase),
		Imports: map[string]string{},
	}
	g.Packages[id] = p
	return p
}

func typeSymbol(g *Graph, p *Package, fset *token.FileSet, decl *ast.GenDecl, ts *ast.TypeSpec, file string) *Symbol {
	kind := "type"
	var fields []string
	switch t := ts.Type.(type) {
	case *ast.StructType:
		kind = "struct"
		fields = fieldList(fset, t.Fields)
	case *ast.InterfaceType:
		kind = "interface"
		fields = fieldList(fset, t.Methods)
	}
	id := symbolID(p, "type", "", ts.Name.Name)
	return &Symbol{
		ID:        id,
		Kind:      kind,
		Name:      ts.Name.Name,
		Signature: "type " + ts.Name.Name + " " + nodeString(fset, ts.Type),
		Doc:       docText(firstComment(ts.Doc, decl.Doc)),
		File:      file,
		Line:      fset.Position(ts.Pos()).Line,
		PackageID: p.ID,
		Slug:      symbolSlug(p, "type", "", ts.Name.Name),
		Fields:    fields,
	}
}

func funcSymbol(g *Graph, p *Package, fset *token.FileSet, decl *ast.FuncDecl, file string, imports map[string]string) *Symbol {
	receiver := ""
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		receiver = receiverName(decl.Recv.List[0].Type)
	}
	kind := "function"
	if receiver != "" {
		kind = "method"
	}
	id := symbolID(p, kind, receiver, decl.Name.Name)
	s := &Symbol{
		ID:        id,
		Kind:      kind,
		Name:      decl.Name.Name,
		Receiver:  receiver,
		Signature: signature(fset, decl),
		Doc:       docText(decl.Doc),
		File:      file,
		Line:      fset.Position(decl.Pos()).Line,
		PackageID: p.ID,
		Slug:      symbolSlug(p, kind, receiver, decl.Name.Name),
		imports:   imports,
	}
	if decl.Body != nil {
		ast.Inspect(decl.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if ref, ok := callName(call.Fun); ok {
				s.rawCalls = append(s.rawCalls, ref)
			}
			return true
		})
	}
	return s
}

func registerSymbol(g *Graph, p *Package, s *Symbol) {
	g.Symbols[s.ID] = s
	p.Symbols = appendUnique(p.Symbols, s.ID)
}

func resolveCalls(g *Graph) {
	funcsByPkg := map[string]map[string]string{}
	methodsByPkg := map[string]map[string][]string{}
	for id, s := range g.Symbols {
		if funcsByPkg[s.PackageID] == nil {
			funcsByPkg[s.PackageID] = map[string]string{}
		}
		if methodsByPkg[s.PackageID] == nil {
			methodsByPkg[s.PackageID] = map[string][]string{}
		}
		if s.Kind == "function" {
			funcsByPkg[s.PackageID][s.Name] = id
		}
		if s.Kind == "method" {
			methodsByPkg[s.PackageID][s.Name] = append(methodsByPkg[s.PackageID][s.Name], id)
			methodsByPkg[s.PackageID][s.Receiver+"."+s.Name] = append(methodsByPkg[s.PackageID][s.Receiver+"."+s.Name], id)
		}
	}

	for _, s := range g.Symbols {
		seen := map[string]bool{}
		for _, ref := range s.rawCalls {
			if ref.Prefix != "" {
				if dep, ok := s.imports[ref.Prefix]; ok {
					call := dep + "." + ref.Selector
					s.ExternalCalls = appendUnique(s.ExternalCalls, call)
					g.Edges = append(g.Edges, Edge{From: s.ID, To: "dep:" + dep, Kind: "uses"})
					continue
				}
				if ids := methodsByPkg[s.PackageID][ref.Prefix+"."+ref.Selector]; len(ids) == 1 {
					if !seen[ids[0]] {
						s.Calls = append(s.Calls, ids[0])
						g.Edges = append(g.Edges, Edge{From: s.ID, To: ids[0], Kind: "calls"})
						seen[ids[0]] = true
					}
					continue
				}
			}
			if id := funcsByPkg[s.PackageID][ref.Name]; id != "" && id != s.ID && !seen[id] {
				s.Calls = append(s.Calls, id)
				g.Edges = append(g.Edges, Edge{From: s.ID, To: id, Kind: "calls"})
				seen[id] = true
				continue
			}
			if ids := methodsByPkg[s.PackageID][ref.Name]; len(ids) == 1 && ids[0] != s.ID && !seen[ids[0]] {
				s.Calls = append(s.Calls, ids[0])
				g.Edges = append(g.Edges, Edge{From: s.ID, To: ids[0], Kind: "calls"})
				seen[ids[0]] = true
			}
		}
		sort.Strings(s.Calls)
		sort.Strings(s.ExternalCalls)
	}
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].From == g.Edges[j].From {
			if g.Edges[i].To == g.Edges[j].To {
				return g.Edges[i].Kind < g.Edges[j].Kind
			}
			return g.Edges[i].To < g.Edges[j].To
		}
		return g.Edges[i].From < g.Edges[j].From
	})
}

func writeMarkdown(g *Graph, out string, clean bool) error {
	files := map[string]string{}
	files["README.md"] = renderReadme()
	files["index.md"] = renderIndex(g)
	files["graph.md"] = renderGraph(g)
	files["codex.md"] = renderCodexPrompt()

	for _, p := range sortedPackages(g) {
		files[filepath.Join("packages", p.Slug+".md")] = renderPackage(g, p)
	}
	calledBy := calledByMap(g)
	for _, s := range sortedSymbols(g) {
		files[filepath.Join("symbols", s.Slug+".md")] = renderSymbol(g, s, calledBy[s.ID])
	}

	if clean {
		removeManifestFiles(out)
	}
	for rel, body := range files {
		path := filepath.Join(out, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
	}
	if err := writeManifest(out, files); err != nil {
		return err
	}
	removeAppleDouble(out)
	return nil
}

func dedupeEdges(g *Graph) {
	seen := map[Edge]bool{}
	out := make([]Edge, 0, len(g.Edges))
	for _, e := range g.Edges {
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	g.Edges = out
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].From == g.Edges[j].From {
			if g.Edges[i].To == g.Edges[j].To {
				return g.Edges[i].Kind < g.Edges[j].Kind
			}
			return g.Edges[i].To < g.Edges[j].To
		}
		return g.Edges[i].From < g.Edges[j].From
	})
}

func removeAppleDouble(out string) {
	filepath.WalkDir(out, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), "._") {
			os.Remove(path)
		}
		return nil
	})
}

func renderReadme() string {
	return `# Menu

This folder is generated by ` + "`clubhouse menu`" + `.

Open ` + "`index.md`" + ` first. The files use normal Markdown links plus Obsidian-style
wikilinks, so they work in any Markdown viewer and can also be imported into graph
tools later.

- ` + "`index.md`" + `: codebase overview
- ` + "`graph.md`" + `: Mermaid dependency graph
- ` + "`packages/`" + `: package pages
- ` + "`symbols/`" + `: type, function, and method pages
- ` + "`codex.md`" + `: prompt for an optional AI enrichment pass
`
}

func renderIndex(g *Graph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Code Menu\n\n")
	fmt.Fprintf(&b, "- Root: `%s`\n", g.Root)
	fmt.Fprintf(&b, "- Generated: `%s`\n", g.Generated.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Packages: `%d`\n", len(g.Packages))
	fmt.Fprintf(&b, "- Symbols: `%d`\n", len(g.Symbols))
	fmt.Fprintf(&b, "- Edges: `%d`\n\n", len(g.Edges))
	b.WriteString("## Navigation\n\n")
	b.WriteString("- [[graph|Graph]] ([open](graph.md))\n")
	b.WriteString("- [[codex|Codex enrichment prompt]] ([open](codex.md))\n\n")
	b.WriteString("## Packages\n\n")
	for _, p := range sortedPackages(g) {
		fmt.Fprintf(&b, "- %s ([open](packages/%s.md)) - `%s` - %d symbols\n", wiki(p.Slug, displayPackage(p)), p.Slug, p.Dir, len(p.Symbols))
	}
	return b.String()
}

func renderGraph(g *Graph) string {
	var b strings.Builder
	b.WriteString("# Graph\n\n")
	b.WriteString("```mermaid\ngraph TD\n")
	count := 0
	for _, e := range g.Edges {
		if e.Kind != "calls" && e.Kind != "imports" {
			continue
		}
		from := mermaidID(e.From)
		to := mermaidID(e.To)
		fmt.Fprintf(&b, "  %s[\"%s\"] -->|%s| %s[\"%s\"]\n", from, mermaidLabel(g, e.From), e.Kind, to, mermaidLabel(g, e.To))
		count++
		if count >= 240 {
			b.WriteString("  omitted[\"graph trimmed after 240 edges\"]\n")
			break
		}
	}
	if count == 0 {
		b.WriteString("  empty[\"No graph edges found\"]\n")
	}
	b.WriteString("```\n")
	return b.String()
}

func renderPackage(g *Graph, p *Package) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntype: package\nname: %s\npath: %s\n---\n\n", p.Name, p.Dir)
	fmt.Fprintf(&b, "# %s\n\n", displayPackage(p))
	b.WriteString("[[index|Back to index]] ([open](../index.md))\n\n")
	b.WriteString("## Files\n\n")
	for _, file := range sortedStrings(p.Files) {
		fmt.Fprintf(&b, "- `%s`\n", file)
	}
	b.WriteString("\n## Imports\n\n")
	if len(p.Imports) == 0 {
		b.WriteString("- none\n")
	} else {
		for _, dep := range sortedMapValues(p.Imports) {
			fmt.Fprintf(&b, "- `%s`\n", dep)
		}
	}
	b.WriteString("\n## Symbols\n\n")
	for _, id := range sortedStrings(p.Symbols) {
		s := g.Symbols[id]
		fmt.Fprintf(&b, "- %s ([open](../symbols/%s.md)) `%s` - %s\n", wiki(s.Slug, s.DisplayName()), s.Slug, s.Kind, firstSentence(s.Doc))
	}
	return b.String()
}

func renderSymbol(g *Graph, s *Symbol, calledBy []string) string {
	var b strings.Builder
	p := g.Packages[s.PackageID]
	fmt.Fprintf(&b, "---\ntype: %s\nname: %s\npackage: %s\n---\n\n", s.Kind, s.DisplayName(), p.Dir)
	fmt.Fprintf(&b, "# %s\n\n", s.DisplayName())
	fmt.Fprintf(&b, "- Kind: `%s`\n", s.Kind)
	fmt.Fprintf(&b, "- Package: %s ([open](../packages/%s.md))\n", wiki(p.Slug, displayPackage(p)), p.Slug)
	fmt.Fprintf(&b, "- Source: `%s:%d`\n", s.File, s.Line)
	fmt.Fprintf(&b, "- Signature: `%s`\n\n", s.Signature)
	b.WriteString("## What It Does\n\n")
	if s.Doc == "" {
		b.WriteString("No comment found. This page was generated from the code shape and call graph.\n")
	} else {
		b.WriteString(s.Doc + "\n")
	}
	if len(s.Fields) > 0 {
		b.WriteString("\n## Shape\n\n")
		for _, field := range s.Fields {
			fmt.Fprintf(&b, "- `%s`\n", field)
		}
	}
	b.WriteString("\n## Calls\n\n")
	if len(s.Calls) == 0 && len(s.ExternalCalls) == 0 {
		b.WriteString("- none detected\n")
	} else {
		for _, id := range s.Calls {
			if target := g.Symbols[id]; target != nil {
				fmt.Fprintf(&b, "- %s ([open](%s.md))\n", wiki(target.Slug, target.DisplayName()), target.Slug)
			}
		}
		for _, ext := range s.ExternalCalls {
			fmt.Fprintf(&b, "- `%s`\n", ext)
		}
	}
	b.WriteString("\n## Called By\n\n")
	if len(calledBy) == 0 {
		b.WriteString("- none detected\n")
	} else {
		for _, id := range sortedStrings(calledBy) {
			if source := g.Symbols[id]; source != nil {
				fmt.Fprintf(&b, "- %s ([open](%s.md))\n", wiki(source.Slug, source.DisplayName()), source.Slug)
			}
		}
	}
	return b.String()
}

func renderCodexPrompt() string {
	return strings.Join([]string{
		"# Codex Enrichment Prompt",
		"",
		"Use this prompt when you want Codex to enrich the generated menu with human-level",
		"summaries after `clubhouse menu` has created the structural graph.",
		"",
		"```text",
		"Read menu/index.md, menu/graph.md, and the relevant menu/packages and menu/symbols",
		"pages. Improve the \"What It Does\" sections with concise, factual summaries.",
		"Preserve existing links, front matter, source paths, and call graph sections.",
		"Do not invent behavior that is not supported by the source code.",
		"```",
		"",
	}, "\n")
}

func calledByMap(g *Graph) map[string][]string {
	out := map[string][]string{}
	for _, e := range g.Edges {
		if e.Kind == "calls" {
			out[e.To] = appendUnique(out[e.To], e.From)
		}
	}
	return out
}

func removeManifestFiles(out string) {
	b, err := os.ReadFile(filepath.Join(out, manifestName))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		path := filepath.Clean(filepath.Join(out, line))
		if !strings.HasPrefix(path, filepath.Clean(out)+string(filepath.Separator)) {
			continue
		}
		os.Remove(path)
	}
}

func writeManifest(out string, files map[string]string) error {
	var names []string
	for name := range files {
		names = append(names, filepath.ToSlash(name))
	}
	sort.Strings(names)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, manifestName), []byte(strings.Join(names, "\n")+"\n"), 0o644)
}

func skipDir(relDir, name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".clubhouse", ".context", "vendor", "node_modules", "dist", "build", ".next":
		return true
	}
	if relDir == "menu" || relDir == "site/public" || relDir == "site/resources" || relDir == "site/static/releases" {
		return true
	}
	return strings.HasPrefix(name, "._")
}

func importAlias(spec *ast.ImportSpec, importPath string) string {
	if spec.Name != nil {
		return spec.Name.Name
	}
	parts := strings.Split(importPath, "/")
	return parts[len(parts)-1]
}

func fieldList(fset *token.FileSet, fields *ast.FieldList) []string {
	if fields == nil {
		return nil
	}
	var out []string
	for _, field := range fields.List {
		typ := nodeString(fset, field.Type)
		if len(field.Names) == 0 {
			out = append(out, typ)
			continue
		}
		for _, name := range field.Names {
			out = append(out, name.Name+" "+typ)
		}
	}
	return out
}

func callName(expr ast.Expr) (callRef, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		return callRef{Name: e.Name}, true
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			return callRef{Name: e.Sel.Name, Prefix: x.Name, Selector: e.Sel.Name}, true
		}
		return callRef{Name: e.Sel.Name, Selector: e.Sel.Name}, true
	default:
		return callRef{}, false
	}
}

func signature(fset *token.FileSet, decl *ast.FuncDecl) string {
	t := nodeString(fset, decl.Type)
	t = strings.TrimPrefix(t, "func")
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		return "func (" + nodeString(fset, decl.Recv.List[0].Type) + ") " + decl.Name.Name + t
	}
	return "func " + decl.Name.Name + t
}

func receiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return receiverName(e.X)
	case *ast.IndexExpr:
		return receiverName(e.X)
	case *ast.IndexListExpr:
		return receiverName(e.X)
	default:
		return strings.TrimPrefix(nodeString(token.NewFileSet(), expr), "*")
	}
}

func nodeString(fset *token.FileSet, node any) string {
	var b bytes.Buffer
	_ = printer.Fprint(&b, fset, node)
	return b.String()
}

func docText(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	return strings.TrimSpace(cg.Text())
}

func firstComment(groups ...*ast.CommentGroup) *ast.CommentGroup {
	for _, group := range groups {
		if group != nil {
			return group
		}
	}
	return nil
}

func symbolID(p *Package, kind, receiver, name string) string {
	if receiver != "" {
		return p.ID + ":" + kind + ":" + receiver + "." + name
	}
	return p.ID + ":" + kind + ":" + name
}

func symbolSlug(p *Package, kind, receiver, name string) string {
	if receiver != "" {
		return slug(p.Dir + "-" + kind + "-" + receiver + "-" + name)
	}
	return slug(p.Dir + "-" + kind + "-" + name)
}

func (s *Symbol) DisplayName() string {
	if s.Receiver != "" {
		return s.Receiver + "." + s.Name
	}
	return s.Name
}

func displayPackage(p *Package) string {
	if p.Dir == "." || p.Dir == "" {
		return p.Name
	}
	return p.Dir
}

func wiki(slug, label string) string {
	return fmt.Sprintf("[[%s|%s]]", slug, label)
}

func firstSentence(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if s == "" {
		return "No comment found."
	}
	if i := strings.Index(s, ". "); i > 0 {
		return s[:i+1]
	}
	return s
}

func slug(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "." || s == "" {
		s = "root"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := unicode.IsLetter(r) || unicode.IsDigit(r)
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "root"
	}
	return out
}

func sortedPackages(g *Graph) []*Package {
	out := make([]*Package, 0, len(g.Packages))
	for _, p := range g.Packages {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return displayPackage(out[i]) < displayPackage(out[j]) })
	return out
}

func sortedSymbols(g *Graph) []*Symbol {
	out := make([]*Symbol, 0, len(g.Symbols))
	for _, s := range g.Symbols {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PackageID == out[j].PackageID {
			return out[i].DisplayName() < out[j].DisplayName()
		}
		return out[i].PackageID < out[j].PackageID
	})
	return out
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func sortedMapValues(m map[string]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range m {
		if !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	sort.Strings(out)
	return out
}

func appendUnique(in []string, s string) []string {
	for _, v := range in {
		if v == s {
			return in
		}
	}
	return append(in, s)
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	r = filepath.ToSlash(r)
	if r == "" {
		return "."
	}
	return r
}

func mermaidID(id string) string {
	return "n_" + slug(id)
}

func mermaidLabel(g *Graph, id string) string {
	if p, ok := g.Packages[id]; ok {
		return displayPackage(p)
	}
	if s, ok := g.Symbols[id]; ok {
		return s.DisplayName()
	}
	return strings.TrimPrefix(id, "dep:")
}
