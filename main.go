package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
)

type ImportFromTarget struct {
	Module string
	Name   string
}

type ModelRef struct {
	Module string
	Name   string
}

func (m ModelRef) String() string {
	return m.Module + "." + m.Name
}

type CallTarget struct {
	Kind string
	Name string
	Base string
	Attr string
}

type ModuleInfo struct {
	ModulePath    string
	FilePath      string
	Tree          *sitter.Tree
	IsPackage     bool
	ModuleImports map[string]string
	FromImports   map[string]ImportFromTarget
	Functions     map[string]*sitter.Node
	Classes       map[string]map[string]*sitter.Node
	Source        []byte
}

func getRoot() string {
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Clean(cwd)
	}
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "../.."))
}

func buildModuleMap(srcRoot string) (map[string]string, error) {
	moduleMap := make(map[string]string)
	err := filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".py") {
			return nil
		}
		relPath, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		parts := strings.Split(relPath, string(filepath.Separator))
		if len(parts) == 0 {
			return nil
		}
		if parts[len(parts)-1] == "__init__.py" {
			parts = parts[:len(parts)-1]
		} else {
			parts[len(parts)-1] = strings.TrimSuffix(parts[len(parts)-1], ".py")
		}
		if len(parts) == 0 {
			return nil
		}
		modulePath := strings.Join(parts, ".")
		if modulePath != "" {
			moduleMap[modulePath] = path
		}
		return nil
	})
	return moduleMap, err
}

func buildPathToModuleMap(moduleMap map[string]string) map[string]string {
	pathToModule := make(map[string]string, len(moduleMap))
	for modulePath, filePath := range moduleMap {
		cleanPath := filepath.Clean(filePath)
		pathToModule[cleanPath] = modulePath
	}
	return pathToModule
}

func resolveEntrypointModule(moduleSpec string, srcRoot string, moduleMap map[string]string, pathToModule map[string]string) (string, bool) {
	if moduleSpec == "" {
		return "", false
	}
	if _, ok := moduleMap[moduleSpec]; ok {
		return moduleSpec, true
	}
	cleanSpec := filepath.Clean(moduleSpec)
	candidates := []string{}
	if filepath.IsAbs(cleanSpec) {
		candidates = append(candidates, cleanSpec)
	} else {
		candidates = append(candidates, filepath.Clean(filepath.Join(srcRoot, cleanSpec)))
		candidates = append(candidates, cleanSpec)
	}
	for _, candidate := range candidates {
		if modulePath, ok := pathToModule[candidate]; ok {
			return modulePath, true
		}
		if !strings.HasSuffix(candidate, ".py") {
			pyCandidate := candidate + ".py"
			if modulePath, ok := pathToModule[pyCandidate]; ok {
				return modulePath, true
			}
		}
	}
	return "", false
}

func resolveRelativeImport(currentModule string, module string, level int, isPackage bool) string {
	if level == 0 {
		return module
	}

	base := ""
	if isPackage {
		base = currentModule
	} else if strings.Contains(currentModule, ".") {
		base = currentModule[:strings.LastIndex(currentModule, ".")]
	}

	var baseParts []string
	if base != "" {
		baseParts = strings.Split(base, ".")
	}

	if level-1 > len(baseParts) {
		return ""
	}
	baseParts = baseParts[:len(baseParts)-(level-1)]
	if module != "" {
		baseParts = append(baseParts, strings.Split(module, ".")...)
	}
	if len(baseParts) == 0 {
		return ""
	}
	return strings.Join(baseParts, ".")
}

func isModelModule(modulePath string) bool {
	if modulePath == "" {
		return false
	}
	for _, part := range strings.Split(modulePath, ".") {
		if part == "models" {
			return true
		}
	}
	return false
}

func parseModule(modulePath string, filePath string) (*ModuleInfo, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree := parser.Parse(nil, content)

	info := &ModuleInfo{
		ModulePath:    modulePath,
		FilePath:      filePath,
		Tree:          tree,
		IsPackage:     filepath.Base(filePath) == "__init__.py",
		ModuleImports: map[string]string{},
		FromImports:   map[string]ImportFromTarget{},
		Functions:     map[string]*sitter.Node{},
		Classes:       map[string]map[string]*sitter.Node{},
		Source:        content,
	}

	collectDefinitions(info)
	moduleImports, fromImports := collectImports(tree.RootNode(), info, content)
	info.ModuleImports = moduleImports
	info.FromImports = fromImports
	return info, nil
}

