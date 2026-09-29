package plantuml

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
)

func ParseDocument(r io.Reader) (*Document, error) {
	return ParseDocumentWithOptions(r, Options{})
}

func ParseDocumentWithOptions(r io.Reader, options Options) (*Document, error) {
	lines, err := readLines(r)
	if err != nil {
		return nil, err
	}
	first := firstContentLine(lines, 0)
	if first < 0 || !strings.HasPrefix(strings.TrimSpace(lines[first].Text), "@startuml") {
		line := 1
		if first >= 0 {
			line = lines[first].Number
		}
		return nil, syntax(line, 1, "expected @startuml")
	}
	if fields := strings.Fields(strings.TrimSpace(lines[first].Text)); len(fields) > 2 {
		return nil, syntax(lines[first].Number, 1, "@startuml accepts at most one output name")
	}
	last := lastContentLine(lines)
	if last <= first || strings.TrimSpace(lines[last].Text) != "@enduml" {
		line := lines[first].Number
		if last >= 0 {
			line = lines[last].Number
		}
		return nil, syntax(line, 1, "expected @enduml")
	}
	for index := last + 1; index < len(lines); index++ {
		if content(lines[index].Text) != "" {
			return nil, syntax(lines[index].Number, 1, "content after @enduml is not supported")
		}
	}
	p := parser{lines: lines, index: first + 1, end: last, strict: options.Strict}
	statements, err := p.parseStatements(false)
	if err != nil {
		return nil, err
	}
	document := &Document{Statements: statements, AllowMixing: p.allowMixing}
	if err := validateMixing(document); err != nil {
		return nil, err
	}
	return document, nil
}

type parser struct {
	lines       []sourceLine
	index       int
	end         int
	strict      bool
	allowMixing bool
}

func (p *parser) parseStatements(inBlock bool) ([]Statement, error) {
	var statements []Statement
	for p.index < p.end {
		line := p.lines[p.index]
		text := content(line.Text)
		p.index++
		if text == "" {
			continue
		}
		if text == "}" {
			if !inBlock {
				return nil, syntax(line.Number, 1, "unexpected }")
			}
			return statements, nil
		}
		statement, err := p.parseStatement(line, text)
		if err != nil {
			return nil, err
		}
		if statement != nil {
			statements = append(statements, statement)
		}
	}
	if inBlock {
		line := 1
		if p.end > 0 {
			line = p.lines[p.end-1].Number
		}
		return nil, syntax(line, 1, "unterminated block; expected }")
	}
	return statements, nil
}

func (p *parser) parseStatement(line sourceLine, text string) (Statement, error) {
	if strings.HasPrefix(text, "@") {
		return nil, syntax(line.Number, 1, "multiple PlantUML blocks are not supported")
	}
	if text == "allowmixing" {
		p.allowMixing = true
		return nil, nil
	}
	if text == "skinparam componentStyle rectangle" || text == "hide empty members" {
		if p.strict && text != "allowmixing" {
			return nil, syntax(line.Number, 1, "statement %q has no ASCII rendering effect", text)
		}
		return nil, nil
	}
	if text == "left to right direction" || text == "top to bottom direction" {
		return &ElementDecl{Kind: ElementTogether, Alias: text, Position: Position{Line: line.Number, Column: 1}}, nil
	}
	if p.strict && strings.HasPrefix(text, "together ") {
		return nil, syntax(line.Number, 1, "statement together has no layout effect")
	}
	if declarationKeyword(text) != "" || strings.HasPrefix(text, "[") || strings.HasPrefix(text, "()") {
		return p.parseElementOrRelation(line, text)
	}
	if looksLikeRelation(text) {
		return parseRelation(text, line.Number)
	}
	return nil, syntax(line.Number, 1, "unsupported PlantUML statement %q", text)
}

func validateMixing(document *Document) error {
	if document.AllowMixing {
		return nil
	}
	hasClass, hasComponent, line := false, false, 1
	var visit func([]Statement)
	visit = func(statements []Statement) {
		for _, statement := range statements {
			decl, ok := statement.(*ElementDecl)
			if !ok {
				continue
			}
			if decl.Kind == ElementClass {
				hasClass, line = true, decl.Position.Line
			} else if decl.Kind == ElementComponent || decl.Kind == ElementRectangle || decl.Kind == ElementCircle {
				hasComponent, line = true, decl.Position.Line
			}
			visit(decl.Statements)
		}
	}
	visit(document.Statements)
	if hasClass && hasComponent {
		return syntax(line, 1, "mixing class and component declarations requires allowmixing")
	}
	return nil
}

