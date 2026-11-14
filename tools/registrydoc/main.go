// Command registrydoc 从 internal/protocol/msg_processor.go 的消息注册表导出
// Markdown 表格，并校验 docs/protocol-coverage.md 中的手工状态表与注册表一致。
//
// 用法：
//
//	go run ./tools/registrydoc -write   # 刷新文档中的自动生成段
//	go run ./tools/registrydoc -check   # 只校验，不写文件；发现漂移时退出码为 1
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	beginMarker = "<!-- BEGIN GENERATED REGISTRY -->"
	endMarker   = "<!-- END GENERATED REGISTRY -->"
)

type registryEntry struct {
	id       uint16
	comment  string
	incoming string
	outgoing string
	process  string
}

func main() {
	sourcePath := flag.String("source", "internal/protocol/msg_processor.go", "注册表源文件")
	ownershipPath := flag.String("ownership", "internal/protocol/ownership.go", "消息归属表源文件")
	docPath := flag.String("doc", "docs/protocol-coverage.md", "覆盖矩阵文档")
	write := flag.Bool("write", false, "把生成的表格写回文档")
	check := flag.Bool("check", false, "校验文档与注册表是否一致（默认行为）")
	flag.Parse()

	if *write && *check {
		fatalf("-write 与 -check 不能同时使用")
	}
	if !*write {
		*check = true
	}

	entries, functions, err := parseRegistry(*sourcePath)
	if err != nil {
		fatalf("解析注册表失败: %v", err)
	}
	ownership, err := parseOwnership(*ownershipPath)
	if err != nil {
		fatalf("解析消息归属表失败: %v", err)
	}
	if problems := checkOwnership(entries, functions, ownership); len(problems) > 0 {
		fmt.Fprintln(os.Stderr, "消息归属表与注册表不一致：")
		for _, p := range problems {
			fmt.Fprintf(os.Stderr, "  - %s\n", p)
		}
		os.Exit(1)
	}
	table := renderTable(entries)
	doc, err := os.ReadFile(*docPath)
	if err != nil {
		fatalf("读取文档失败: %v", err)
	}

	if *write {
		updated, err := replaceGeneratedSection(string(doc), table)
		if err != nil {
			fatalf("%v", err)
		}
		if err := os.WriteFile(*docPath, []byte(updated), 0o644); err != nil {
			fatalf("写入文档失败: %v", err)
		}
		fmt.Printf("已更新 %s，共 %d 条注册项\n", *docPath, len(entries))
		return
	}

	if problems := checkConsistency(string(doc), entries, table); len(problems) > 0 {
		fmt.Fprintln(os.Stderr, "覆盖矩阵与注册表不一致：")
		for _, p := range problems {
			fmt.Fprintf(os.Stderr, "  - %s\n", p)
		}
		os.Exit(1)
	}
	fmt.Printf("覆盖矩阵与注册表一致，共 %d 条注册项\n", len(entries))
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// parseRegistry 解析 options[0xXXXX] = &action{...} 以及
// for _, messageID := range []uint16{...} { options[messageID] = &action{...} } 两种注册形式。
// 同时返回源文件中声明的方法名集合，供归属表校验业务半边是否存在。
func parseRegistry(path string) ([]registryEntry, map[string]bool, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	lines := strings.Split(string(raw), "\n")

	byID := make(map[uint16]registryEntry)
	functions := make(map[string]bool)
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			functions[node.Name.Name] = true
		case *ast.RangeStmt:
			ids := compositeUint16IDs(node.X)
			if len(ids) == 0 {
				return true
			}
			action := findOptionsAssign(node.Body)
			if action == nil {
				return true
			}
			in, out, proc := readAction(action)
			for _, id := range ids {
				byID[id] = registryEntry{id: id, incoming: in, outgoing: out, process: proc}
			}
		case *ast.AssignStmt:
			id, ok := literalOptionsID(node)
			if !ok {
				return true
			}
			action := compositeFromValue(node.Rhs[0])
			if action == nil {
				return true
			}
			in, out, proc := readAction(action)
			comment := ""
			line := fset.Position(node.Pos()).Line
			if line > 0 && line <= len(lines) {
				comment = trailingComment(lines[line-1])
			}
			byID[id] = registryEntry{id: id, comment: comment, incoming: in, outgoing: out, process: proc}
		}
		return true
	})

	entries := make([]registryEntry, 0, len(byID))
	for _, e := range byID {
		entries = append(entries, e)
	}
	// 业务半边可能声明在注册表以外的文件；扫描同目录收集所有方法名。
	dirFunctions, err := parsePackageFunctions(filepath.Dir(path))
	if err != nil {
		return nil, nil, err
	}
	for name := range dirFunctions {
		functions[name] = true
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].id < entries[j].id })
	return entries, functions, nil
}

