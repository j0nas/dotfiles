package main

import (
	"encoding/json"
	"encoding/xml"
	"path"
	"regexp"
	"sort"
	"strings"
)

type manifest struct {
	Path   string         `json:"path"`
	Kind   string         `json:"kind"`
	Direct int            `json:"direct"`
	Groups map[string]int `json:"groups,omitempty"`
}

type depsSection struct {
	Manifests   []manifest `json:"manifests"`
	DirectTotal int        `json:"direct_total"`
	Lockfiles   []string   `json:"lockfiles"`
	Unavailable string     `json:"unavailable,omitempty"`
}

// Total is the direct dependency count (0 when none could be read).
func (d depsSection) Total() int { return d.DirectTotal }

// MarshalJSON leaves direct_total out when no recognised manifest exists, so
// "unknown" is never recorded as zero.
func (d depsSection) MarshalJSON() ([]byte, error) {
	type plain struct {
		Manifests   []manifest `json:"manifests"`
		DirectTotal *int       `json:"direct_total,omitempty"`
		Lockfiles   []string   `json:"lockfiles"`
		Unavailable string     `json:"unavailable,omitempty"`
	}
	p := plain{Manifests: d.Manifests, Lockfiles: d.Lockfiles, Unavailable: d.Unavailable}
	if d.Unavailable == "" {
		p.DirectTotal = &d.DirectTotal
	}
	return json.Marshal(p)
}

var lockfileNames = map[string]bool{
	"package-lock.json": true, "npm-shrinkwrap.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"bun.lockb": true, "bun.lock": true, "poetry.lock": true, "uv.lock": true, "Pipfile.lock": true,
	"pdm.lock": true, "go.sum": true, "Cargo.lock": true, "Gemfile.lock": true, "composer.lock": true,
	"gradle.lockfile": true, "packages.lock.json": true, "mix.lock": true, "pubspec.lock": true,
	"Podfile.lock": true, "Package.resolved": true, "flake.lock": true, "deno.lock": true,
}

// scanDeps counts the direct dependencies each manifest under root declares.
// files are root-relative paths.
func scanDeps(root string, files []string) depsSection {
	d := depsSection{Manifests: []manifest{}, Lockfiles: []string{}}
	for _, f := range files {
		if isVendoredPath(f) {
			continue
		}
		base := path.Base(f)
		if lockfileNames[base] {
			d.Lockfiles = append(d.Lockfiles, f)
			continue
		}
		var m *manifest
		switch {
		case base == "package.json":
			m = depsPackageJSON(root, f)
		case base == "pyproject.toml":
			m = depsPyproject(root, f)
		case reRequirements.MatchString(f):
			m = depsRequirements(root, f)
		case base == "Pipfile":
			m = depsPipfile(root, f)
		case base == "go.mod":
			m = depsGoMod(root, f)
		case base == "Cargo.toml":
			m = depsCargo(root, f)
		case base == "Gemfile":
			m = depsGemfile(root, f)
		case base == "pom.xml":
			m = depsPom(root, f)
		case base == "build.gradle" || base == "build.gradle.kts":
			m = depsGradle(root, f)
		case base == "composer.json":
			m = depsComposer(root, f)
		case strings.HasSuffix(base, ".csproj") || strings.HasSuffix(base, ".fsproj") || strings.HasSuffix(base, ".vbproj"):
			m = depsCsproj(root, f)
		}
		if m != nil {
			d.Manifests = append(d.Manifests, *m)
			d.DirectTotal += m.Direct
		}
	}
	sort.Strings(d.Lockfiles)
	if len(d.Manifests) == 0 {
		d.Unavailable = "no recognised dependency manifest (package.json, pyproject.toml, requirements*.txt, go.mod, Cargo.toml, Gemfile, pom.xml, build.gradle, composer.json, *.csproj, Pipfile)"
	}
	return d
}

var reRequirements = regexp.MustCompile(`(^|/)requirements[^/]*\.txt$|(^|/)requirements/[^/]+\.txt$`)

func newManifest(p, kind string, groups map[string]int, skip ...string) *manifest {
	m := &manifest{Path: p, Kind: kind, Groups: groups}
	for g, n := range groups {
		if !contains(skip, g) {
			m.Direct += n
		}
	}
	return m
}

