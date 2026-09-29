package plantuml

type Position struct {
	Line   int
	Column int
}

type Document struct {
	Statements  []Statement
	AllowMixing bool
}

type Statement interface {
	statementPosition() Position
}

type ElementKind uint8

const (
	ElementComponent ElementKind = iota
	ElementRectangle
	ElementCircle
	ElementClass
	ElementPackage
	ElementNode
	ElementFolder
	ElementFrame
	ElementCloud
	ElementDatabase
	ElementTogether
)

type ElementDecl struct {
	Kind       ElementKind
	Alias      string
	Label      string
	Members    []MemberDecl
	Ports      []PortDecl
	Statements []Statement
	Position   Position
}

func (d *ElementDecl) statementPosition() Position { return d.Position }

type MemberDecl struct {
	ID       string
	Label    string
	Position Position
}

type PortDecl struct {
	ID        string
	Direction string
	Position  Position
}

type EndpointRef struct {
	Name     string
	Member   string
	Position Position
}

type RelationDecl struct {
	Source   EndpointRef
	Target   EndpointRef
	Operator string
	Label    string
	Position Position
}

func (d *RelationDecl) statementPosition() Position { return d.Position }
