package diagram

import "fmt"

const (
	MaximumRank      = 10_000
	MaximumMinLength = 10_000
)

func (p *Problem) initializeAndValidate() error {
	for index, node := range p.nodes {
		if node.ID == "" {
			return fmt.Errorf("node at index %d has an empty ID", index)
		}
		if _, exists := p.nodeByID[node.ID]; exists {
			return fmt.Errorf("duplicate node ID %q", node.ID)
		}
		p.nodeByID[node.ID] = index
		if err := validateNodeNames(node); err != nil {
			return err
		}
		if node.Rank.Fixed != nil && (*node.Rank.Fixed < 0 || *node.Rank.Fixed > MaximumRank) {
			return fmt.Errorf("node %q fixed rank %d is outside 0..%d", node.ID, *node.Rank.Fixed, MaximumRank)
		}
	}
	for index, group := range p.groups {
		if group.ID == "" {
			return fmt.Errorf("group at index %d has an empty ID", index)
		}
		if _, exists := p.groupByID[group.ID]; exists {
			return fmt.Errorf("duplicate group ID %q", group.ID)
		}
		p.groupByID[group.ID] = index
		if err := validatePortNames("group", string(group.ID), group.Ports); err != nil {
			return err
		}
	}
	for index, edge := range p.edges {
		if edge.ID == "" {
			return fmt.Errorf("edge at index %d has an empty ID", index)
		}
		if _, exists := p.edgeByID[edge.ID]; exists {
			return fmt.Errorf("duplicate edge ID %q", edge.ID)
		}
		p.edgeByID[edge.ID] = index
		if edge.MinLength < 1 || edge.MinLength > MaximumMinLength {
			return fmt.Errorf("edge %q minimum length %d is outside 1..%d", edge.ID, edge.MinLength, MaximumMinLength)
		}
		if err := p.validateEndpoint(edge.Source); err != nil {
			return fmt.Errorf("edge %q source: %w", edge.ID, err)
		}
		if err := p.validateEndpoint(edge.Target); err != nil {
			return fmt.Errorf("edge %q target: %w", edge.ID, err)
		}
		if edge.Source.Kind == EndpointGroup && edge.Target.Kind == EndpointGroup && edge.Source.Group == edge.Target.Group {
			return fmt.Errorf("edge %q: group self-loops are not supported", edge.ID)
		}
	}
	for _, group := range p.groups {
		if group.Parent != "" {
			if _, exists := p.groupByID[group.Parent]; !exists {
				return fmt.Errorf("group %q has unknown parent %q", group.ID, group.Parent)
			}
			if err := p.setParent(ElementRef{Kind: ElementGroup, Group: group.ID}, group.Parent); err != nil {
				return err
			}
		}
		for _, member := range group.Members {
			if _, exists := p.nodeByID[member]; !exists {
				return fmt.Errorf("group %q has unknown member %q", group.ID, member)
			}
			if err := p.setParent(ElementRef{Kind: ElementNode, Node: member}, group.ID); err != nil {
				return err
			}
		}
	}
	for _, group := range p.groups {
		seen := map[GroupID]bool{group.ID: true}
		for parent := group.Parent; parent != ""; {
			if seen[parent] {
				return fmt.Errorf("group containment cycle at %q", parent)
			}
			seen[parent] = true
			parent = p.groups[p.groupByID[parent]].Parent
		}
	}
	return nil
}

func validateNodeNames(node Node) error {
	cells := make(map[CellID]bool)
	for _, row := range node.Cells {
		for _, cell := range row {
			if cell.ID == "" {
				continue
			}
			if cells[cell.ID] {
				return fmt.Errorf("node %q has duplicate cell ID %q", node.ID, cell.ID)
			}
			cells[cell.ID] = true
		}
	}
	return validatePortNames("node", string(node.ID), node.Ports)
}

func validatePortNames(ownerKind, ownerID string, ports []NamedPort) error {
	seen := make(map[PortID]bool)
	for _, port := range ports {
		if port.ID == "" {
			return fmt.Errorf("%s %q has an empty port ID", ownerKind, ownerID)
		}
		if seen[port.ID] {
			return fmt.Errorf("%s %q has duplicate port ID %q", ownerKind, ownerID, port.ID)
		}
		seen[port.ID] = true
	}
	return nil
}

func (p *Problem) setParent(ref ElementRef, parent GroupID) error {
	if existing, exists := p.parentOf[ref]; exists && existing != parent {
		return fmt.Errorf("element belongs to both groups %q and %q", existing, parent)
	}
	p.parentOf[ref] = parent
	return nil
}

func (p *Problem) validateEndpoint(endpoint Endpoint) error {
	switch endpoint.Kind {
	case EndpointNode:
		if endpoint.Node == "" || endpoint.Group != "" {
			return fmt.Errorf("node endpoint must set only Node")
		}
		index, exists := p.nodeByID[endpoint.Node]
		if !exists {
			return fmt.Errorf("unknown node %q", endpoint.Node)
		}
		node := p.nodes[index]
		if endpoint.Cell != "" && !nodeHasCell(node, endpoint.Cell) {
			return fmt.Errorf("node %q has no cell %q", endpoint.Node, endpoint.Cell)
		}
		if endpoint.NamedPort != "" && !hasPort(node.Ports, endpoint.NamedPort) {
			return fmt.Errorf("node %q has no port %q", endpoint.Node, endpoint.NamedPort)
		}
	case EndpointGroup:
		if endpoint.Group == "" || endpoint.Node != "" {
			return fmt.Errorf("group endpoint must set only Group")
		}
		index, exists := p.groupByID[endpoint.Group]
		if !exists {
			return fmt.Errorf("unknown group %q", endpoint.Group)
		}
		if endpoint.Cell != "" {
			return fmt.Errorf("group %q cannot have cell %q", endpoint.Group, endpoint.Cell)
		}
		if endpoint.NamedPort != "" && !hasPort(p.groups[index].Ports, endpoint.NamedPort) {
			return fmt.Errorf("group %q has no port %q", endpoint.Group, endpoint.NamedPort)
		}
	default:
		return fmt.Errorf("unknown endpoint kind %d", endpoint.Kind)
	}
	if endpoint.Cell != "" && endpoint.NamedPort != "" {
		return fmt.Errorf("endpoint cannot select both cell %q and port %q", endpoint.Cell, endpoint.NamedPort)
	}
	return nil
}

func nodeHasCell(node Node, id CellID) bool {
	for _, row := range node.Cells {
		for _, cell := range row {
			if cell.ID == id {
				return true
			}
		}
	}
	return false
}

func hasPort(ports []NamedPort, id PortID) bool {
	for _, port := range ports {
		if port.ID == id {
			return true
		}
	}
	return false
}