func collectDefinitions(info *ModuleInfo) {
	root := info.Tree.RootNode()
	var walk func(node *sitter.Node, currentClass string)
	walk = func(node *sitter.Node, currentClass string) {
		if node == nil {
			return
		}
		switch node.Type() {
		case "decorated_definition":
			def := node.ChildByFieldName("definition")
			if def != nil {
				walk(def, currentClass)
				return
			}
		case "class_definition":
			nameNode := node.ChildByFieldName("name")
			className := ""
			if nameNode != nil {
				className = nodeText(info.Source, nameNode)
			}
			if className != "" {
				if _, ok := info.Classes[className]; !ok {
					info.Classes[className] = map[string]*sitter.Node{}
				}
			}
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				walk(child, className)
			}
			return
		case "function_definition":
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				name := nodeText(info.Source, nameNode)
				if name != "" {
					if currentClass != "" {
						if _, ok := info.Classes[currentClass]; !ok {
							info.Classes[currentClass] = map[string]*sitter.Node{}
						}
						info.Classes[currentClass][name] = node
					} else {
						info.Functions[name] = node
					}
				}
			}
		}

		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			walk(child, currentClass)
		}
	}
	walk(root, "")
}

type importAlias struct {
	Name string
	As   string
}

func collectImports(node *sitter.Node, moduleInfo *ModuleInfo, source []byte) (map[string]string, map[string]ImportFromTarget) {
	moduleImports := map[string]string{}
	fromImports := map[string]ImportFromTarget{}

	walk(node, func(n *sitter.Node) {
		switch n.Type() {
		case "import_statement":
			text := nodeText(source, n)
			aliases := parseImportStatement(text)
			for _, alias := range aliases {
				name := alias.Name
				asname := alias.As
				if asname == "" {
					asname = name
				}
				moduleImports[asname] = name
			}
		case "import_from_statement":
			text := nodeText(source, n)
			module, level, aliases, ok := parseImportFromStatement(text)
			if !ok {
				return
			}
			if module == "" && level == 0 {
				return
			}
			resolved := resolveRelativeImport(moduleInfo.ModulePath, module, level, moduleInfo.IsPackage)
			if resolved == "" {
				return
			}
			for _, alias := range aliases {
				if alias.Name == "*" {
					continue
				}
				asname := alias.As
				if asname == "" {
					asname = alias.Name
				}
				fromImports[asname] = ImportFromTarget{Module: resolved, Name: alias.Name}
			}
		}
	})

	return moduleImports, fromImports
}

func parseImportStatement(text string) []importAlias {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "import ") {
		return nil
	}
	rest := strings.TrimSpace(strings.TrimPrefix(text, "import "))
	if rest == "" {
		return nil
	}
	parts := splitCommaSeparated(rest)
	aliases := make([]importAlias, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		name, asname := splitAs(part)
		aliases = append(aliases, importAlias{Name: name, As: asname})
	}
	return aliases
}

func parseImportFromStatement(text string) (string, int, []importAlias, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "from ") {
		return "", 0, nil, false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(text, "from "))
	parts := strings.SplitN(rest, " import ", 2)
	if len(parts) != 2 {
		return "", 0, nil, false
	}
	left := strings.TrimSpace(parts[0])
	right := strings.TrimSpace(parts[1])
	right = strings.TrimSuffix(strings.TrimPrefix(right, "("), ")")

	level := 0
	for level < len(left) && left[level] == '.' {
		level++
	}
	module := strings.TrimSpace(left[level:])
	aliases := parseImportTargets(right)
	return module, level, aliases, true
}

func parseImportTargets(text string) []importAlias {
	parts := splitCommaSeparated(text)
	aliases := make([]importAlias, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		name, asname := splitAs(part)
		aliases = append(aliases, importAlias{Name: name, As: asname})
	}
	return aliases
}

func splitCommaSeparated(text string) []string {
	rawParts := strings.Split(text, ",")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		parts = append(parts, strings.TrimSpace(part))
	}
	return parts
}

func splitAs(part string) (string, string) {
	part = strings.TrimSpace(part)
	if part == "" {
		return "", ""
	}
	tokens := strings.Split(part, " as ")
	if len(tokens) == 2 {
		return strings.TrimSpace(tokens[0]), strings.TrimSpace(tokens[1])
	}
	return part, ""
}

type usageInfo struct {
	Names map[string]struct{}
	Attrs [][2]string
}

