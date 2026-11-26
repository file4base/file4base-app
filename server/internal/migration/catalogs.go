package migration

// designOwners limits discovery to the document's design containers. File nodes
// nested in script operands, privileges or external references are not owners.
func designOwners(root *xmlNode) []*xmlNode {
	var owners []*xmlNode
	for _, child := range root.Children {
		if child.Name == "File" || child.Name == "Database" || child.Name == "FMSaveAsXMLFile" {
			owners = append(owners, child)
		}
		if root.Name == "FMSaveAsXML" && child.Name == "Structure" {
			for _, action := range child.Children {
				if action.Name == "AddAction" {
					owners = append(owners, action)
				}
			}
		}
	}
	if len(owners) == 0 {
		return []*xmlNode{root}
	}
	return owners
}

func designChildren(root *xmlNode, names ...string) []*xmlNode {
	var out []*xmlNode
	for _, owner := range designOwners(root) {
		for _, child := range owner.Children {
			if nodeNamed(child, names...) {
				out = append(out, child)
			}
		}
	}
	return out
}

func nodeNamed(node *xmlNode, names ...string) bool {
	for _, name := range names {
		if node.Name == name {
			return true
		}
	}
	return false
}

// catalogDefinitions reads definitions from their catalog, traversing its folder
// groups but never an object's contents. The same element names also occur as
// references in privileges, triggers, layout objects and script operands (#88).
// Compact design XML may put objects directly in the design container instead.
func catalogDefinitions(root *xmlNode, catalogs []string, objects ...string) []*xmlNode {
	var out []*xmlNode
	var visit func(*xmlNode)
	visit = func(container *xmlNode) {
		for _, child := range container.Children {
			switch {
			case nodeNamed(child, objects...):
				out = append(out, child)
			case nodeNamed(child, "Group", "group", "Folder", "folder", "ObjectList"):
				// Save a Copy as XML wraps some catalogs' members in an
				// <ObjectList>, for example <AccountsCatalog>.
				visit(child)
			}
		}
	}
	for _, owner := range designOwners(root) {
		for _, child := range owner.Children {
			switch {
			case nodeNamed(child, catalogs...):
				visit(child)
			case nodeNamed(child, objects...):
				out = append(out, child)
			}
		}
	}
	return out
}