func (p *parser) parseElementOrRelation(line sourceLine, text string) (Statement, error) {
	if looksLikeRelation(text) {
		return parseRelation(text, line.Number)
	}
	decl, opens, err := parseElementHeader(text, line.Number)
	if err != nil {
		return nil, err
	}
	if !opens {
		return decl, nil
	}
	if decl.Kind == ElementClass {
		members, err := p.parseClassMembers()
		if err != nil {
			return nil, err
		}
		decl.Members = members
		return decl, nil
	}
	if decl.Kind == ElementComponent {
		statements, ports, err := p.parseComponentBody()
		if err != nil {
			return nil, err
		}
		decl.Statements, decl.Ports = statements, ports
		return decl, nil
	}
	statements, err := p.parseStatements(true)
	if err != nil {
		return nil, err
	}
	decl.Statements = statements
	return decl, nil
}

func (p *parser) parseClassMembers() ([]MemberDecl, error) {
	var members []MemberDecl
	seen := make(map[string]bool)
	for p.index < p.end {
		line := p.lines[p.index]
		text := content(line.Text)
		p.index++
		if text == "" {
			continue
		}
		if text == "}" {
			return members, nil
		}
		if isClassSeparator(text) {
			continue
		}
		id := memberID(text)
		if id == "" {
			return nil, syntax(line.Number, 1, "class member has no usable name")
		}
		if seen[id] {
			return nil, syntax(line.Number, 1, "duplicate class member %q", id)
		}
		seen[id] = true
		members = append(members, MemberDecl{ID: id, Label: unescapeText(text), Position: Position{Line: line.Number, Column: 1}})
	}
	return nil, syntax(p.lines[p.end-1].Number, 1, "unterminated class block")
}

func (p *parser) parseComponentBody() ([]Statement, []PortDecl, error) {
	var statements []Statement
	var ports []PortDecl
	for p.index < p.end {
		line := p.lines[p.index]
		text := content(line.Text)
		if text == "" {
			p.index++
			continue
		}
		if text == "}" {
			p.index++
			return statements, ports, nil
		}
		fields := strings.Fields(text)
		if len(fields) == 2 && (fields[0] == "port" || fields[0] == "portin" || fields[0] == "portout") {
			p.index++
			ports = append(ports, PortDecl{ID: fields[1], Direction: fields[0], Position: Position{Line: line.Number, Column: 1}})
			continue
		}
		p.index++
		statement, err := p.parseStatement(line, text)
		if err != nil {
			return nil, nil, err
		}
		if statement != nil {
			statements = append(statements, statement)
		}
	}
	return nil, nil, syntax(p.lines[p.end-1].Number, 1, "unterminated component block")
}

func parseElementHeader(text string, line int) (*ElementDecl, bool, error) {
	opens := strings.HasSuffix(strings.TrimSpace(text), "{")
	if opens {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "{"))
	}
	position := Position{Line: line, Column: 1}
	if strings.HasPrefix(text, "[") {
		label, rest, err := consumeBracket(text)
		if err != nil {
			return nil, false, syntax(line, 1, "%v", err)
		}
		alias, err := parseAlias(rest)
		if err != nil {
			return nil, false, syntax(line, 1, "%v", err)
		}
		if alias == "" {
			alias = label
		}
		return &ElementDecl{Kind: ElementComponent, Alias: alias, Label: unescapeText(label), Position: position}, opens, nil
	}
	if strings.HasPrefix(text, "()") {
		label, alias, err := parseNameAlias(strings.TrimSpace(strings.TrimPrefix(text, "()")))
		if err != nil {
			return nil, false, syntax(line, 1, "%v", err)
		}
		return &ElementDecl{Kind: ElementCircle, Alias: alias, Label: label, Position: position}, opens, nil
	}
	keyword := declarationKeyword(text)
	if keyword == "" {
		return nil, false, syntax(line, 1, "expected an element declaration")
	}
	rest := strings.TrimSpace(strings.TrimPrefix(text, keyword))
	if keyword == "together" {
		if rest != "" {
			return nil, false, syntax(line, 1, "together does not have a name")
		}
		return &ElementDecl{Kind: ElementTogether, Position: position}, opens, nil
	}
	label, alias, err := parseNameAlias(rest)
	if err != nil {
		return nil, false, syntax(line, 1, "%v", err)
	}
	if label == "" {
		return nil, false, syntax(line, 1, "%s requires a name", keyword)
	}
	return &ElementDecl{Kind: elementKind(keyword), Alias: alias, Label: label, Position: position}, opens, nil
}

func parseNameAlias(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") {
		label, rest, err := consumeBracket(raw)
		if err != nil {
			return "", "", err
		}
		alias, err := parseAlias(rest)
		if alias == "" {
			alias = label
		}
		return unescapeText(label), alias, err
	}
	name, rest, err := consumeName(raw)
	if err != nil {
		return "", "", err
	}
	alias, err := parseAlias(rest)
	if err != nil {
		return "", "", err
	}
	if alias == "" {
		alias = name
	}
	return unescapeText(name), alias, nil
}