func analyzeFunctionModels(functionNode *sitter.Node, moduleInfo *ModuleInfo, moduleMap map[string]string) map[ModelRef]struct{} {
	localModuleImports, localFromImports := collectImports(functionNode, moduleInfo, moduleInfo.Source)

	moduleImports := map[string]string{}
	for k, v := range moduleInfo.ModuleImports {
		moduleImports[k] = v
	}
	for k, v := range localModuleImports {
		moduleImports[k] = v
	}

	fromImports := map[string]ImportFromTarget{}
	for k, v := range moduleInfo.FromImports {
		fromImports[k] = v
	}
	for k, v := range localFromImports {
		fromImports[k] = v
	}

	usage := collectUsage(functionNode, moduleInfo.Source)
	models := map[ModelRef]struct{}{}

	for name := range usage.Names {
		if target, ok := fromImports[name]; ok {
			modulePath := target.Module
			if isModelModule(modulePath) {
				models[ModelRef{Module: modulePath, Name: target.Name}] = struct{}{}
			}
		}
	}

	for _, item := range usage.Attrs {
		base := item[0]
		attr := item[1]
		if modulePath, ok := moduleImports[base]; ok {
			if isModelModule(modulePath) {
				models[ModelRef{Module: modulePath, Name: attr}] = struct{}{}
			}
			continue
		}
		if target, ok := fromImports[base]; ok {
			modulePath := target.Module + "." + target.Name
			if isModelModule(modulePath) {
				if _, ok := moduleMap[modulePath]; ok {
					models[ModelRef{Module: modulePath, Name: attr}] = struct{}{}
				} else if isModelModule(target.Module) {
					models[ModelRef{Module: target.Module, Name: target.Name}] = struct{}{}
				}
			}
		}
	}

	return models
}

func collectUsage(functionNode *sitter.Node, source []byte) usageInfo {
	usage := usageInfo{
		Names: map[string]struct{}{},
		Attrs: make([][2]string, 0),
	}
	walk(functionNode, func(n *sitter.Node) {
		switch n.Type() {
		case "attribute":
			obj := n.ChildByFieldName("object")
			attr := n.ChildByFieldName("attribute")
			if obj != nil && attr != nil && obj.Type() == "identifier" && attr.Type() == "identifier" {
				usage.Attrs = append(usage.Attrs, [2]string{nodeText(source, obj), nodeText(source, attr)})
			}
		case "identifier":
			if shouldCountIdentifier(n) {
				usage.Names[nodeText(source, n)] = struct{}{}
			}
		}
	})
	return usage
}

func shouldCountIdentifier(node *sitter.Node) bool {
	parent := node.Parent()
	if parent == nil {
		return true
	}
	switch parent.Type() {
	case "function_definition", "class_definition":
		if fieldName(parent, node) == "name" {
			return false
		}
	case "import_statement", "import_from_statement", "aliased_import":
		return false
	case "parameters", "default_parameter", "typed_parameter", "list_splat_pattern", "dictionary_splat_pattern":
		return false
	case "keyword_argument":
		if fieldName(parent, node) == "name" {
			return false
		}
	}
	return true
}

func analyzeFunctionCalls(functionNode *sitter.Node, source []byte) []CallTarget {
	calls := []CallTarget{}
	walk(functionNode, func(n *sitter.Node) {
		if n.Type() != "call" {
			return
		}
		fnNode := n.ChildByFieldName("function")
		if fnNode == nil {
			return
		}
		switch fnNode.Type() {
		case "identifier":
			calls = append(calls, CallTarget{Kind: "name", Name: nodeText(source, fnNode)})
		case "attribute":
			obj := fnNode.ChildByFieldName("object")
			attr := fnNode.ChildByFieldName("attribute")
			if obj != nil && attr != nil && obj.Type() == "identifier" && attr.Type() == "identifier" {
				calls = append(calls, CallTarget{Kind: "attr", Base: nodeText(source, obj), Attr: nodeText(source, attr)})
			}
		}
	})
	return calls
}

func resolveCallTarget(call CallTarget, moduleInfo *ModuleInfo, moduleMap map[string]string, currentClass string) (string, string, string, bool) {
	if call.Kind == "name" && call.Name != "" {
		if fn, ok := moduleInfo.Functions[call.Name]; ok && fn != nil {
			return moduleInfo.ModulePath, "", call.Name, true
		}
		if target, ok := moduleInfo.FromImports[call.Name]; ok {
			if _, ok := moduleMap[target.Module]; ok {
				return target.Module, "", target.Name, true
			}
		}
	} else if call.Kind == "attr" && call.Base != "" && call.Attr != "" {
		if (call.Base == "self" || call.Base == "cls") && currentClass != "" {
			methods := moduleInfo.Classes[currentClass]
			if _, ok := methods[call.Attr]; ok {
				return moduleInfo.ModulePath, currentClass, call.Attr, true
			}
		}
		if modulePath, ok := moduleInfo.ModuleImports[call.Base]; ok {
			if _, ok := moduleMap[modulePath]; ok {
				return modulePath, "", call.Attr, true
			}
		}
		if target, ok := moduleInfo.FromImports[call.Base]; ok {
			modulePath := target.Module + "." + target.Name
			if _, ok := moduleMap[modulePath]; ok {
				return modulePath, "", call.Attr, true
			}
		}
		if methods, ok := moduleInfo.Classes[call.Base]; ok {
			if _, ok := methods[call.Attr]; ok {
				return moduleInfo.ModulePath, call.Base, call.Attr, true
			}
		}
	}
	return "", "", "", false
}