// parsePackageFunctions 收集目录下所有 Go 文件的函数/方法名。
func parsePackageFunctions(dir string) (map[string]bool, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool)
	fset := token.NewFileSet()
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				names[fn.Name.Name] = true
			}
		}
	}
	return names, nil
}

func literalOptionsID(assign *ast.AssignStmt) (uint16, bool) {
	if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return 0, false
	}
	index, ok := assign.Lhs[0].(*ast.IndexExpr)
	if !ok {
		return 0, false
	}
	base, ok := index.X.(*ast.Ident)
	if !ok || base.Name != "options" {
		return 0, false
	}
	lit, ok := index.Index.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return 0, false
	}
	value, err := strconv.ParseUint(lit.Value, 0, 16)
	if err != nil {
		return 0, false
	}
	return uint16(value), true
}

// compositeFromValue 解包 &action{...} 中的复合字面量。
func compositeFromValue(expr ast.Expr) *ast.CompositeLit {
	if unary, ok := expr.(*ast.UnaryExpr); ok {
		expr = unary.X
	}
	cl, _ := expr.(*ast.CompositeLit)
	return cl
}

func compositeUint16IDs(expr ast.Expr) []uint16 {
	cl, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	if arr, ok := cl.Type.(*ast.ArrayType); ok {
		if ident, ok := arr.Elt.(*ast.Ident); !ok || ident.Name != "uint16" {
			return nil
		}
	}
	var ids []uint16
	for _, elt := range cl.Elts {
		lit, ok := elt.(*ast.BasicLit)
		if !ok || lit.Kind != token.INT {
			continue
		}
		value, err := strconv.ParseUint(lit.Value, 0, 16)
		if err != nil {
			continue
		}
		ids = append(ids, uint16(value))
	}
	return ids
}

func findOptionsAssign(body *ast.BlockStmt) *ast.CompositeLit {
	var found *ast.CompositeLit
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		index, ok := assign.Lhs[0].(*ast.IndexExpr)
		if !ok {
			return true
		}
		if base, ok := index.X.(*ast.Ident); !ok || base.Name != "options" {
			return true
		}
		found = compositeFromValue(assign.Rhs[0])
		return false
	})
	return found
}

func readAction(action *ast.CompositeLit) (incoming, outgoing, process string) {
	for _, elt := range action.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "genData":
			funcLit, ok := kv.Value.(*ast.FuncLit)
			if !ok {
				continue
			}
			ast.Inspect(funcLit.Body, func(n ast.Node) bool {
				cl, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				selector, ok := cl.Type.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "ProcessData" {
					return true
				}
				for _, field := range cl.Elts {
					fieldKV, ok := field.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					fieldKey, ok := fieldKV.Key.(*ast.Ident)
					if !ok {
						continue
					}
					name := pointerModelName(fieldKV.Value)
					switch fieldKey.Name {
					case "Incoming":
						incoming = name
					case "Outgoing":
						outgoing = name
					}
				}
				return false
			})
		case "process":
			if selector, ok := kv.Value.(*ast.SelectorExpr); ok {
				process = selector.Sel.Name
			}
		}
	}
	return incoming, outgoing, process
}