func parseAlias(rest string) (string, error) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", nil
	}
	if !strings.HasPrefix(rest, "as ") {
		return "", fmt.Errorf("unexpected text %q after element name", rest)
	}
	alias := strings.TrimSpace(strings.TrimPrefix(rest, "as "))
	if !validIdentifier(alias) {
		return "", fmt.Errorf("invalid alias %q", alias)
	}
	return alias, nil
}

func parseRelation(text string, line int) (*RelationDecl, error) {
	withoutLabel, label, err := splitRelationLabel(text)
	if err != nil {
		return nil, syntax(line, 1, "%v", err)
	}
	start, end := relationOperatorBounds(withoutLabel)
	if start < 0 {
		return nil, syntax(line, 1, "expected a supported relation operator")
	}
	sourceText := strings.TrimSpace(withoutLabel[:start])
	targetText := strings.TrimSpace(withoutLabel[end:])
	if sourceText == "" || targetText == "" {
		return nil, syntax(line, 1, "relation requires source and target")
	}
	if nextStart, _ := relationOperatorBounds(targetText); nextStart >= 0 {
		return nil, syntax(line, 1, "chained relations are not supported; write one relation per line")
	}
	source, err := parseEndpointRef(sourceText, line)
	if err != nil {
		return nil, err
	}
	target, err := parseEndpointRef(targetText, line)
	if err != nil {
		return nil, err
	}
	operator := strings.ReplaceAll(withoutLabel[start:end], " ", "")
	if err := validateOperator(operator); err != nil {
		return nil, syntax(line, start+1, "%v", err)
	}
	return &RelationDecl{Source: source, Target: target, Operator: operator, Label: label, Position: Position{Line: line, Column: 1}}, nil
}

func parseEndpointRef(raw string, line int) (EndpointRef, error) {
	raw = strings.TrimSpace(raw)
	name := raw
	member := ""
	if index := strings.Index(raw, "::"); index >= 0 {
		name, member = strings.TrimSpace(raw[:index]), strings.Trim(strings.TrimSpace(raw[index+2:]), `"`)
	}
	if strings.HasPrefix(name, "[") {
		label, rest, err := consumeBracket(name)
		if err != nil || strings.TrimSpace(rest) != "" {
			return EndpointRef{}, syntax(line, 1, "invalid bracketed endpoint %q", raw)
		}
		name = label
	} else if strings.HasPrefix(name, `"`) {
		value, rest, err := consumeName(name)
		if err != nil || strings.TrimSpace(rest) != "" {
			return EndpointRef{}, syntax(line, 1, "invalid quoted endpoint %q", raw)
		}
		name = value
	}
	if name == "" || member == "" && strings.Contains(raw, "::") {
		return EndpointRef{}, syntax(line, 1, "invalid endpoint %q", raw)
	}
	return EndpointRef{Name: unescapeText(name), Member: member, Position: Position{Line: line, Column: 1}}, nil
}

func splitRelationLabel(text string) (string, string, error) {
	inQuote, bracket := false, 0
	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '"':
			if index == 0 || text[index-1] != '\\' {
				inQuote = !inQuote
			}
		case '[':
			if !inQuote {
				bracket++
			}
		case ']':
			if !inQuote && bracket > 0 {
				bracket--
			}
		case ':':
			if !inQuote && bracket == 0 && (index == 0 || text[index-1] != ':') && (index+1 >= len(text) || text[index+1] != ':') {
				label := strings.TrimSpace(text[index+1:])
				if label == "" {
					return "", "", fmt.Errorf("empty relation label")
				}
				if strings.HasPrefix(label, `"`) {
					value, rest, err := consumeName(label)
					if err != nil || strings.TrimSpace(rest) != "" {
						return "", "", fmt.Errorf("invalid quoted relation label")
					}
					label = value
				}
				return strings.TrimSpace(text[:index]), unescapeText(label), nil
			}
		}
	}
	return text, "", nil
}