func depsPackageJSON(root, f string) *manifest {
	text, ok := readText(root, f, 4<<20)
	if !ok {
		return nil
	}
	var pj struct {
		Dependencies         map[string]any `json:"dependencies"`
		DevDependencies      map[string]any `json:"devDependencies"`
		PeerDependencies     map[string]any `json:"peerDependencies"`
		OptionalDependencies map[string]any `json:"optionalDependencies"`
	}
	if json.Unmarshal([]byte(text), &pj) != nil {
		return nil
	}
	return newManifest(f, "package.json", map[string]int{
		"prod": len(pj.Dependencies), "dev": len(pj.DevDependencies),
		"optional": len(pj.OptionalDependencies), "peer": len(pj.PeerDependencies),
	}, "peer")
}

func tomlKeyCount(doc *tomlDoc, section string, ignore ...string) int {
	n := 0
	for k := range doc.sections[section] {
		if !contains(ignore, k) {
			n++
		}
	}
	return n
}

func depsPyproject(root, f string) *manifest {
	text, ok := readText(root, f, 2<<20)
	if !ok {
		return nil
	}
	doc := parseTOMLLite(text)
	g := map[string]int{}
	if v, ok := doc.sections["project"]["dependencies"]; ok {
		g["prod"] += tomlArrayItems(v)
	}
	for _, s := range doc.sectionsWithPrefix("project.optional-dependencies") {
		for _, v := range doc.sections[s] {
			g["optional"] += tomlArrayItems(v)
		}
	}
	for _, v := range doc.sections["dependency-groups"] {
		g["dev"] += tomlArrayItems(v)
	}
	g["prod"] += tomlKeyCount(doc, "tool.poetry.dependencies", "python")
	g["dev"] += tomlKeyCount(doc, "tool.poetry.dev-dependencies", "python")
	for _, s := range doc.sectionsWithPrefix("tool.poetry") {
		switch {
		case strings.HasPrefix(s, "tool.poetry.group.") && strings.HasSuffix(s, ".dependencies"):
			g["dev"] += tomlKeyCount(doc, s)
		case strings.HasPrefix(s, "tool.poetry.dependencies."):
			g["prod"]++
		}
	}
	for _, v := range doc.sections["tool.pdm.dev-dependencies"] {
		g["dev"] += tomlArrayItems(v)
	}
	if v, ok := doc.sections["tool.uv"]["dev-dependencies"]; ok {
		g["dev"] += tomlArrayItems(v)
	}
	if v, ok := doc.sections["build-system"]["requires"]; ok {
		g["build"] = tomlArrayItems(v)
	}
	if v, ok := doc.sections["tool.hatch.envs.default"]["dependencies"]; ok {
		g["dev"] += tomlArrayItems(v)
	}
	for k, n := range g {
		if n == 0 {
			delete(g, k)
		}
	}
	return newManifest(f, "pyproject.toml", g, "build")
}

func depsRequirements(root, f string) *manifest {
	text, ok := readText(root, f, 2<<20)
	if !ok {
		return nil
	}
	n := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "-e") {
			continue
		}
		n++
	}
	return newManifest(f, "requirements.txt", map[string]int{"prod": n})
}

func depsPipfile(root, f string) *manifest {
	text, ok := readText(root, f, 2<<20)
	if !ok {
		return nil
	}
	doc := parseTOMLLite(text)
	return newManifest(f, "Pipfile", map[string]int{"prod": tomlKeyCount(doc, "packages"), "dev": tomlKeyCount(doc, "dev-packages")})
}

var reGoReq = regexp.MustCompile(`^\s*([^\s/]+(?:/\S*)?)\s+(v\S+)(\s*//\s*indirect)?`)

func depsGoMod(root, f string) *manifest {
	text, ok := readText(root, f, 2<<20)
	if !ok {
		return nil
	}
	direct, indirect := 0, 0
	inBlock := false
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "require ("):
			inBlock = true
			continue
		case inBlock && t == ")":
			inBlock = false
			continue
		case strings.HasPrefix(t, "require "):
			t = strings.TrimPrefix(t, "require ")
		case !inBlock:
			continue
		}
		if m := reGoReq.FindStringSubmatch(t); m != nil {
			if m[3] != "" {
				indirect++
			} else {
				direct++
			}
		}
	}
	return newManifest(f, "go.mod", map[string]int{"prod": direct, "indirect": indirect}, "indirect")
}

