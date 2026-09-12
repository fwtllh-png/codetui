package symbols

import (
	"regexp"
	"strings"
)

// One rule table serves C and C++: the declaration forms C++ adds — templates,
// namespaces, operators, access labels — have C-shaped cores, and the
// differences that matter to a reader (which overloads what) are signature
// detail the decorate step keeps. It is Tier 1: rows report ResolutionLexical.

var (
	// cppTemplateHead is stripped before a declaration is matched, so
	// `template<typename T> T max(T a, T b)` reads as the `T max(...)` inside
	// it. The parameters of the template itself stay in the signature.
	cppTemplateHead = regexp.MustCompile(`^template\s*<[^>]*>\s*`)
	// cppTypeHead reads class, struct and union heads. An attribute may sit
	// between the keyword and the name; inheritance and `final` sit after the
	// name and are the signature's business.
	cppTypeHead = regexp.MustCompile(
		`^(?:class|struct|union)\s+(?:__attribute__\s*\([^)]*\)\s+)?` +
			`(?:alignas\s*\([^)]*\)\s+)?([A-Za-z_]\w*)`,
	)
	cppEnumHead = regexp.MustCompile(`^enum(?:\s+(?:class|struct))?\s+([A-Za-z_]\w*)`)
	// cppNamespaceHead names a namespace by its full path; nested paths read
	// as one container, which is what a reader navigates by.
	cppNamespaceHead = regexp.MustCompile(`^namespace\s+([A-Za-z_][\w:]*)`)
	// cppTailModifiers are the words a member head may end with after its
	// parameter list: `) const`, `) const noexcept override`. Stripping them
	// lets an Allman head end at its `)` where the body brace opens below.
	cppTailModifiers = regexp.MustCompile(
		`((?:\s+(?:const|noexcept|override|final))+)$`,
	)
	// cppSpecialMember suppresses the `= default` / `= delete` tail so the
	// member still reports as a declaration.
	cppSpecialMember = regexp.MustCompile(`=\s*(?:default|delete|0)\s*$`)
	// cppAssignment matches an assignment that is not also a comparison, so a
	// call used as a value (`int x = compute();`) does not read as a
	// declaration.
	cppAssignment = regexp.MustCompile(`[^=!<>+\-*/%&|^]=[^=]`)
	// cppAccessLabel switches the exported view inside a class-like container.
	cppAccessLabel = regexp.MustCompile(`^(public|private|protected)\s*:`)
)

// cppContainer is one class, struct or namespace being walked through. A
// class-like container carries the exported view of its members: a struct
// starts public, a class starts private, and either switches at its next
// access label. A namespace leaves its members visible.
type cppContainer struct {
	name      string
	classLike bool
	exported  bool
	depth     int
	opened    bool
}

// extractCPP walks braces for containers and access labels for visibility.
// Exported answers "declared where a reader can reach it": a file-scope or
// namespace-scope function reports true, a class member follows its labels.
// Declarations are indexed alongside definitions — a header's member
// prototypes and the .cpp definitions are two rows, and a search that finds
// both errs on the side of finding.
func extractCPP(lines []line, options Options) []Symbol {
	var found []Symbol
	var containers []cppContainer
	depth := 0
	for index, source := range lines {
		trimmed := strings.TrimSpace(source.Code)
		if trimmed == "" {
			depth += strings.Count(source.Code, "{") - strings.Count(source.Code, "}")
			continue
		}
		// Preprocessor lines carry no declarations; the scan leaves them as
		// code, so they are excluded here by their shape.
		if strings.HasPrefix(trimmed, "#") {
			depth += strings.Count(source.Code, "{") - strings.Count(source.Code, "}")
			continue
		}
		if match := cppAccessLabel.FindStringSubmatch(trimmed); match != nil {
			for position := len(containers) - 1; position >= 0; position-- {
				if containers[position].classLike {
					containers[position].exported = match[1] == "public"
					break
				}
			}
			depth += strings.Count(source.Code, "{") - strings.Count(source.Code, "}")
			continue
		}
		declaration := cppTemplateHead.ReplaceAllLiteralString(trimmed, "")
		name, kind, isNamespace, isClassLike, exportedDefault :=
			cppTypeDeclaration(declaration)
		if isNamespace {
			containers = append(containers, cppContainer{
				name: name, depth: depth,
			})
			depth += strings.Count(source.Code, "{") - strings.Count(source.Code, "}")
			continue
		}
		containerName, classOwner := cppInnermost(containers)
		if name != "" {
			memberExported := exportedDefault
			if classOwner != nil {
				memberExported = classOwner.exported
			}
			found = append(found, decorate(lines, index, Symbol{
				Name: name, Kind: kind, Container: containerName,
				Line: source.Number, Exported: memberExported,
				Resolution: ResolutionLexical,
			}, options))
			if isClassLike {
				containers = append(containers, cppContainer{
					name: name, classLike: true,
					exported: exportedDefault, depth: depth,
				})
			}
			depth += strings.Count(source.Code, "{") - strings.Count(source.Code, "}")
			continue
		}
		if member, ok := cppFunctionHead(
			lines, index, declaration, classOwner != nil,
		); ok {
			// A namespace is a place, not a type: a function inside one stays
			// a function that names its container, while a class or struct
			// makes its members methods.
			memberKind := KindFunction
			memberExported := true
			if classOwner != nil {
				memberKind = KindMethod
				memberExported = classOwner.exported
			}
			found = append(found, decorate(lines, index, Symbol{
				Name: member, Kind: memberKind, Container: containerName,
				Line: source.Number, Exported: memberExported,
				Resolution: ResolutionLexical,
			}, options))
		}
		depth += strings.Count(source.Code, "{") - strings.Count(source.Code, "}")
		if depth < 0 {
			depth = 0
		}
		for position := range containers {
			if depth > containers[position].depth {
				containers[position].opened = true
			}
		}
		for len(containers) != 0 {
			innermost := containers[len(containers)-1]
			if !innermost.opened || depth > innermost.depth {
				break
			}
			containers = containers[:len(containers)-1]
		}
	}
	return found
}