func relationOperatorBounds(text string) (int, int) {
	inQuote, bracket := false, 0
	for index := 0; index < len(text); index++ {
		char := text[index]
		if char == '"' && (index == 0 || text[index-1] != '\\') {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		if char == '[' {
			bracket++
			continue
		}
		if char == ']' && bracket > 0 {
			bracket--
			continue
		}
		if bracket == 0 && (char == '-' || char == '.' || char == '<') {
			end := index
			if text[end] == '<' {
				end++
			}
			for end < len(text) && (text[end] == '-' || text[end] == '.') {
				end++
			}
			if end < len(text) && text[end] == '[' {
				if close := strings.IndexByte(text[end:], ']'); close >= 0 {
					end += close + 1
				} else {
					return index, len(text)
				}
			}
			for _, direction := range []string{"left", "right", "up", "down", "l", "r", "u", "d"} {
				if strings.HasPrefix(text[end:], direction) {
					end += len(direction)
					break
				}
			}
			for end < len(text) && (text[end] == '-' || text[end] == '.') {
				end++
			}
			if end < len(text) && text[end] == '>' {
				end++
			}
			return index, end
		}
	}
	return -1, -1
}

func validateOperator(operator string) error {
	styleStart := strings.IndexByte(operator, '[')
	styleEnd := strings.IndexByte(operator, ']')
	base := operator
	if styleStart >= 0 {
		if styleEnd < styleStart {
			return fmt.Errorf("unterminated relation style")
		}
		style := operator[styleStart+1 : styleEnd]
		switch style {
		case "bold", "dashed", "dotted", "hidden", "plain":
		default:
			return fmt.Errorf("unsupported relation style %q", style)
		}
		base = operator[:styleStart] + operator[styleEnd+1:]
	}
	for _, direction := range []string{"left", "right", "up", "down"} {
		base = strings.Replace(base, direction, "", 1)
	}
	if len(base) > 2 {
		for _, direction := range []string{"l", "r", "u", "d"} {
			base = strings.Replace(base, direction, "", 1)
		}
	}
	if !strings.ContainsAny(base, "-.") || strings.Count(base, "<") > 1 || strings.Count(base, ">") > 1 {
		return fmt.Errorf("unsupported relation operator %q", operator)
	}
	for _, char := range base {
		if !strings.ContainsRune("-.<>", char) {
			return fmt.Errorf("unsupported relation operator %q", operator)
		}
	}
	return nil
}

func looksLikeRelation(text string) bool {
	start, end := relationOperatorBounds(text)
	return start > 0 && end > start
}

func declarationKeyword(text string) string {
	for _, keyword := range []string{"component", "rectangle", "circle", "class", "package", "node", "folder", "frame", "cloud", "database", "together"} {
		if text == keyword || strings.HasPrefix(text, keyword+" ") || strings.HasPrefix(text, keyword+"{") {
			return keyword
		}
	}
	return ""
}

func elementKind(keyword string) ElementKind {
	return map[string]ElementKind{"component": ElementComponent, "rectangle": ElementRectangle, "circle": ElementCircle, "class": ElementClass, "package": ElementPackage, "node": ElementNode, "folder": ElementFolder, "frame": ElementFrame, "cloud": ElementCloud, "database": ElementDatabase, "together": ElementTogether}[keyword]
}

func consumeBracket(raw string) (string, string, error) {
	if !strings.HasPrefix(raw, "[") {
		return "", raw, fmt.Errorf("expected [")
	}
	for index := 1; index < len(raw); index++ {
		if raw[index] == ']' && raw[index-1] != '\\' {
			return raw[1:index], raw[index+1:], nil
		}
	}
	return "", "", fmt.Errorf("unterminated component name")
}

func consumeName(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", nil
	}
	if raw[0] != '"' {
		fields := strings.Fields(raw)
		if len(fields) == 0 {
			return "", "", nil
		}
		return fields[0], strings.TrimSpace(strings.TrimPrefix(raw, fields[0])), nil
	}
	for index := 1; index < len(raw); index++ {
		if raw[index] == '"' && raw[index-1] != '\\' {
			value, err := strconv.Unquote(raw[:index+1])
			if err != nil {
				return "", "", err
			}
			return value, strings.TrimSpace(raw[index+1:]), nil
		}
	}
	return "", "", fmt.Errorf("unterminated quoted name")
}

func validIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if !(unicode.IsLetter(char) || unicode.IsDigit(char) || char == '_' || char == '.' || char == '-') {
			return false
		}
	}
	return true
}

func memberID(text string) string {
	text = strings.TrimSpace(strings.TrimLeft(text, "+-#~"))
	if index := strings.IndexByte(text, ':'); index >= 0 {
		return strings.TrimSpace(text[:index])
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	if len(fields) > 1 {
		return strings.TrimSuffix(fields[len(fields)-1], "()")
	}
	return strings.TrimSuffix(fields[0], "()")
}

func isClassSeparator(text string) bool {
	trimmed := strings.Trim(text, " ._=-")
	return trimmed == ""
}

func unescapeText(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, `\n`, "\n"), `\"`, `"`)
}

func content(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "'") {
		return ""
	}
	return trimmed
}

func firstContentLine(lines []sourceLine, start int) int {
	for index := start; index < len(lines); index++ {
		if content(lines[index].Text) != "" {
			return index
		}
	}
	return -1
}

func lastContentLine(lines []sourceLine) int {
	for index := len(lines) - 1; index >= 0; index-- {
		if content(lines[index].Text) != "" {
			return index
		}
	}
	return -1
}
