package exchange

import "encoding/xml"

type graphML struct {
	XMLName xml.Name     `xml:"graphml"`
	XMLNS   string       `xml:"xmlns,attr"`
	Keys    []graphMLKey `xml:"key"`
	Graph   graphMLGraph `xml:"graph"`
}

type graphMLKey struct {
	ID   string `xml:"id,attr"`
	For  string `xml:"for,attr"`
	Name string `xml:"attr.name,attr"`
	Type string `xml:"attr.type,attr"`
}

type graphMLGraph struct {
	ID          string        `xml:"id,attr"`
	EdgeDefault string        `xml:"edgedefault,attr"`
	Data        []graphMLData `xml:"data"`
	Nodes       []graphMLNode `xml:"node"`
	Edges       []graphMLEdge `xml:"edge"`
}

type graphMLNode struct {
	ID   string        `xml:"id,attr"`
	Data []graphMLData `xml:"data"`
}

type graphMLEdge struct {
	ID       string        `xml:"id,attr"`
	Source   string        `xml:"source,attr"`
	Target   string        `xml:"target,attr"`
	Directed *bool         `xml:"directed,attr,omitempty"`
	Data     []graphMLData `xml:"data"`
}

type graphMLData struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}