func getEntrySeeds(moduleInfo *ModuleInfo, entryObject string, entryClass string, entryMethod string) [][3]string {
	seeds := make([][3]string, 0)
	if entryClass != "" {
		if entryMethod != "" {
			if methods, ok := moduleInfo.Classes[entryClass]; ok {
				if _, ok := methods[entryMethod]; ok {
					seeds = append(seeds, [3]string{moduleInfo.ModulePath, entryClass, entryMethod})
				}
			}
			return seeds
		}
		if methods, ok := moduleInfo.Classes[entryClass]; ok {
			for method := range methods {
				seeds = append(seeds, [3]string{moduleInfo.ModulePath, entryClass, method})
			}
		}
		return seeds
	}

	if entryObject == "" {
		for fn := range moduleInfo.Functions {
			seeds = append(seeds, [3]string{moduleInfo.ModulePath, "", fn})
		}
		for className, methods := range moduleInfo.Classes {
			for method := range methods {
				seeds = append(seeds, [3]string{moduleInfo.ModulePath, className, method})
			}
		}
		return seeds
	}

	if _, ok := moduleInfo.Functions[entryObject]; ok {
		seeds = append(seeds, [3]string{moduleInfo.ModulePath, "", entryObject})
		return seeds
	}
	if methods, ok := moduleInfo.Classes[entryObject]; ok {
		for method := range methods {
			seeds = append(seeds, [3]string{moduleInfo.ModulePath, entryObject, method})
		}
		return seeds
	}
	return seeds
}

func collectModelsForEntrypoint(entrypoint string, srcRoot string) (map[ModelRef]struct{}, map[ModelRef]map[string]struct{}, []string) {
	moduleMap, err := buildModuleMap(srcRoot)
	if err != nil {
		return map[ModelRef]struct{}{}, map[ModelRef]map[string]struct{}{}, []string{err.Error()}
	}
	pathToModule := buildPathToModuleMap(moduleMap)
	moduleSpec := entrypoint
	entryObject := ""
	if strings.Contains(entrypoint, ":") {
		parts := strings.SplitN(entrypoint, ":", 2)
		moduleSpec = parts[0]
		entryObject = parts[1]
	}
	modulePath := moduleSpec
	if resolved, ok := resolveEntrypointModule(moduleSpec, srcRoot, moduleMap, pathToModule); ok {
		modulePath = resolved
	}
	if _, ok := moduleMap[modulePath]; !ok {
		return map[ModelRef]struct{}{}, map[ModelRef]map[string]struct{}{}, []string{fmt.Sprintf("Module not found: %s", moduleSpec)}
	}
	entryClass := ""
	entryMethod := ""
	if entryObject != "" && strings.Contains(entryObject, "::") {
		parts := strings.SplitN(entryObject, "::", 2)
		entryClass = parts[0]
		entryMethod = parts[1]
		entryObject = ""
	}

	moduleCache := map[string]*ModuleInfo{}
	getModuleInfo := func(path string) (*ModuleInfo, error) {
		if info, ok := moduleCache[path]; ok {
			return info, nil
		}
		info, err := parseModule(path, moduleMap[path])
		if err != nil {
			return nil, err
		}
		moduleCache[path] = info
		return info, nil
	}

	entryModule, err := getModuleInfo(modulePath)
	if err != nil {
		return map[ModelRef]struct{}{}, map[ModelRef]map[string]struct{}{}, []string{err.Error()}
	}
	seeds := getEntrySeeds(entryModule, entryObject, entryClass, entryMethod)
	if len(seeds) == 0 {
		entryLabel := entryObject
		if entryClass != "" {
			if entryMethod != "" {
				entryLabel = entryClass + "::" + entryMethod
			} else {
				entryLabel = entryClass
			}
		}
		if entryLabel == "" {
			entryLabel = "(module)"
		}
		return map[ModelRef]struct{}{}, map[ModelRef]map[string]struct{}{}, []string{fmt.Sprintf("Entrypoint object not found: %s in %s", entryLabel, modulePath)}
	}

	models := map[ModelRef]struct{}{}
	modelUsage := map[ModelRef]map[string]struct{}{}
	errors := []string{}
	queue := make([][3]string, 0, len(seeds))
	queue = append(queue, seeds...)
	visited := map[[3]string]struct{}{}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		moduleName := current[0]
		className := current[1]
		funcName := current[2]
		if funcName == "" {
			continue
		}
		key := [3]string{moduleName, className, funcName}
		if _, ok := visited[key]; ok {
			continue
		}
		visited[key] = struct{}{}

		if _, ok := moduleMap[moduleName]; !ok {
			continue
		}
		moduleInfo, err := getModuleInfo(moduleName)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		var funcNode *sitter.Node
		if className != "" {
			if methods, ok := moduleInfo.Classes[className]; ok {
				funcNode = methods[funcName]
			}
		} else {
			funcNode = moduleInfo.Functions[funcName]
		}
		if funcNode == nil {
			errors = append(errors, fmt.Sprintf("Function not found: %s:%s.%s", moduleName, className, funcName))
			continue
		}

		foundModels := analyzeFunctionModels(funcNode, moduleInfo, moduleMap)
		usageKey := fmt.Sprintf("%s:%s%s", moduleInfo.ModulePath, funcClassPrefix(className), funcName)
		for model := range foundModels {
			models[model] = struct{}{}
			if _, ok := modelUsage[model]; !ok {
				modelUsage[model] = map[string]struct{}{}
			}
			modelUsage[model][usageKey] = struct{}{}
		}

		for _, call := range analyzeFunctionCalls(funcNode, moduleInfo.Source) {
			targetModule, targetClass, targetFunc, ok := resolveCallTarget(call, moduleInfo, moduleMap, className)
			if !ok || targetFunc == "" {
				continue
			}
			if _, ok := moduleMap[targetModule]; ok {
				queue = append(queue, [3]string{targetModule, targetClass, targetFunc})
			}
		}
	}

	return models, modelUsage, errors
}