func pointerModelName(expr ast.Expr) string {
	unary, ok := expr.(*ast.UnaryExpr)
	if !ok {
		return ""
	}
	cl, ok := unary.X.(*ast.CompositeLit)
	if !ok {
		return ""
	}
	selector, ok := cl.Type.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return selector.Sel.Name
}

func trailingComment(line string) string {
	if idx := strings.Index(line, "//"); idx >= 0 {
		return strings.TrimSpace(line[idx+2:])
	}
	return ""
}

// ownershipTable 是 ownership.go 中两份映射的解析结果。
type ownershipTable struct {
	owners         map[uint16]string
	businessHalves map[uint16]string
}

var (
	ownerLinePattern  = regexp.MustCompile(`(?m)^\s*(0x[0-9A-Fa-f]{4}):\s*(Owner[A-Za-z]+)\s*,`)
	halfLinePattern   = regexp.MustCompile(`(?m)^\s*(0x[0-9A-Fa-f]{4}):\s*"([A-Za-z0-9_]+)"\s*,`)
	businessHalfBlock = regexp.MustCompile(`(?s)var\s+BusinessHalves\s*=\s*map\[uint16\]string\s*\{(.*?)\n\}`)
)

// parseOwnership 解析 Ownership 与 BusinessHalves 两份映射。
func parseOwnership(path string) (ownershipTable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ownershipTable{}, err
	}
	table := ownershipTable{
		owners:         map[uint16]string{},
		businessHalves: map[uint16]string{},
	}
	for _, match := range ownerLinePattern.FindAllStringSubmatch(string(raw), -1) {
		id, err := strconv.ParseUint(strings.TrimPrefix(match[1], "0x"), 16, 16)
		if err != nil {
			continue
		}
		table.owners[uint16(id)] = match[2]
	}
	block := businessHalfBlock.FindStringSubmatch(string(raw))
	if block != nil {
		for _, match := range halfLinePattern.FindAllStringSubmatch(block[1], -1) {
			id, err := strconv.ParseUint(strings.TrimPrefix(match[1], "0x"), 16, 16)
			if err != nil {
				continue
			}
			table.businessHalves[uint16(id)] = match[2]
		}
	}
	return table, nil
}

// checkOwnership 校验注册表的每个消息 ID 都有明确归属，
// 且归属与业务半边一致：控制面必须有业务实现，网关项不得声明业务半边。
func checkOwnership(entries []registryEntry, functions map[string]bool, ownership ownershipTable) []string {
	var problems []string
	for _, entry := range entries {
		owner, ok := ownership.owners[entry.id]
		if !ok {
			problems = append(problems, fmt.Sprintf("`0x%04X` 已在注册表但 ownership.go 未声明归属", entry.id))
			continue
		}
		switch owner {
		case "OwnerGateway":
			if half, ok := ownership.businessHalves[entry.id]; ok {
				problems = append(problems, fmt.Sprintf("`0x%04X` 归属网关，不应声明业务半边 `%s`", entry.id, half))
			}
		case "OwnerControl":
			if entry.process == "" {
				problems = append(problems, fmt.Sprintf("`0x%04X` 归属管控，但注册项没有 process 业务实现", entry.id))
			}
		case "OwnerBoth":
			if entry.process == "" {
				problems = append(problems, fmt.Sprintf("`0x%04X` 归属双方，但注册项没有网关协议半边 process", entry.id))
			}
			half, ok := ownership.businessHalves[entry.id]
			if !ok {
				problems = append(problems, fmt.Sprintf("`0x%04X` 归属双方，但 BusinessHalves 未声明业务半边", entry.id))
				continue
			}
			if !functions[half] {
				problems = append(problems, fmt.Sprintf("`0x%04X` 的业务半边 `%s` 在处理器中不存在", entry.id, half))
			}
		default:
			problems = append(problems, fmt.Sprintf("`0x%04X` 归属值 `%s` 不是 OwnerGateway/OwnerControl/OwnerBoth", entry.id, owner))
		}
	}
	return problems
}