func depsCargo(root, f string) *manifest {
	text, ok := readText(root, f, 2<<20)
	if !ok {
		return nil
	}
	doc := parseTOMLLite(text)
	g := map[string]int{}
	for s := range doc.sections {
		switch {
		case s == "dependencies" || strings.HasSuffix(s, ".dependencies") && strings.HasPrefix(s, "target."):
			g["prod"] += len(doc.sections[s])
		case s == "dev-dependencies" || strings.HasSuffix(s, ".dev-dependencies"):
			g["dev"] += len(doc.sections[s])
		case s == "build-dependencies" || strings.HasSuffix(s, ".build-dependencies"):
			g["build"] += len(doc.sections[s])
		case s == "workspace.dependencies":
			g["workspace"] += len(doc.sections[s])
		case strings.HasPrefix(s, "dependencies."):
			g["prod"]++
		case strings.HasPrefix(s, "dev-dependencies."):
			g["dev"]++
		case strings.HasPrefix(s, "build-dependencies."):
			g["build"]++
		}
	}
	for k, n := range g {
		if n == 0 {
			delete(g, k)
		}
	}
	return newManifest(f, "Cargo.toml", g, "workspace")
}

var reGem = regexp.MustCompile(`(?m)^\s*gem\s+['"]`)

func depsGemfile(root, f string) *manifest {
	text, ok := readText(root, f, 2<<20)
	if !ok {
		return nil
	}
	return newManifest(f, "Gemfile", map[string]int{"prod": len(reGem.FindAllString(text, -1))})
}

// xmlDeps counts elements named elem, skipping any that sit under one of the
// skip ancestors. testScope reports whether an element's <scope> says test.
func xmlDeps(text, elem string, skip []string) (prod, test int) {
	dec := xml.NewDecoder(strings.NewReader(text))
	dec.Strict = false
	var stack []string
	scopeOf := map[int]string{}
	depth := 0
	counted := map[int]bool{}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			stack = append(stack, name)
			depth++
			if name == elem {
				skipIt := false
				for _, s := range stack[:len(stack)-1] {
					if contains(skip, s) {
						skipIt = true
					}
				}
				if !skipIt {
					counted[depth] = true
				}
			}
			if name == "scope" {
				var s string
				_ = dec.DecodeElement(&s, &t)
				stack = stack[:len(stack)-1]
				depth--
				if counted[depth] {
					scopeOf[depth] = strings.TrimSpace(s)
				}
			}
		case xml.EndElement:
			if counted[depth] {
				if scopeOf[depth] == "test" {
					test++
				} else {
					prod++
				}
				delete(counted, depth)
				delete(scopeOf, depth)
			}
			stack = stack[:len(stack)-1]
			depth--
		}
	}
	return prod, test
}

func depsPom(root, f string) *manifest {
	text, ok := readText(root, f, 4<<20)
	if !ok {
		return nil
	}
	prod, test := xmlDeps(text, "dependency", []string{"dependencyManagement", "pluginManagement", "plugin"})
	return newManifest(f, "pom.xml", map[string]int{"prod": prod, "test": test})
}

var reGradleDep = regexp.MustCompile(`(?m)^\s*(implementation|api|compileOnly|runtimeOnly|compile|runtime|annotationProcessor|kapt|ksp|classpath|testImplementation|testCompileOnly|testRuntimeOnly|testCompile|androidTestImplementation|debugImplementation|developmentOnly)\s*[\( ]\s*(?:platform\()?['"(a-zA-Z]`)

func depsGradle(root, f string) *manifest {
	text, ok := readText(root, f, 2<<20)
	if !ok {
		return nil
	}
	prod, test := 0, 0
	for _, m := range reGradleDep.FindAllStringSubmatch(text, -1) {
		if strings.HasPrefix(m[1], "test") || strings.HasPrefix(m[1], "androidTest") {
			test++
		} else {
			prod++
		}
	}
	return newManifest(f, "build.gradle", map[string]int{"prod": prod, "test": test})
}

func depsComposer(root, f string) *manifest {
	text, ok := readText(root, f, 2<<20)
	if !ok {
		return nil
	}
	var c struct {
		Require    map[string]any `json:"require"`
		RequireDev map[string]any `json:"require-dev"`
	}
	if json.Unmarshal([]byte(text), &c) != nil {
		return nil
	}
	count := func(m map[string]any) int {
		n := 0
		for k := range m {
			if k != "php" && !strings.HasPrefix(k, "ext-") && !strings.HasPrefix(k, "lib-") {
				n++
			}
		}
		return n
	}
	return newManifest(f, "composer.json", map[string]int{"prod": count(c.Require), "dev": count(c.RequireDev)})
}

var rePackageRef = regexp.MustCompile(`(?i)<PackageReference\b`)

func depsCsproj(root, f string) *manifest {
	text, ok := readText(root, f, 2<<20)
	if !ok {
		return nil
	}
	n := len(rePackageRef.FindAllString(text, -1))
	return newManifest(f, "csproj", map[string]int{"prod": n})
}