// cppTypeDeclaration classifies a declaration head as a type or namespace
// opener. name is empty when the line opens neither.
func cppTypeDeclaration(declaration string) (
	name, kind string, isNamespace, isClassLike bool, exportedDefault bool,
) {
	if match := cppNamespaceHead.FindStringSubmatch(declaration); match != nil {
		return match[1], "", true, false, false
	}
	if match := cppTypeHead.FindStringSubmatch(declaration); match != nil {
		switch {
		case strings.HasPrefix(declaration, "class"):
			return match[1], KindClass, false, true, false
		case strings.HasPrefix(declaration, "struct"),
			strings.HasPrefix(declaration, "union"):
			return match[1], KindType, false, true, true
		}
	}
	if match := cppEnumHead.FindStringSubmatch(declaration); match != nil {
		return match[1], KindType, false, false, true
	}
	return "", "", false, false, false
}

// cppInnermost reports the name of the innermost container — namespace or
// class — plus the nearest class-like container, which is the one a member's
// kind and visibility belong to. A nil owner means the position leaves
// visibility on and a function stays a function.
func cppInnermost(containers []cppContainer) (string, *cppContainer) {
	var owner *cppContainer
	for position := len(containers) - 1; position >= 0; position-- {
		if containers[position].classLike {
			owner = &containers[position]
			break
		}
	}
	if len(containers) == 0 {
		return "", owner
	}
	return containers[len(containers)-1].name, owner
}

// cppFunctionHead decides that a line declares a function. The head is a
// name with a balanced parameter list that opens a body on this line, ends a
// declaration (`;`, the header-file form), or ends at `)` with the brace
// opening below (Allman). classContext relaxes the return-type requirement
// for constructors and destructors, which have none. A destructor's `~` is
// part of its name.
func cppFunctionHead(
	lines []line, index int, declaration string, classContext bool,
) (string, bool) {
	cleaned := cppSpecialMember.ReplaceAllLiteralString(declaration, "")
	if cppAssignment.MatchString(cleaned) {
		return "", false
	}
	if !strings.HasSuffix(cleaned, "{") && !strings.HasSuffix(cleaned, ";") {
		withoutTail := strings.TrimSpace(
			cppTailModifiers.ReplaceAllLiteralString(cleaned, ""),
		)
		if !strings.HasSuffix(withoutTail, ")") {
			return "", false
		}
		opensBody := false
		for rest := index + 1; rest < len(lines); rest++ {
			following := strings.TrimSpace(lines[rest].Code)
			if following == "" {
				continue
			}
			opensBody = strings.HasPrefix(following, "{")
			break
		}
		if !opensBody {
			return "", false
		}
	}
	location := genericCall.FindStringSubmatchIndex(cleaned)
	if location == nil {
		return "", false
	}
	name := cleaned[location[2]:location[3]]
	if _, control := callHeadKeywords[name]; control {
		return "", false
	}
	head := strings.TrimSpace(cleaned[:location[2]])
	// Outside a class a bare `name(...)` is a call statement; a declaration
	// carries a return type to the name's left. Inside a class the bare form
	// is a constructor or destructor, which is exactly the rule that lets it
	// through.
	if head == "" && !classContext {
		return "", false
	}
	if strings.Contains(cleaned, "~"+name+"(") {
		name = "~" + name
	}
	return name, true
}