// uplinkOverrides 记录无法按 MsgID 段判断方向的 1078 上行消息。
// 0x9xxx 段整体是平台下行，但 0x9105 是终端上报的传输状态通知。
var uplinkOverrides = map[uint16]bool{
	0x9105: true,
}

func renderTable(entries []registryEntry) string {
	var b strings.Builder
	b.WriteString("| MsgID | 方向 | 代码注释 | 请求模型 | 应答模型 | 处理函数 |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, e := range entries {
		direction := "下行"
		if e.id < 0x8000 || uplinkOverrides[e.id] {
			direction = "上行"
		}
		b.WriteString(fmt.Sprintf("| `0x%04X` | %s | %s | %s | %s | %s |\n",
			e.id, direction, dash(e.comment), dash(e.incoming), dash(e.outgoing), dash(e.process)))
	}
	return b.String()
}

func dash(value string) string {
	if value == "" {
		return "—"
	}
	return "`" + value + "`"
}

func replaceGeneratedSection(doc, table string) (string, error) {
	start := strings.Index(doc, beginMarker)
	end := strings.Index(doc, endMarker)
	if start < 0 || end < 0 || end < start {
		return "", fmt.Errorf("文档缺少自动生成段的标记 %s / %s", beginMarker, endMarker)
	}
	return doc[:start+len(beginMarker)] + "\n\n" + table + "\n" + doc[end:], nil
}

var docRowPattern = regexp.MustCompile("^\\|\\s*`(0x[0-9A-Fa-f]{4})`")

func checkConsistency(doc string, entries []registryEntry, table string) []string {
	var problems []string

	registered := make(map[uint16]bool, len(entries))
	for _, e := range entries {
		registered[e.id] = true
	}

	documented := make(map[uint16]string)
	for _, line := range strings.Split(doc, "\n") {
		match := docRowPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		value, err := strconv.ParseUint(strings.TrimPrefix(match[1], "0x"), 16, 16)
		if err != nil {
			continue
		}
		status := statusColumn(line)
		documented[uint16(value)] = status
	}

	var missing []string
	for _, e := range entries {
		if _, ok := documented[e.id]; !ok {
			missing = append(missing, fmt.Sprintf("`0x%04X` 已在注册表但覆盖矩阵未记录", e.id))
		}
	}
	sort.Strings(missing)
	problems = append(problems, missing...)

	var stale []uint16
	for id, status := range documented {
		if registered[id] {
			continue
		}
		if status == "缺失" || status == "保留/自定义" {
			continue
		}
		stale = append(stale, id)
	}
	sort.Slice(stale, func(i, j int) bool { return stale[i] < stale[j] })
	for _, id := range stale {
		problems = append(problems, fmt.Sprintf("`0x%04X` 在矩阵中标记为 `%s`，但注册表中不存在", id, documented[id]))
	}

	start := strings.Index(doc, beginMarker)
	end := strings.Index(doc, endMarker)
	if start < 0 || end < 0 || end < start {
		problems = append(problems, "文档缺少自动生成段的标记")
		return problems
	}
	current := strings.TrimSpace(doc[start+len(beginMarker) : end])
	if current != strings.TrimSpace(table) {
		problems = append(problems, "自动生成段已过期，请运行 `go run ./tools/registrydoc -write`")
	}
	return problems
}

func statusColumn(line string) string {
	parts := strings.Split(line, "|")
	if len(parts) < 4 {
		return ""
	}
	return strings.TrimSpace(strings.Trim(parts[3], "`* "))
}