func funcClassPrefix(className string) string {
	if className == "" {
		return ""
	}
	return className + "."
}

func nodeText(source []byte, node *sitter.Node) string {
	if node == nil {
		return ""
	}
	return string(source[node.StartByte():node.EndByte()])
}

func fieldName(parent *sitter.Node, child *sitter.Node) string {
	if parent == nil || child == nil {
		return ""
	}
	for i := 0; i < int(parent.ChildCount()); i++ {
		if parent.Child(i).ID() == child.ID() {
			return parent.FieldNameForChild(i)
		}
	}
	return ""
}

func walk(node *sitter.Node, fn func(*sitter.Node)) {
	if node == nil {
		return
	}
	fn(node)
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		walk(child, fn)
	}
}

func main() {
	entrypoint := flag.String("entrypoint", "", "Entrypoint like 'pkg.module:MyClass' or 'src/path/file.py:MyClass::method'")
	rootFlag := flag.String("root", "", "Python source root (base directory containing package roots).")
	explain := flag.Bool("explain", false, "Print where each model is used (module:function).")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "modex traces a Python entrypoint and lists referenced models from static analysis.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  modex --entrypoint <module-or-path[:object]> [--root <path>] [--explain]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() == 0 && *entrypoint == "" {
		flag.Usage()
		return
	}

	if *entrypoint == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --entrypoint is required")
		os.Exit(2)
	}

	root := *rootFlag
	if root == "" {
		root = getRoot()
	}
	models, modelUsage, errors := collectModelsForEntrypoint(*entrypoint, root)
	if len(errors) > 0 {
		for _, err := range errors {
			fmt.Printf("ERROR: %s\n", err)
		}
		os.Exit(2)
	}

	modelList := make([]ModelRef, 0, len(models))
	for model := range models {
		modelList = append(modelList, model)
	}
	sort.Slice(modelList, func(i, j int) bool {
		if modelList[i].Module == modelList[j].Module {
			return modelList[i].Name < modelList[j].Name
		}
		return modelList[i].Module < modelList[j].Module
	})

	for _, model := range modelList {
		if *explain {
			fmt.Println(model.String())
			usageSet := modelUsage[model]
			usageList := make([]string, 0, len(usageSet))
			for item := range usageSet {
				usageList = append(usageList, item)
			}
			sort.Strings(usageList)
			if len(usageList) == 0 {
				fmt.Println("  - (unknown)")
			} else {
				for _, item := range usageList {
					fmt.Printf("  - %s\n", item)
				}
			}
		} else {
			fmt.Println(model.String())
		}
	}
}
